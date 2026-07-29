package urth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Reconciler defaults.
//
// The interval is the resolution at which a stuck run becomes visible, not a
// throughput knob: nothing here is on the latency path of a healthy run, and a
// scan that finds nothing costs a handful of indexed queries.
const (
	// DefaultReconcileInterval is how often a reconciler attempts a scan.
	DefaultReconcileInterval = 1 * time.Minute

	// DefaultReconcileLease is how long one scan may hold the right to run.
	// It must exceed a scan's duration, or a second reconciler starts the same
	// work while the first is still in it -- safe, because every transition is
	// version-guarded, but wasted.
	DefaultReconcileLease = 5 * time.Minute

	// DefaultReconcileBatchSize bounds how much one scan repairs. A backlog is
	// drained over several scans rather than in one transaction that holds
	// connections for as long as the backlog is deep.
	DefaultReconcileBatchSize = 256

	// DefaultPendingDispatchTimeout is how long a Result may sit pending with a
	// published dispatch before the dispatch is presumed gone.
	//
	// It has to exceed the transport's own message expiry -- JetStream's
	// `MaxJobAge`, an hour by default -- or the reconciler expires runs whose
	// messages are still queued and claimable. The composition passes the
	// configured value; this default is that value plus room for a queue that is
	// merely slow.
	DefaultPendingDispatchTimeout = 90 * time.Minute

	// ReconcileScanLeaseName names the lease row guarding the scan.
	ReconcileScanLeaseName = "dispatch-reconcile"
)

// ReconcileLease is the row that stops concurrent reconcilers repeating one
// another's scan.
//
// It is an efficiency measure rather than a correctness one -- every transition
// a scan makes is guarded by the Result's version, so two scans racing produce
// one winner and one no-op either way. What the lease buys is that the losing
// reconciler does not spend a database round trip per candidate discovering
// that.
//
// Time columns carry no explicit `type:` tag, for the reason given on
// DispatchOutboxEntry: gorm's Postgres driver already maps time.Time to
// timestamptz, and naming it forces it on SQLite where the driver cannot scan
// the result.
type ReconcileLease struct {
	Name      string `gorm:"primaryKey;size:128"`
	Holder    string `gorm:"not null"`
	ExpiresAt time.Time
	UpdatedAt time.Time
}

// TableName keeps the table out of gorm's pluralisation guesswork.
func (ReconcileLease) TableName() string {
	return "reconcile_leases"
}

// IsTerminal reports whether a job has finished, one way or another.
//
// A terminal Result is immutable history: no dispatch will start it, no claim
// will take it, and anything that rewrote one would be altering the record of
// what actually happened. Retry, when there is a policy for it, creates a new
// Result instead.
func (s JobStatus) IsTerminal() bool {
	return slices.Contains(TerminalJobStates(), s)
}

// TerminalJobStates lists the states a Result never leaves.
func TerminalJobStates() []JobStatus {
	return []JobStatus{JobCompleted, JobErrored, JobExpired}
}

// PendingRun pairs a Result that has not started with the dispatch written for
// it, so the reconciler can tell "nobody has published this yet" from "it was
// published and the message is gone".
//
// The distinction is the whole point of consulting the outbox here. A pending
// Result whose entry is still unpublished belongs to the relay, and treating it
// as a lost dispatch would expire a run that is moments from being queued.
type PendingRun struct {
	Result Result

	// Dispatch is the outbox entry written for this Result's current version, or
	// nil when no entry exists at all.
	Dispatch *DispatchOutboxEntry
}

// StaleDispatch is an outbox entry whose Result no longer wants it.
type StaleDispatch struct {
	Entry DispatchOutboxEntry

	// Delivered reports that the Result was claimed through this very dispatch.
	// A claim is only issued to a worker that then acknowledges its message, so
	// there is nothing left in the queue to withdraw -- the entry needs retiring
	// for tidiness, not the broker touching.
	Delivered bool
}

// ReconcileStore is the reconciler's view of authoritative state.
//
// It is deliberately a separate interface from the resource APIs: everything
// here is a whole-table question ("which runs have outlived their lease") that
// the UID-addressed store cannot express, and none of it is reachable over
// REST.
type ReconcileStore interface {
	// AcquireScanLease claims the right to run one scan, reporting false when
	// another reconciler holds it.
	AcquireScanLease(ctx context.Context, holder string, lease time.Duration) (bool, error)

	// ReleaseScanLease gives the lease up early, so the next scan need not wait
	// out a lease this reconciler has finished with.
	ReleaseScanLease(ctx context.Context, holder string) error

	// ExpiredRuns lists running Results whose execution lease elapsed before cutoff.
	ExpiredRuns(ctx context.Context, cutoff time.Time, limit int) ([]Result, error)

	// StalePendingRuns lists Results still pending since before cutoff, each with
	// the outbox entry written for its current version, if there is one.
	StalePendingRuns(ctx context.Context, cutoff time.Time, limit int) ([]PendingRun, error)

	// ExpireRun records a Result as expired and retires any dispatch still
	// waiting to be published for it, in one transaction. It reports false when
	// the Result moved on -- someone claimed or finished it -- between the scan
	// reading it and this write.
	ExpireRun(ctx context.Context, result Result, at time.Time, reason string) (bool, error)

	// EnqueueDispatch writes the missing outbox entry for a pending Result,
	// reporting false when an entry for that event already exists.
	EnqueueDispatch(ctx context.Context, result Result, now time.Time) (bool, error)

	// StaleDispatches lists unretired outbox entries whose Result is terminal,
	// deleted, or gone.
	StaleDispatches(ctx context.Context, limit int) ([]StaleDispatch, error)

	// RetireDispatch marks an entry as one that will never be published.
	RetireDispatch(ctx context.Context, id uint, at time.Time, reason string) error

	// ReleaseAbandonedDispatches clears leases whose relay is gone, returning
	// them to the pool. It reports how many were released.
	ReleaseAbandonedDispatches(ctx context.Context, now time.Time) (int, error)

	// ActiveRunnerUIDs lists the runners that should have a live queue.
	ActiveRunnerUIDs(ctx context.Context) ([]manifest.ResourceID, error)

	// SilentWorkers lists registrations quiet on every liveness signal since
	// before cutoff. Workers that have never reported at all are excluded --
	// they are `unknown`, not offline, and there is no evidence to act on.
	SilentWorkers(ctx context.Context, cutoff time.Time, limit int) ([]WorkerInstance, error)

	// DropWorker revokes one worker's registration.
	DropWorker(ctx context.Context, worker WorkerInstance) (bool, error)
}

// RunnerChannelReconciler is the transport's half of reconciliation.
//
// It is owned here and implemented by transport packages, for the same reason
// DispatchPublisher is: the domain knows a runner must have somewhere to collect
// work and that a withdrawn dispatch must stop being deliverable, and nothing
// about streams or consumers.
type RunnerChannelReconciler interface {
	// EnsureRunnerChannel restores a runner's queue assets, reporting whether
	// anything had to be created.
	//
	// This is the control plane doing it. ADR 0004 reserves asset administration
	// here precisely so a worker that finds no consumer fails loudly instead of
	// creating one that overlaps the real one.
	EnsureRunnerChannel(ctx context.Context, runnerUID manifest.ResourceID) (restored bool, err error)

	// DropDispatch withdraws a published message that is no longer wanted.
	// Withdrawing something already gone is not an error.
	DropDispatch(ctx context.Context, entry DispatchOutboxEntry) error
}

// ReconcileReport is what one scan did, and is the reconciler's observability
// surface: an operator watching a queue that has stopped moving needs to know
// whether anything is scanning at all, and what it found when it last did.
type ReconcileReport struct {
	StartedAt time.Time     `json:"startedAt" yaml:"startedAt"`
	Duration  time.Duration `json:"duration" yaml:"duration"`

	// Skipped reports that another reconciler held the scan lease. A run of
	// skipped scans is normal in a multi-replica deployment and is not a fault.
	Skipped bool `json:"skipped,omitempty" yaml:"skipped,omitempty"`

	// ExpiredRunning counts runs abandoned by a worker that died holding them.
	ExpiredRunning int `json:"expiredRunning" yaml:"expiredRunning"`

	// ExpiredPending counts runs whose dispatch was published and then lost.
	ExpiredPending int `json:"expiredPending" yaml:"expiredPending"`

	// Redispatched counts pending runs whose outbox entry had gone missing and
	// was written again.
	Redispatched int `json:"redispatched" yaml:"redispatched"`

	// RetiredDispatches counts outbox entries retired because their Result is
	// terminal or gone.
	RetiredDispatches int `json:"retiredDispatches" yaml:"retiredDispatches"`

	// DroppedMessages counts queued messages withdrawn from the transport.
	DroppedMessages int `json:"droppedMessages" yaml:"droppedMessages"`

	// ReleasedLeases counts outbox rows taken back from relays that are gone.
	ReleasedLeases int `json:"releasedLeases" yaml:"releasedLeases"`

	// RestoredChannels counts runner queues that had to be recreated.
	RestoredChannels int `json:"restoredChannels" yaml:"restoredChannels"`

	// EvictedWorkers counts registrations dropped after going silent on every
	// liveness signal for longer than the retention window.
	EvictedWorkers int `json:"evictedWorkers" yaml:"evictedWorkers"`

	// Failures counts repairs that were attempted and did not land. A scan
	// continues past them: one unreachable broker is no reason to leave every
	// expired lease in place.
	Failures int `json:"failures" yaml:"failures"`

	// OldestInconsistent is the age of the oldest Result found needing repair.
	// It is the number to alert on -- it stays near zero while the reconciler
	// keeps up, however much it is repairing.
	OldestInconsistent time.Duration `json:"oldestInconsistent" yaml:"oldestInconsistent"`
}

// Repaired reports how many inconsistencies this scan resolved.
func (r ReconcileReport) Repaired() int {
	return r.ExpiredRunning + r.ExpiredPending + r.Redispatched +
		r.RetiredDispatches + r.DroppedMessages + r.ReleasedLeases + r.RestoredChannels +
		r.EvictedWorkers
}

// ReconcileStatus is the reconciler's own health, as distinct from what any one
// scan found. A scan age that keeps growing is the signal that reconciliation
// has stopped, which no individual report can show.
type ReconcileStatus struct {
	// LastSuccessAt is when a scan last completed without failures. It is zero
	// until one has, which is itself the answer to "is anything reconciling".
	LastSuccessAt time.Time `json:"lastSuccessAt" yaml:"lastSuccessAt"`

	// ScanAge is how long it has been since that scan.
	ScanAge time.Duration `json:"scanAge" yaml:"scanAge"`

	// Last is the most recent scan, successful or not.
	Last ReconcileReport `json:"last" yaml:"last"`
}

// Reconciler repairs drift between authoritative Results and transport state.
//
// The lease and the outbox are records of intent; on their own they only make
// drift *detectable*. This is what acts on them: without it a worker that dies
// mid-run leaves a Result running forever, and a job the broker aged out leaves
// one pending forever. Both look, to everything else in the system, exactly like
// work still in progress.
//
// Every repair is idempotent and version-guarded, so running several
// reconcilers, re-running one after a crash, or running one against a state that
// has already been repaired are all safe.
type Reconciler struct {
	store    ReconcileStore
	channels RunnerChannelReconciler

	holder         string
	interval       time.Duration
	lease          time.Duration
	batchSize       int
	pendingTimeout  time.Duration
	leaseGrace      time.Duration
	workerRetention time.Duration

	mu          sync.Mutex
	last        ReconcileReport
	lastSuccess time.Time
}

// ReconcilerOption configures a Reconciler.
type ReconcilerOption func(*Reconciler)

// WithReconcilerID names this reconciler in the scan lease it takes.
func WithReconcilerID(value string) ReconcilerOption {
	return func(r *Reconciler) { r.holder = value }
}

// WithReconcileInterval sets how often a scan is attempted.
func WithReconcileInterval(value time.Duration) ReconcilerOption {
	return func(r *Reconciler) { r.interval = value }
}

// WithReconcileLease sets how long one scan holds the right to run.
func WithReconcileLease(value time.Duration) ReconcilerOption {
	return func(r *Reconciler) { r.lease = value }
}

// WithReconcileBatchSize bounds how much one scan repairs.
func WithReconcileBatchSize(value int) ReconcilerOption {
	return func(r *Reconciler) { r.batchSize = value }
}

// WithPendingDispatchTimeout sets how long a pending Result with a published
// dispatch is given before the dispatch is presumed lost. It must exceed the
// transport's own message expiry.
func WithPendingDispatchTimeout(value time.Duration) ReconcilerOption {
	return func(r *Reconciler) { r.pendingTimeout = value }
}

// WithExecutionLeaseGrace sets how long past its deadline a running Result is
// left alone.
func WithExecutionLeaseGrace(value time.Duration) ReconcilerOption {
	return func(r *Reconciler) { r.leaseGrace = value }
}

// WithWorkerRetention sets how long a worker silent on every liveness signal is
// kept before its registration is dropped. Zero disables eviction, which leaves
// dead registrations listed forever -- tolerable where worker names are stable,
// less so where every restart mints a new one.
func WithWorkerRetention(value time.Duration) ReconcilerOption {
	return func(r *Reconciler) { r.workerRetention = value }
}

// WithRunnerChannels gives the reconciler the transport's half. Without it,
// runner queues and withdrawn messages are left alone -- which is the right
// behaviour for a transport that has no such notion, not a degraded mode.
func WithRunnerChannels(value RunnerChannelReconciler) ReconcilerOption {
	return func(r *Reconciler) { r.channels = value }
}

// NewReconciler builds a reconciler over authoritative state.
func NewReconciler(store ReconcileStore, options ...ReconcilerOption) *Reconciler {
	reconciler := &Reconciler{
		store:          store,
		holder:         fmt.Sprintf("reconciler-%s", NewRandToken(8)),
		interval:        DefaultReconcileInterval,
		lease:           DefaultReconcileLease,
		batchSize:       DefaultReconcileBatchSize,
		pendingTimeout:  DefaultPendingDispatchTimeout,
		workerRetention: DefaultWorkerRetention,
		// The capability a worker holds outlives its deadline by this much, so
		// that a run using its whole budget can still report. Expiring at the
		// deadline itself would bump the Result's version out from under an
		// upload that is still permitted and still on its way.
		leaseGrace: artifactUploadGrace,
	}

	for _, option := range options {
		option(reconciler)
	}

	return reconciler
}

// Status reports the reconciler's own health.
func (r *Reconciler) Status() ReconcileStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	status := ReconcileStatus{
		LastSuccessAt: r.lastSuccess,
		Last:          r.last,
	}
	if !r.lastSuccess.IsZero() {
		status.ScanAge = time.Since(r.lastSuccess)
	}

	return status
}

// RunOnce performs one scan, returning what it repaired.
//
// Exposed separately from Run so tests drive it deterministically rather than
// racing a ticker. The error is advisory: it reports what went wrong, while the
// report says what nonetheless got fixed. A scan that hits one failure carries
// on with the rest, because the passes are independent and stopping at the first
// one would let a single unreachable broker block every lease expiry.
func (r *Reconciler) RunOnce(ctx context.Context) (ReconcileReport, error) {
	report := ReconcileReport{StartedAt: time.Now()}

	held, err := r.store.AcquireScanLease(ctx, r.holder, r.lease)
	if err != nil {
		report.Duration = time.Since(report.StartedAt)
		report.Failures++
		r.record(report)

		return report, fmt.Errorf("failed to acquire the reconcile lease: %w", err)
	}
	if !held {
		report.Skipped = true
		report.Duration = time.Since(report.StartedAt)

		return report, nil
	}
	defer func() {
		// Detached from the caller's context: the release must happen even when
		// the scan was cut short by a shutdown, or the next reconciler waits out
		// a full lease for no reason.
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()

		if err := r.store.ReleaseScanLease(releaseCtx, r.holder); err != nil {
			log.Printf("reconciler %q failed to release its scan lease: %v", r.holder, err)
		}
	}()

	err = errors.Join(
		r.releaseAbandonedDispatches(ctx, &report),
		// Expiry runs before the dispatch sweep so a run expired by this scan
		// has its queued message withdrawn by the same scan rather than the next.
		r.expireAbandonedRuns(ctx, &report),
		r.reconcilePendingDispatches(ctx, &report),
		r.retireStaleDispatches(ctx, &report),
		r.reconcileRunnerChannels(ctx, &report),
		r.evictSilentWorkers(ctx, &report),
	)

	report.Duration = time.Since(report.StartedAt)
	r.record(report)
	r.log(report, err)

	return report, err
}

// record stores the scan for Status to report.
func (r *Reconciler) record(report ReconcileReport) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.last = report
	if report.Failures == 0 && !report.Skipped {
		r.lastSuccess = report.StartedAt
	}
}

// log emits a scan summary, quietly when there was nothing to say.
func (r *Reconciler) log(report ReconcileReport, err error) {
	if report.Repaired() == 0 && report.Failures == 0 {
		return
	}

	log.Printf("reconciler %q repaired %d in %v (running-expired=%d pending-expired=%d redispatched=%d "+
		"retired=%d dropped=%d leases=%d channels=%d workers-evicted=%d failures=%d oldest=%v)",
		r.holder, report.Repaired(), report.Duration,
		report.ExpiredRunning, report.ExpiredPending, report.Redispatched,
		report.RetiredDispatches, report.DroppedMessages, report.ReleasedLeases,
		report.RestoredChannels, report.EvictedWorkers, report.Failures, report.OldestInconsistent)

	if err != nil {
		log.Printf("reconciler %q: %v", r.holder, err)
	}
}

// Run reconciles until the context is cancelled.
//
// Errors are logged rather than returned, for the same reason the relay's are: a
// reconciler that stopped on the first database hiccup would leave state
// unattended exactly while something was already wrong.
func (r *Reconciler) Run(ctx context.Context) error {
	log.Printf("reconciler %q started (interval=%v, batch=%d, pending-timeout=%v)",
		r.holder, r.interval, r.batchSize, r.pendingTimeout)

	for {
		if _, err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("reconciler %q scan failed: %v", r.holder, err)
		}

		select {
		case <-ctx.Done():
			log.Printf("reconciler %q stopped", r.holder)
			return ctx.Err()
		case <-time.After(r.interval):
		}
	}
}

// releaseAbandonedDispatches returns rows leased by relays that are gone.
//
// The relay's own claim already steps over an expired lease, so this is not what
// unblocks the backlog. What it fixes is the record: a row that names a relay
// which died last Tuesday reads, to an operator looking for why a dispatch is
// stuck, as a relay currently working on it.
func (r *Reconciler) releaseAbandonedDispatches(ctx context.Context, report *ReconcileReport) error {
	released, err := r.store.ReleaseAbandonedDispatches(ctx, time.Now())
	if err != nil {
		report.Failures++
		return fmt.Errorf("failed to release abandoned dispatch leases: %w", err)
	}

	report.ReleasedLeases += released

	return nil
}

// expireAbandonedRuns terminates runs whose execution lease has elapsed.
//
// This is the lease finally meaning something. A worker that dies mid-probe
// acknowledged its message and will never report, so nothing else in the system
// will ever move that Result: it reads as a run still in progress for as long as
// the database keeps it.
//
// The Result becomes JobExpired and stays that way. It is not reopened and not
// re-dispatched: the attempt happened, it is history, and a retry policy -- when
// there is one -- creates a new Result rather than pretending this one did not
// occur.
func (r *Reconciler) expireAbandonedRuns(ctx context.Context, report *ReconcileReport) error {
	now := time.Now()
	cutoff := now.Add(-r.leaseGrace)

	candidates, err := r.store.ExpiredRuns(ctx, cutoff, r.batchSize)
	if err != nil {
		report.Failures++
		return fmt.Errorf("failed to list runs with expired leases: %w", err)
	}

	var errs error
	for _, candidate := range candidates {
		report.noteInconsistent(candidate, now)

		reason := fmt.Sprintf("execution lease expired at %v", candidate.Status.Deadline.UTC().Format(time.RFC3339))
		switch expired, err := r.store.ExpireRun(ctx, candidate, now, reason); {
		case err != nil:
			report.Failures++
			errs = errors.Join(errs, fmt.Errorf("failed to expire run %q: %w", candidate.Name, err))
		case !expired:
			// The version guard rejected the write, which means the Result
			// changed under this scan -- the worker reported after all, or
			// another reconciler got there first. Either way the drift is gone.
			log.Printf("run %q moved on before its expired lease could be recorded", candidate.Name)
		default:
			report.ExpiredRunning++
			log.Printf("run %q expired: %s", candidate.Name, reason)
		}
	}

	return errs
}

// evictSilentWorkers drops registrations of workers that stopped reporting.
//
// A worker's registration is not self-cleaning: nothing deletes it when the
// process goes away, so without this the fleet view accumulates every worker
// that ever registered. That is bearable where worker names are stable and
// steadily worse where they are not -- a container or pod that mints a new name
// on each restart leaves one dead row behind per restart, and the page that is
// meant to show who is running becomes a history of who ever ran.
//
// Two things it deliberately does not do. It never touches a worker that has
// reported nothing at all: that is `unknown`, the state of the asynq prototype
// and of every record predating liveness reporting, and deleting on an absence
// of evidence is how you take a working fleet offline. And it requires *both*
// signals to be silent, so a worker still announcing itself over NATS survives
// however long its route to the API server has been broken -- that case wants an
// operator, not a deletion.
//
// Evicting a worker does not disturb its runner or anything queued for it, and
// the worker may register again at any time. This drops a registration; it does
// not bar a worker.
func (r *Reconciler) evictSilentWorkers(ctx context.Context, report *ReconcileReport) error {
	if r.workerRetention <= 0 {
		return nil
	}

	cutoff := time.Now().Add(-r.workerRetention)

	candidates, err := r.store.SilentWorkers(ctx, cutoff, r.batchSize)
	if err != nil {
		report.Failures++
		return fmt.Errorf("failed to list workers that have gone silent: %w", err)
	}

	var errs error
	for _, candidate := range candidates {
		switch dropped, err := r.store.DropWorker(ctx, candidate); {
		case err != nil:
			report.Failures++
			errs = errors.Join(errs, fmt.Errorf("failed to evict silent worker %q: %w", candidate.Name, err))
		case !dropped:
			// The version guard rejected the write: the worker re-registered
			// between this scan reading it and the delete. It is back, so there
			// is nothing to evict.
			log.Printf("worker %q came back before it could be evicted", candidate.Name)
		default:
			report.EvictedWorkers++
			log.Printf("evicted worker %q, silent on every signal since before %v",
				candidate.Name, cutoff.UTC().Format(time.RFC3339))
		}
	}

	return errs
}

// reconcilePendingDispatches repairs runs that never started.
//
// Three states hide behind "still pending", and they want opposite handling:
//
//   - no outbox entry at all: the dispatch was removed administratively, or the
//     row predates the outbox. The Result is authoritative and still wants to
//     run, so the entry is written again and the relay takes it from there.
//   - an entry not yet published: this is the relay's work, not the
//     reconciler's, and stealing it would expire runs that are seconds from
//     being queued. Explicitly left alone.
//   - an entry published long ago: the message was accepted and is no longer
//     coming. Past the transport's own expiry there is nothing left to wait for,
//     so the Result is recorded as expired rather than left pending forever.
func (r *Reconciler) reconcilePendingDispatches(ctx context.Context, report *ReconcileReport) error {
	now := time.Now()
	cutoff := now.Add(-r.pendingTimeout)

	candidates, err := r.store.StalePendingRuns(ctx, cutoff, r.batchSize)
	if err != nil {
		report.Failures++
		return fmt.Errorf("failed to list stale pending runs: %w", err)
	}

	var errs error
	for _, candidate := range candidates {
		// An entry that is written but unpublished belongs to the relay. This is
		// the constraint that keeps the two from fighting: a relay holding an
		// unpublished row is evidence that the dispatch is still coming, and
		// inferring a missing message while that is true expires live work.
		if candidate.Dispatch != nil && candidate.Dispatch.PublishedAt == nil && candidate.Dispatch.RetiredAt == nil {
			continue
		}

		report.noteInconsistent(candidate.Result, now)

		if candidate.Dispatch == nil {
			switch enqueued, err := r.store.EnqueueDispatch(ctx, candidate.Result, now); {
			case err != nil:
				report.Failures++
				errs = errors.Join(errs, fmt.Errorf("failed to re-enqueue dispatch for %q: %w", candidate.Result.Name, err))
			case enqueued:
				report.Redispatched++
				log.Printf("re-enqueued the missing dispatch for pending run %q", candidate.Result.Name)
			}

			continue
		}

		reason := "dispatch was published but never delivered"
		if candidate.Dispatch.RetiredAt != nil {
			reason = "dispatch was retired: " + candidate.Dispatch.RetiredReason
		}

		switch expired, err := r.store.ExpireRun(ctx, candidate.Result, now, reason); {
		case err != nil:
			report.Failures++
			errs = errors.Join(errs, fmt.Errorf("failed to expire pending run %q: %w", candidate.Result.Name, err))
		case !expired:
			log.Printf("pending run %q moved on before it could be expired", candidate.Result.Name)
		default:
			report.ExpiredPending++
			log.Printf("pending run %q expired: %s", candidate.Result.Name, reason)
		}
	}

	return errs
}

// retireStaleDispatches clears dispatches whose Result has finished without them.
//
// Two different leftovers, one sweep. An entry still waiting to be published for
// a terminal Result would put a job on a queue that no worker may claim; a
// message already queued for one occupies a runner's bounded share of the stream
// until it ages out, and a runner with no workers online has nothing to age it
// out early.
//
// A dispatch the Result was actually claimed through is neither: the claim is
// only issued to a worker that then acknowledges its message, so there is
// nothing in the queue and the entry is retired without touching the transport.
func (r *Reconciler) retireStaleDispatches(ctx context.Context, report *ReconcileReport) error {
	stale, err := r.store.StaleDispatches(ctx, r.batchSize)
	if err != nil {
		report.Failures++
		return fmt.Errorf("failed to list stale dispatches: %w", err)
	}

	now := time.Now()

	var errs error
	for _, dispatch := range stale {
		reason := "result reached a terminal state"

		if r.shouldDrop(dispatch) {
			if err := r.channels.DropDispatch(ctx, dispatch.Entry); err != nil {
				// Left unretired on purpose: the next scan tries again. Retiring
				// it now would lose the only record of a message still sitting in
				// a runner's queue.
				report.Failures++
				errs = errors.Join(errs, fmt.Errorf("failed to withdraw dispatch %v: %w", dispatch.Entry.EventUID, err))

				continue
			}

			report.DroppedMessages++
			reason = "result reached a terminal state; queued message withdrawn"
		}

		if err := r.store.RetireDispatch(ctx, dispatch.Entry.ID, now, reason); err != nil {
			report.Failures++
			errs = errors.Join(errs, fmt.Errorf("failed to retire dispatch %v: %w", dispatch.Entry.EventUID, err))

			continue
		}

		report.RetiredDispatches++
	}

	return errs
}

// shouldDrop reports whether a stale entry still has a message worth withdrawing.
func (r *Reconciler) shouldDrop(dispatch StaleDispatch) bool {
	return r.channels != nil &&
		!dispatch.Delivered &&
		dispatch.Entry.PublishedAt != nil &&
		dispatch.Entry.PublishedSeq != 0
}

// reconcileRunnerChannels restores queues that active runners have lost.
//
// A runner whose consumer was deleted -- by an operator clearing up, by a
// restore from a backup taken before it existed -- accepts dispatches and
// delivers none of them, which presents as probes silently stopping. Workers
// cannot fix this themselves by design, so if the control plane does not notice,
// nobody does.
func (r *Reconciler) reconcileRunnerChannels(ctx context.Context, report *ReconcileReport) error {
	if r.channels == nil {
		return nil
	}

	runners, err := r.store.ActiveRunnerUIDs(ctx)
	if err != nil {
		report.Failures++
		return fmt.Errorf("failed to list active runners: %w", err)
	}

	var errs error
	for _, runnerUID := range runners {
		restored, err := r.channels.EnsureRunnerChannel(ctx, runnerUID)
		if err != nil {
			report.Failures++
			errs = errors.Join(errs, fmt.Errorf("failed to restore the queue for runner %v: %w", runnerUID, err))

			continue
		}
		if restored {
			report.RestoredChannels++
			log.Printf("restored the missing queue for runner %v", runnerUID)
		}
	}

	return errs
}

// noteInconsistent tracks the age of the oldest drift this scan has seen.
func (r *ReconcileReport) noteInconsistent(result Result, now time.Time) {
	if result.CreatedAt == nil {
		return
	}

	if age := now.Sub(*result.CreatedAt); age > r.OldestInconsistent {
		r.OldestInconsistent = age
	}
}

// expireResult applies the expiry transition to a Result in memory.
//
// Kept next to the reconciler rather than in the store so that what "expired"
// means to the domain -- terminal state, an end time, a timeout outcome, and
// labels that make it selectable -- is stated once and is not something a store
// implementation gets to decide.
func expireResult(result *Result, at time.Time) {
	result.Status.Status = JobExpired
	result.Status.Result = prob.RunFinishedTimeout
	result.Spec.TimeEnded = &at

	result.Labels = manifest.MergeLabels(
		result.Labels,
		manifest.Labels{
			LabelResultJobState: string(result.Status.Status),
			LabelResultStatus:   string(result.Status.Result),
		},
	)
}
