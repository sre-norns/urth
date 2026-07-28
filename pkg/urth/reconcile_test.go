package urth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The reconciler is tested against a real Postgres for the same reason the
// outbox is: what is being asserted here is that a Result transition and the
// retirement of its dispatch land or fail together, and that two writers racing
// one Result produce exactly one winner. Neither is a property of the code --
// both are properties of a transaction and a conditional UPDATE.

// fakeChannels stands in for the transport's half of reconciliation.
type fakeChannels struct {
	mu sync.Mutex

	// missing names the runners whose queue does not exist, so a scan is seen to
	// restore only those rather than reporting every runner as repaired.
	missing map[manifest.ResourceID]bool

	ensured []manifest.ResourceID
	dropped []uint64

	ensureErr error
	dropErr   error
}

func newFakeChannels(missing ...manifest.ResourceID) *fakeChannels {
	absent := map[manifest.ResourceID]bool{}
	for _, uid := range missing {
		absent[uid] = true
	}

	return &fakeChannels{missing: absent}
}

func (f *fakeChannels) EnsureRunnerChannel(_ context.Context, runnerUID manifest.ResourceID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ensured = append(f.ensured, runnerUID)
	if f.ensureErr != nil {
		return false, f.ensureErr
	}

	restored := f.missing[runnerUID]
	delete(f.missing, runnerUID)

	return restored, nil
}

func (f *fakeChannels) DropDispatch(_ context.Context, entry urth.DispatchOutboxEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.dropErr != nil {
		return f.dropErr
	}
	f.dropped = append(f.dropped, entry.PublishedSeq)

	return nil
}

func (f *fakeChannels) droppedSeqs() []uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]uint64(nil), f.dropped...)
}

// testKeys mints the three signing secrets a full claim needs.
func testKeys(t *testing.T) urth.SigningKeys {
	t.Helper()

	keys, err := urth.SigningKeysConfig{
		EnrolmentKey: "test-enrolment",
		SessionKey:   "test-session",
		RunKey:       "test-run",
	}.Build()
	require.NoError(t, err)

	return keys
}

// claimRun drives the whole registration and claim handshake, so a test of what
// happens *after* a claim starts from a Result a worker genuinely holds --
// executor recorded, dispatch ID recorded, lease recorded -- rather than from
// one a test wrote to look that way.
func claimRun(t *testing.T, srv urth.Service, result urth.Result) urth.AuthJobResponse {
	t.Helper()

	ctx := context.Background()

	enrolment, found, err := srv.Runners().GetToken(ctx, "test-runner")
	require.NoError(t, err)
	require.True(t, found)

	registration, err := srv.Runners().AuthWorker(ctx, enrolment, manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance},
		Metadata: manifest.ObjectMeta{Name: "test-worker"},
		Spec:     &urth.WorkerInstanceSpec{},
	})
	require.NoError(t, err)

	claim, err := srv.Results("").ClaimRun(ctx, result.UID, registration.Session, urth.ClaimJobRequest{
		DispatchID:    urth.DispatchEventUID(result.UID, result.Version),
		ResultVersion: result.Version,
	})
	require.NoError(t, err)

	return claim
}

// backdateCreation ages a Result so it looks like one that has been waiting.
// UpdateColumn, not Update: the point is to move creation time without touching
// the version or update time the reconciler and its guards read.
func backdateCreation(t *testing.T, db *gorm.DB, uid manifest.ResourceID, at time.Time) {
	t.Helper()

	require.NoError(t, db.Model(&urth.Result{}).
		Where("uid = ?", uid).
		UpdateColumn("created_at", at).Error)
}

func loadResult(t *testing.T, store *dbstore.DBStore, uid manifest.ResourceID) urth.Result {
	t.Helper()

	var result urth.Result
	found, err := store.GetByUID(context.Background(), &result, uid)
	require.NoError(t, err)
	require.True(t, found)

	return result
}

func loadDispatch(t *testing.T, db *gorm.DB, uid manifest.ResourceID) urth.DispatchOutboxEntry {
	t.Helper()

	var entry urth.DispatchOutboxEntry
	require.NoError(t, db.Where("result_uid = ?", uid).First(&entry).Error)

	return entry
}

// publishDispatches drains the outbox through a stub transport, so a test can
// reach the state "the dispatch was published and the message is gone" without a
// broker.
func publishDispatches(t *testing.T, db *gorm.DB, sequence uint64) {
	t.Helper()

	relay := urth.NewDispatchRelay(urth.NewDispatchOutbox(db), &recordingPublisher{sequence: sequence})
	_, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
}

// A worker that acknowledged its message and then died leaves a Result nothing
// else will ever move: it acked, so the broker will not redeliver, and it is
// gone, so it will not report. Without the lease being acted on, that Result
// reads as a run still in progress for as long as the database keeps it.
func TestReconcilerExpiresRunAbandonedAfterClaim(t *testing.T) {
	keys := testKeys(t)
	// A one-millisecond ceiling on run duration, so the lease the claim records
	// is already in the past by the time the scan looks at it. The alternative --
	// writing a stale deadline straight into the row -- would prove only that the
	// query matches a value the test chose.
	srv, db, store := newTestService(t, &stubScheduler{},
		urth.WithSigningKeys(keys),
		urth.WithMaxRunDuration(time.Millisecond),
	)
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	claim := claimRun(t, srv, created)
	require.False(t, claim.Deadline.IsZero(), "a claim must record an execution lease")

	running := loadResult(t, store, created.UID)
	require.Equal(t, urth.JobRunning, running.Status.Status)

	// The worker dies here: no status update will ever arrive.
	time.Sleep(5 * time.Millisecond)

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithExecutionLeaseGrace(0),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ExpiredRunning)
	require.Zero(t, report.Failures)
	require.Positive(t, report.OldestInconsistent)

	expired := loadResult(t, store, created.UID)
	require.Equal(t, urth.JobExpired, expired.Status.Status)
	require.Equal(t, prob.RunFinishedTimeout, expired.Status.Result)
	require.NotNil(t, expired.Spec.TimeEnded)
	require.Equal(t, string(urth.JobExpired), expired.Labels[urth.LabelResultJobState])

	// The attempt is history, not something to undo: the executor recorded at
	// claim time survives the expiry, because "which worker was holding this when
	// it died" is the first thing anyone diagnosing it will ask.
	require.Equal(t, running.Status.Executor, expired.Status.Executor)
}

// An expired Result is immutable history. A reconciler that reopened one -- or
// expired it a second time, bumping its version under whatever is reading it --
// would be rewriting the record of an attempt that really happened.
func TestReconcilerDoesNotReopenAnExpiredRun(t *testing.T) {
	keys := testKeys(t)
	srv, db, store := newTestService(t, &stubScheduler{},
		urth.WithSigningKeys(keys),
		urth.WithMaxRunDuration(time.Millisecond),
	)
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)
	claimRun(t, srv, created)
	time.Sleep(5 * time.Millisecond)

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithExecutionLeaseGrace(0),
	)

	first, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, first.ExpiredRunning)

	settled := loadResult(t, store, created.UID)

	second, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, second.ExpiredRunning)
	require.Zero(t, second.Redispatched, "an expired run must not be dispatched again")

	unchanged := loadResult(t, store, created.UID)
	require.Equal(t, settled.Version, unchanged.Version, "a second scan must not touch a settled run")
	require.Equal(t, urth.JobExpired, unchanged.Status.Status)
}

// A Result that finished is not the reconciler's to rewrite. The version guard
// settles a race, but it would admit an expiry written over a run that was
// already `completed` when the scan read it -- turning a run that succeeded into
// one that timed out.
func TestExpireRunRefusesATerminalResult(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	finished := loadResult(t, store, created.UID)
	finished.Status.Status = urth.JobCompleted
	finished.Status.Result = prob.RunFinishedSuccess
	_, err = store.CreateOrUpdate(ctx, &finished)
	require.NoError(t, err)

	settled := loadResult(t, store, created.UID)

	expired, err := urth.NewReconcileStore(db, store).
		ExpireRun(ctx, settled, time.Now(), "lease expired")
	require.NoError(t, err)
	require.False(t, expired)

	unchanged := loadResult(t, store, created.UID)
	require.Equal(t, urth.JobCompleted, unchanged.Status.Status)
	require.Equal(t, prob.RunFinishedSuccess, unchanged.Status.Result)
	require.Equal(t, settled.Version, unchanged.Version)
}

// A dispatch the broker accepted and then aged out at MaxJobAge leaves a Result
// pending with nothing left to deliver it. Nobody else notices: the outbox row
// says published, the Result says pending, and both are telling the truth.
func TestReconcilerExpiresPendingRunWhoseDispatchWasLost(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	publishDispatches(t, db, 0)
	backdateCreation(t, db, created.UID, time.Now().Add(-3*time.Hour))

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithPendingDispatchTimeout(time.Hour),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ExpiredPending)
	require.Zero(t, report.Failures)

	expired := loadResult(t, store, created.UID)
	require.Equal(t, urth.JobExpired, expired.Status.Status)
	require.Equal(t, prob.RunFinishedTimeout, expired.Status.Result)
}

// The reconciler must not expire a run whose dispatch has simply not been
// published yet. An unpublished outbox row is the relay's work in progress, and
// a broker that has been down for two hours is exactly the case where both the
// backlog and the pending Results are old -- treating that as a lost dispatch
// would expire every run the relay is about to deliver.
func TestReconcilerLeavesUnpublishedDispatchesToTheRelay(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)
	backdateCreation(t, db, created.UID, time.Now().Add(-3*time.Hour))

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithPendingDispatchTimeout(time.Hour),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, report.ExpiredPending, "an unpublished dispatch is not a lost one")
	require.Zero(t, report.Redispatched, "the entry is already there; writing another would double-dispatch")

	waiting := loadResult(t, store, created.UID)
	require.Equal(t, urth.JobPending, waiting.Status.Status)

	// Once the relay does its job, the same Result becomes expirable.
	publishDispatches(t, db, 0)

	report, err = reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ExpiredPending)
}

// A dispatch removed administratively -- a truncated table, a restore from a
// backup taken before the run was requested -- leaves an authoritative Result
// that still wants to run and nothing queued to run it. The Result is the
// authority, so the entry is written again rather than the run being abandoned.
func TestReconcilerReEnqueuesAMissingDispatch(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	require.NoError(t, db.Where("result_uid = ?", created.UID).
		Delete(&urth.DispatchOutboxEntry{}).Error)
	backdateCreation(t, db, created.UID, time.Now().Add(-3*time.Hour))

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithPendingDispatchTimeout(time.Hour),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.Redispatched)

	restored := loadDispatch(t, db, created.UID)
	require.Equal(t, urth.DispatchEventUID(created.UID, created.Version), restored.EventUID,
		"the replacement must carry the same event UID, or the broker cannot deduplicate a republication")
	require.Nil(t, restored.PublishedAt)

	require.Equal(t, urth.JobPending, loadResult(t, store, created.UID).Status.Status,
		"a run being re-dispatched has not failed")

	// Repeating the scan must not queue it a second time.
	second, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, second.Redispatched)

	var entries int64
	require.NoError(t, db.Model(&urth.DispatchOutboxEntry{}).
		Where("result_uid = ?", created.UID).Count(&entries).Error)
	require.EqualValues(t, 1, entries)
}

// A reconciler that expires a Result and dies before withdrawing its queued
// message leaves the broker holding work for a run nothing can execute. The next
// scan has to finish the job from the state it finds, without being told a crash
// happened.
func TestReconcilerConvergesAfterCrashBeforeCleanup(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	// Published at a known sequence, so the withdrawal can be checked to name
	// the message that was actually queued.
	const sequence = uint64(42)
	publishDispatches(t, db, sequence)
	backdateCreation(t, db, created.UID, time.Now().Add(-3*time.Hour))

	reconcileStore := urth.NewReconcileStore(db, store)

	// The crash: the transition lands, the cleanup never runs. Calling the store
	// directly is the whole point -- it reproduces a half-finished scan rather
	// than a scan that was asked to do half the work.
	expired, err := reconcileStore.ExpireRun(ctx, loadResult(t, store, created.UID), time.Now(), "lease expired")
	require.NoError(t, err)
	require.True(t, expired)

	stranded := loadDispatch(t, db, created.UID)
	require.Nil(t, stranded.RetiredAt, "the crash is before cleanup, so the entry is still live")

	channels := newFakeChannels()
	reconciler := urth.NewReconciler(reconcileStore,
		urth.WithPendingDispatchTimeout(time.Hour),
		urth.WithRunnerChannels(channels),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.RetiredDispatches)
	require.Equal(t, 1, report.DroppedMessages)
	require.Equal(t, []uint64{sequence}, channels.droppedSeqs())

	settled := loadDispatch(t, db, created.UID)
	require.NotNil(t, settled.RetiredAt)
	require.Contains(t, settled.RetiredReason, "terminal")

	// And it converges: a third scan finds nothing left to do.
	final, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, final.Repaired())
}

// A dispatch the Result was claimed through has already been acknowledged by the
// worker that claimed it, so there is no message to withdraw. Asking the broker
// to delete one would be a pointless round trip per completed run -- which is
// every run, on a healthy system.
func TestReconcilerRetiresDeliveredDispatchWithoutTouchingTheBroker(t *testing.T) {
	keys := testKeys(t)
	srv, db, store := newTestService(t, &stubScheduler{},
		urth.WithSigningKeys(keys),
		urth.WithMaxRunDuration(time.Millisecond),
	)
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	publishDispatches(t, db, 7)
	claimRun(t, srv, created)
	time.Sleep(5 * time.Millisecond)

	channels := newFakeChannels()
	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithExecutionLeaseGrace(0),
		urth.WithRunnerChannels(channels),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ExpiredRunning)
	require.Equal(t, 1, report.RetiredDispatches)
	require.Zero(t, report.DroppedMessages)
	require.Empty(t, channels.droppedSeqs(),
		"the claim proves the message was acknowledged; there is nothing to withdraw")
}

// A retired entry must leave the relay's view. Otherwise the reconciler decides
// a dispatch will never be published and the relay publishes it anyway, putting
// a job on a queue for a run that is already terminal.
func TestRetiredDispatchIsNotRelayed(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)
	backdateCreation(t, db, created.UID, time.Now().Add(-3*time.Hour))

	// Expiring a pending run retires the dispatch that would have started it, in
	// the same transaction.
	reconcileStore := urth.NewReconcileStore(db, store)
	expired, err := reconcileStore.ExpireRun(ctx, loadResult(t, store, created.UID), time.Now(), "lost dispatch")
	require.NoError(t, err)
	require.True(t, expired)

	retired := loadDispatch(t, db, created.UID)
	require.NotNil(t, retired.RetiredAt)

	outbox := urth.NewDispatchOutbox(db)
	claimed, err := outbox.Claim(ctx, "relay-a", 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, claimed, "a retired entry must never be offered to a relay")

	stats, err := outbox.Stats(ctx, time.Now())
	require.NoError(t, err)
	require.Zero(t, stats.Pending, "a retired entry is not backlog")
}

// A relay that died mid-publication leaves its name on the row. The relay's own
// claim steps over the expired lease, so the backlog still drains -- what is
// broken is the record, which tells an operator hunting a stuck dispatch that a
// relay is working on it.
func TestReconcilerReleasesAbandonedDispatchLeases(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	_, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	// A lease that expired the moment it was taken: the relay that took it is
	// gone.
	claimed, err := urth.NewDispatchOutbox(db).Claim(ctx, "relay-that-died", 10, -time.Second)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store))

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.ReleasedLeases)

	var entry urth.DispatchOutboxEntry
	require.NoError(t, db.First(&entry, claimed[0].ID).Error)
	require.Empty(t, entry.ClaimedBy)
	require.Nil(t, entry.ClaimExpiresAt)

	// Idempotent: nothing left to release on the next pass.
	second, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, second.ReleasedLeases)
}

// Two reconcilers reaching for the same Result is the normal case, not an exotic
// one: every API replica runs one. The version guard is what makes that safe,
// and it has to be a property of the write rather than of the scan that found
// the candidate.
func TestConcurrentReconcilersExpireARunExactlyOnce(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)

	const racers = 4

	// Each racer loads the Result for itself, before any of them writes: that is
	// what separate scans in separate processes produce, and sharing one loaded
	// value between them would not be the same test -- gorm writes update times
	// back through the pointers in ObjectMeta, so the copies would alias.
	candidates := make([]urth.Result, 0, racers)
	for range racers {
		candidates = append(candidates, loadResult(t, store, created.UID))
	}

	startingVersion := candidates[0].Version

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		winners int
	)

	for _, candidate := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// A store per goroutine, as separate processes would have.
			expired, err := urth.NewReconcileStore(db, store).
				ExpireRun(ctx, candidate, time.Now(), "lease expired")

			mu.Lock()
			defer mu.Unlock()

			require.NoError(t, err)
			if expired {
				winners++
			}
		}()
	}

	wg.Wait()

	require.Equal(t, 1, winners, "the version guard must admit exactly one transition")
	require.Equal(t, startingVersion+1, loadResult(t, store, created.UID).Version)
}

// The scan lease keeps replicas from repeating one another's work. It is not
// what makes concurrency safe -- the version guard is -- but without it every
// replica pays for every candidate to discover it lost.
func TestReconcileScanLeaseAdmitsOneScannerAtATime(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})

	ctx := context.Background()
	reconcileStore := urth.NewReconcileStore(db, store)

	held, err := reconcileStore.AcquireScanLease(ctx, "reconciler-a", time.Minute)
	require.NoError(t, err)
	require.True(t, held)

	blocked, err := reconcileStore.AcquireScanLease(ctx, "reconciler-b", time.Minute)
	require.NoError(t, err)
	require.False(t, blocked, "a live lease must not be handed to a second reconciler")

	// A reconciler that dies holding the lease must not stop reconciliation
	// until someone notices.
	require.NoError(t, reconcileStore.ReleaseScanLease(ctx, "reconciler-a"))

	tookOver, err := reconcileStore.AcquireScanLease(ctx, "reconciler-b", time.Minute)
	require.NoError(t, err)
	require.True(t, tookOver)

	// And a release by a reconciler that no longer holds it is ignored, or the
	// loser of a takeover would evict the winner on its way out.
	require.NoError(t, reconcileStore.ReleaseScanLease(ctx, "reconciler-a"))

	stillBlocked, err := reconcileStore.AcquireScanLease(ctx, "reconciler-c", time.Minute)
	require.NoError(t, err)
	require.False(t, stillBlocked)
}

// A scan that cannot get the lease reports so rather than pretending it repaired
// nothing, so a run of quiet scans in a multi-replica deployment is not mistaken
// for a healthy system with nothing to fix.
func TestReconcilerSkipsWhenAnotherHoldsTheLease(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})

	ctx := context.Background()
	reconcileStore := urth.NewReconcileStore(db, store)

	held, err := reconcileStore.AcquireScanLease(ctx, "someone-else", time.Minute)
	require.NoError(t, err)
	require.True(t, held)

	report, err := urth.NewReconciler(reconcileStore).RunOnce(ctx)
	require.NoError(t, err)
	require.True(t, report.Skipped)
	require.Zero(t, report.Repaired())
}

// A runner whose consumer was deleted accepts dispatches and delivers none of
// them. Its workers cannot fix that -- ADR 0004 gives them no administration
// rights, deliberately -- so the control plane has to notice.
func TestReconcilerRestoresMissingRunnerChannels(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)

	ctx := context.Background()

	var runner urth.Runner
	found, err := store.GetByName(ctx, &runner, "test-runner")
	require.NoError(t, err)
	require.True(t, found)

	disabled := urth.Runner{
		ObjectMeta: manifest.ObjectMeta{Name: "disabled-runner"},
		Spec:       urth.RunnerSpec{IsActive: false},
	}
	require.NoError(t, store.Create(ctx, &disabled))

	channels := newFakeChannels(runner.UID)
	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithRunnerChannels(channels),
	)

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.RestoredChannels)
	require.Equal(t, []manifest.ResourceID{runner.UID}, channels.ensured,
		"a disabled runner has no queue to keep alive")

	// Restored once; a healthy queue is not reported as repaired every minute.
	second, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, second.RestoredChannels)
}

// One failing pass must not cancel the others. An unreachable broker is no
// reason to leave every expired execution lease in place, and a scan that
// stopped at the first error would do exactly that.
func TestReconcilerContinuesPastAFailedPass(t *testing.T) {
	keys := testKeys(t)
	srv, db, store := newTestService(t, &stubScheduler{},
		urth.WithSigningKeys(keys),
		urth.WithMaxRunDuration(time.Millisecond),
	)
	scenarioName := seedScenario(t, store)

	ctx := context.Background()

	created, err := srv.Results(scenarioName).Create(ctx, newRunRequest())
	require.NoError(t, err)
	claimRun(t, srv, created)
	time.Sleep(5 * time.Millisecond)

	channels := newFakeChannels()
	channels.ensureErr = errors.New("nats: no servers available for connection")

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithExecutionLeaseGrace(0),
		urth.WithRunnerChannels(channels),
	)

	report, err := reconciler.RunOnce(ctx)
	require.Error(t, err, "the failure is reported")
	require.Equal(t, 1, report.ExpiredRunning, "and the work that could be done was done")
	require.Equal(t, 1, report.Failures)

	require.Equal(t, urth.JobExpired, loadResult(t, store, created.UID).Status.Status)

	// A scan that failed does not count as the last successful one, or the age
	// an operator watches would reset on every broken scan.
	require.True(t, reconciler.Status().LastSuccessAt.IsZero())
}

// The reconciler's own health is separate from what any scan found: a scan age
// that keeps growing is the signal that reconciliation has stopped, which no
// individual report can show.
func TestReconcilerReportsItsOwnHealth(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store))
	require.True(t, reconciler.Status().LastSuccessAt.IsZero())

	_, err := reconciler.RunOnce(context.Background())
	require.NoError(t, err)

	status := reconciler.Status()
	require.False(t, status.LastSuccessAt.IsZero())
	require.Less(t, status.ScanAge, time.Minute)
	require.Zero(t, status.Last.Failures)
}
