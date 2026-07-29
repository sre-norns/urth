package urth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Presence is written straight to its columns, and these tests are why.
//
// They run on Postgres because that is where the schema lives -- the resource
// tables cannot be migrated on SQLite, for the wyrd `idx_name` reason recorded
// in CLAUDE.md.

// seedWorker registers one worker against one runner and returns both.
func seedWorker(t *testing.T, store *dbstore.DBStore) (urth.Runner, urth.WorkerInstance) {
	t.Helper()

	ctx := context.Background()

	runner := urth.Runner{
		ObjectMeta: manifest.ObjectMeta{Name: "presence-runner"},
		Spec:       urth.RunnerSpec{IsActive: true},
	}
	require.NoError(t, store.Create(ctx, &runner))

	worker := urth.WorkerInstance{
		ObjectMeta: manifest.ObjectMeta{Name: "presence-worker"},
		Spec:       urth.WorkerInstanceSpec{RunnerID: runner.UID},
	}
	require.NoError(t, store.Create(ctx, &worker))

	return runner, worker
}

func reloadWorker(t *testing.T, store *dbstore.DBStore, uid manifest.ResourceID) urth.WorkerInstance {
	t.Helper()

	var worker urth.WorkerInstance
	found, err := store.GetByUID(context.Background(), &worker, uid)
	require.NoError(t, err)
	require.True(t, found)

	return worker
}

// Recording presence must not touch the resource version.
//
// This is the whole reason presence bypasses dbstore. wyrd's
// ObjectMeta.BeforeSave increments Version on every gorm Save, so a heartbeat
// routed through CreateOrUpdate -- the correct path for a resource *edit* --
// would bump the version every interval for as long as the worker lived. Two
// things break as a result: the version stops meaning "this record was edited",
// and the version-guarded delete behind the UI's Drop button starts failing
// against a version that was current a minute ago.
//
// The second half of this test is that consequence, asserted directly.
func TestRecordingPresenceDoesNotBumpTheResourceVersion(t *testing.T) {
	_, db, store := newTestService(t, nil)
	_, worker := seedWorker(t, store)

	presence := urth.NewWorkerPresenceStore(db)
	ctx := context.Background()

	before := reloadWorker(t, store, worker.UID)

	for range 5 {
		found, err := presence.RecordContact(ctx, worker.UID, time.Now(), urth.WorkerContactHeartbeat, false)
		require.NoError(t, err)
		require.True(t, found)
	}

	after := reloadWorker(t, store, worker.UID)
	require.Equal(t, before.Version, after.Version,
		"reporting liveness is not editing the resource and must not bump its version")
	require.NotNil(t, after.Status.LastSeenTime, "the timestamp itself must still be written")

	// A client holding the version it fetched before those heartbeats can still
	// drop the worker. This is the Drop button.
	deleted, err := store.Delete(ctx, &urth.WorkerInstance{}, worker.UID, before.Version)
	require.NoError(t, err)
	require.True(t, deleted, "a version-guarded delete must survive intervening heartbeats")
}

// Nor may it move updated_at.
//
// This is what separates UpdateColumns from Updates -- both leave the version
// alone when given a map, but Updates refreshes updated_at. A heartbeat is not a
// modification of the record, and letting every report touch that column would
// destroy the answer to "when was this worker last actually changed".
func TestRecordingPresenceDoesNotLookLikeAnEdit(t *testing.T) {
	_, db, store := newTestService(t, nil)
	_, worker := seedWorker(t, store)

	presence := urth.NewWorkerPresenceStore(db)

	before := reloadWorker(t, store, worker.UID)
	require.NotNil(t, before.UpdatedAt)

	// Postgres timestamps here have sub-second resolution, but sleeping past a
	// whole second makes a failure unmistakable rather than a rounding argument.
	time.Sleep(1100 * time.Millisecond)

	_, err := presence.RecordContact(context.Background(), worker.UID, time.Now(), urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)

	after := reloadWorker(t, store, worker.UID)
	require.NotNil(t, after.UpdatedAt)
	require.True(t, after.UpdatedAt.Equal(*before.UpdatedAt),
		"reporting liveness must not register as a modification of the record")
}

// The two signals are stored separately so that one can move while the other
// does not. If a heartbeat also refreshed the NATS timestamp, or vice versa, the
// half-connected cases would be unobservable and the whole diagnosis lost.
func TestPresenceSignalsAreRecordedIndependently(t *testing.T) {
	_, db, store := newTestService(t, nil)
	runner, worker := seedWorker(t, store)

	presence := urth.NewWorkerPresenceStore(db)
	ctx := context.Background()

	_, err := presence.RecordContact(ctx, worker.UID, time.Now(), urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)

	afterHeartbeat := reloadWorker(t, store, worker.UID)
	require.NotNil(t, afterHeartbeat.Status.LastSeenTime)
	require.Equal(t, urth.WorkerContactHeartbeat, afterHeartbeat.Status.LastSeenVia)
	require.Nil(t, afterHeartbeat.Status.NATSLastSeenTime,
		"a heartbeat says nothing about whether the worker is on its queue")

	_, err = presence.RecordNATSPresence(ctx, worker.UID, runner.UID, time.Now())
	require.NoError(t, err)

	afterAnnounce := reloadWorker(t, store, worker.UID)
	require.NotNil(t, afterAnnounce.Status.NATSLastSeenTime)
	require.Equal(t, afterHeartbeat.Status.LastSeenTime.UnixNano(), afterAnnounce.Status.LastSeenTime.UnixNano(),
		"announcing over NATS must not be mistaken for reaching the API server")
}

// A claim is recorded as its own kind of evidence, so the UI can say "last seen
// 20 seconds ago (claimed a run)" rather than implying a heartbeat arrived.
func TestRecordContactRemembersWhichEvidence(t *testing.T) {
	_, db, store := newTestService(t, nil)
	_, worker := seedWorker(t, store)

	presence := urth.NewWorkerPresenceStore(db)

	_, err := presence.RecordContact(context.Background(), worker.UID, time.Now(), urth.WorkerContactClaim, false)
	require.NoError(t, err)

	require.Equal(t, urth.WorkerContactClaim, reloadWorker(t, store, worker.UID).Status.LastSeenVia)
}

// Leaving is recorded, and any later contact clears it.
//
// The clearing matters more than it looks: `nil` is a zero value, and the
// dbstore.Update path would silently drop it -- the trap that once made
// disabling a scenario a no-op. A worker that restarted would then stay marked
// as having left, and would read offline while running.
func TestLeavingIsRecordedAndClearedOnReturn(t *testing.T) {
	_, db, store := newTestService(t, nil)
	_, worker := seedWorker(t, store)

	presence := urth.NewWorkerPresenceStore(db)
	ctx := context.Background()

	_, err := presence.RecordContact(ctx, worker.UID, time.Now(), urth.WorkerContactHeartbeat, true)
	require.NoError(t, err)
	require.NotNil(t, reloadWorker(t, store, worker.UID).Status.LeftAt)

	_, err = presence.RecordContact(ctx, worker.UID, time.Now(), urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)
	require.Nil(t, reloadWorker(t, store, worker.UID).Status.LeftAt,
		"a worker that came back must not stay marked as having left")
}

// Broker permissions stop one runner's workers speaking for another's, but say
// nothing about a worker naming a sibling under the same prefix. The write
// requires the membership so that announcing presence for another worker does
// nothing.
func TestNATSPresenceRequiresRunnerMembership(t *testing.T) {
	_, db, store := newTestService(t, nil)
	_, worker := seedWorker(t, store)

	presence := urth.NewWorkerPresenceStore(db)

	otherRunner := manifest.ResourceID(uuid.NewString())
	found, err := presence.RecordNATSPresence(context.Background(), worker.UID, otherRunner, time.Now())
	require.NoError(t, err)
	require.False(t, found, "an announcement from the wrong runner must not land")
	require.Nil(t, reloadWorker(t, store, worker.UID).Status.NATSLastSeenTime)
}

// A worker whose registration was dropped must learn so, rather than reporting
// into nothing for the rest of its life. Reported as "not found" here, which the
// API turns into a 404 and the worker turns into a re-registration.
func TestRecordingPresenceReportsAMissingRegistration(t *testing.T) {
	_, db, _ := newTestService(t, nil)

	presence := urth.NewWorkerPresenceStore(db)

	found, err := presence.RecordContact(context.Background(), "00000000-0000-0000-0000-000000000000",
		time.Now(), urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)
	require.False(t, found)
}

// Silence is only actionable from a worker that was once heard.
//
// The reconciler's candidate query has to agree with WorkerPresenceAt about
// that, or it evicts registrations the UI is still reporting as `unknown`.
func TestSilentWorkersSkipsWorkersThatNeverReported(t *testing.T) {
	_, db, store := newTestService(t, nil)
	runner, heard := seedWorker(t, store)

	ctx := context.Background()

	// A second worker of the same runner that has never reported anything --
	// the asynq prototype, or a record written before liveness reporting.
	silent := urth.WorkerInstance{
		ObjectMeta: manifest.ObjectMeta{Name: "never-reported"},
		Spec:       urth.WorkerInstanceSpec{RunnerID: runner.UID},
	}
	require.NoError(t, store.Create(ctx, &silent))

	// And a third that is still announcing itself over NATS while its route to
	// the API server has been broken for a day.
	halfConnected := urth.WorkerInstance{
		ObjectMeta: manifest.ObjectMeta{Name: "api-unreachable"},
		Spec:       urth.WorkerInstanceSpec{RunnerID: runner.UID},
	}
	require.NoError(t, store.Create(ctx, &halfConnected))

	presence := urth.NewWorkerPresenceStore(db)
	longAgo := time.Now().Add(-48 * time.Hour)

	_, err := presence.RecordContact(ctx, heard.UID, longAgo, urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)
	_, err = presence.RecordContact(ctx, halfConnected.UID, longAgo, urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)
	_, err = presence.RecordNATSPresence(ctx, halfConnected.UID, runner.UID, time.Now())
	require.NoError(t, err)

	reconcile := urth.NewReconcileStore(db, store)
	candidates, err := reconcile.SilentWorkers(ctx, time.Now().Add(-24*time.Hour), 100)
	require.NoError(t, err)

	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, string(candidate.Name))
	}

	require.Equal(t, []string{"presence-worker"}, names,
		"only a worker once heard and now silent on every signal may be evicted")
}

// The reconciler drops exactly the registrations its query found, and nothing
// else about the runner.
func TestReconcilerEvictsSilentWorkers(t *testing.T) {
	_, db, store := newTestService(t, nil)
	runner, worker := seedWorker(t, store)

	ctx := context.Background()
	presence := urth.NewWorkerPresenceStore(db)

	_, err := presence.RecordContact(ctx, worker.UID, time.Now().Add(-48*time.Hour), urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store),
		urth.WithWorkerRetention(24*time.Hour))

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, report.EvictedWorkers)

	found, err := store.GetByUID(ctx, &urth.WorkerInstance{}, worker.UID)
	require.NoError(t, err)
	require.False(t, found, "the registration is gone")

	// Its runner is untouched: a worker disappearing does not decommission the
	// channel it was serving, nor anything queued on it.
	found, err = store.GetByUID(ctx, &urth.Runner{}, runner.UID)
	require.NoError(t, err)
	require.True(t, found)
}

// Retention of zero turns eviction off, for a deployment that would rather keep
// every registration and drop them by hand.
func TestWorkerEvictionCanBeDisabled(t *testing.T) {
	_, db, store := newTestService(t, nil)
	_, worker := seedWorker(t, store)

	ctx := context.Background()
	presence := urth.NewWorkerPresenceStore(db)

	_, err := presence.RecordContact(ctx, worker.UID, time.Now().Add(-48*time.Hour), urth.WorkerContactHeartbeat, false)
	require.NoError(t, err)

	reconciler := urth.NewReconciler(urth.NewReconcileStore(db, store), urth.WithWorkerRetention(0))

	report, err := reconciler.RunOnce(ctx)
	require.NoError(t, err)
	require.Zero(t, report.EvictedWorkers)

	found, err := store.GetByUID(ctx, &urth.WorkerInstance{}, worker.UID)
	require.NoError(t, err)
	require.True(t, found)
}

// Keeps the compiler honest about the gorm handle being the one the store uses.
var _ = func(db *gorm.DB) urth.WorkerPresenceStore { return urth.NewWorkerPresenceStore(db) }
