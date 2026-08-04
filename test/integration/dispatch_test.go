package integration

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ADR 0004, "Postgres commits, NATS is down".
//
// The outbox's whole promise: a run requested while the broker is unreachable is
// still a run that happens. The Result and its dispatch commit together, the
// relay fails and records why, and when the broker returns the same committed
// entry is published without anyone re-creating anything.
func TestDispatchSurvivesABrokerOutage(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("outage-runner", nil)
	scenario := h.applyScenario("outage-scenario", testProbSpec{Message: "broker outage"}, manifest.LabelSelector{})

	instance := h.startWorker(runner.Name)

	h.stopNATS()

	// Postgres is fine, so the run is recorded and its dispatch with it.
	run := h.createRun(scenario.Name)
	require.Equal(t, urth.JobPending, run.Status.Status)

	entries := h.outbox(run.UID)
	require.Len(t, entries, 1)
	require.Nil(t, entries[0].PublishedAt)

	published, err := h.relayOnce()
	require.Error(t, err, "the relay cannot publish to a broker that is not there")
	require.Zero(t, published)

	// A broker outage is not an execution failure. The prototype rewrote the
	// Result as `errored` here, which claimed an execution that never happened
	// and removed the only record a retry could work from.
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)

	failed := h.outbox(run.UID)
	require.Len(t, failed, 1)
	require.Nil(t, failed[0].PublishedAt)
	require.Equal(t, 1, failed[0].Attempts)
	require.NotEmpty(t, failed[0].LastError)

	h.startNATS()

	// The relay retries on a backoff, so the first pass after the broker returns
	// may still find the entry held back. The publication is what is under test,
	// not the schedule.
	h.eventually(60*time.Second, "the committed dispatch to be published once the broker returns", func() bool {
		published, err := h.relayOnce()

		return err == nil && published == 1
	})

	finished := h.awaitTerminal(run.UID, 60*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.Equal(t, instance.Meta().UID, finished.Status.Executor.WorkerID)

	require.EqualValues(t, 1, probeRunCount("broker outage"),
		"one committed dispatch is one execution, however many publication attempts it took")
}

// dyingPublisher publishes for real and then reports failure.
//
// This is the crash point the event UID exists for and the one that needs no
// production seam: the message is out, the broker has it, and the outbox row
// does not know. Whether the relay process died or merely lost Postgres for a
// moment, the recovery is the same -- publish again, with the same event UID,
// and let JetStream discard it.
type dyingPublisher struct {
	urth.DispatchPublisher

	dieOnce atomic.Bool
}

func (p *dyingPublisher) PublishDispatch(ctx context.Context, entry urth.DispatchOutboxEntry) (urth.DispatchReceipt, error) {
	receipt, err := p.DispatchPublisher.PublishDispatch(ctx, entry)
	if err != nil {
		return receipt, err
	}

	if p.dieOnce.CompareAndSwap(true, false) {
		return urth.DispatchReceipt{}, fmt.Errorf("relay died after the broker accepted dispatch %v", entry.EventUID)
	}

	return receipt, nil
}

// ADR 0004, "the relay publishes and then dies before marking the outbox".
//
// The republication carries the entry's stable event UID, which is the
// Nats-Msg-Id, so JetStream suppresses it. One committed dispatch, two
// publication attempts, one message, one run.
//
// The event UID is minted once when the entry is enqueued and reused on every
// retry precisely for this; a relay that derived a fresh identifier per attempt
// would pass every unit test and double-run every scenario it ever retried.
func TestRelayCrashAfterPublishDoesNotDuplicateTheRun(t *testing.T) {
	t.Parallel()

	publisher := &dyingPublisher{}
	publisher.dieOnce.Store(true)

	h := newHarness(t, withPublisherDecorator(func(next urth.DispatchPublisher) urth.DispatchPublisher {
		publisher.DispatchPublisher = next

		return publisher
	}))

	runner := h.applyRunner("crash-runner", nil)
	scenario := h.applyScenario("crash-scenario", testProbSpec{Message: "relay crash"}, manifest.LabelSelector{})

	h.startWorker(runner.Name)

	run := h.createRun(scenario.Name)

	// The message reaches the broker; the bookkeeping does not survive.
	published, err := h.relayOnce()
	require.Error(t, err)
	require.Zero(t, published)

	interrupted := h.outbox(run.UID)
	require.Len(t, interrupted, 1)
	require.Nil(t, interrupted[0].PublishedAt, "the row does not know what the broker already has")

	eventUID := interrupted[0].EventUID

	// The relay comes back and retries the same entry.
	h.eventually(30*time.Second, "the interrupted dispatch to be published again", func() bool {
		published, err := h.relayOnce()

		return err == nil && published == 1
	})

	recorded := h.outbox(run.UID)
	require.Len(t, recorded, 1)
	require.NotNil(t, recorded[0].PublishedAt)
	require.Equal(t, eventUID, recorded[0].EventUID, "the event UID must survive the retry; it is the deduplication key")

	finished := h.awaitTerminal(run.UID, 60*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)

	require.EqualValues(t, 1, probeRunCount("relay crash"),
		"the republished dispatch must not become a second run")

	// Directly: the broker accepted one message. LastSeq counts every message
	// the stream ever took, and a suppressed duplicate does not advance it.
	require.EqualValues(t, 1, h.streamState().LastSeq,
		"the second publication should have been discarded by the duplicate window")
}

// ADR 0004, "the message ages out".
//
// Two halves, and the second is the one that is easy to break. A pending run
// whose dispatch was published long ago is expired by the reconciler; a pending
// run whose dispatch is written but *not yet published* belongs to the relay and
// must be left alone, because inferring a lost message there expires work the
// relay is seconds away from delivering.
func TestAgedPendingRunIsExpiredByTheReconcilerAndNotBeforeItIsPublished(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.applyRunner("expiry-runner", nil)
	scenario := h.applyScenario("expiry-scenario", testProbSpec{Message: "expiry"}, manifest.LabelSelector{})

	// No worker: nothing may claim this run, which is the situation being
	// reconciled.
	run := h.createRun(scenario.Name)

	// Older than MaxJobAge plus the pending grace, so every age test the
	// reconciler makes is satisfied.
	h.backdate("results", "created_at", "uid", run.UID,
		h.Config.NATS.MaxJobAge+h.Config.Controllers.PendingDispatchGrace+time.Hour)

	report := h.reconcileOnce()
	require.Zero(t, report.ExpiredPending,
		"an unpublished outbox row is the relay's work; expiring its run terminates a dispatch that is still coming")
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)

	h.mustRelay(1)

	// The relay is not what expires a run either: it published this one and has
	// nothing further to say about it.
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)
	published, err := h.relayOnce()
	require.NoError(t, err)
	require.Zero(t, published)
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)

	// Published and long past the transport's own expiry: the message is not
	// coming, and there is nothing left to wait for.
	report = h.reconcileOnce()
	require.Equal(t, 1, report.ExpiredPending)

	expired := h.result(run.UID)
	require.Equal(t, urth.JobExpired, expired.Status.Status)

	// And the entry is retired, so it leaves the relay's queries and the
	// outbox's statistics rather than being retried forever.
	require.GreaterOrEqual(t, report.RetiredDispatches, 1)
	require.NotNil(t, h.outbox(run.UID)[0].RetiredAt)

	// An expired Result is never reopened: a retry is a new Result.
	require.Equal(t, urth.JobExpired, h.reconcileAndReread(run.UID).Status.Status)
}

// reconcileAndReread runs another scan and returns the run as it stands after it.
func (h *harness) reconcileAndReread(uid manifest.ResourceID) urth.Result {
	h.t.Helper()

	h.reconcileOnce()

	return h.result(uid)
}

// ADR 0004, "Postgres is unavailable".
//
// A claim the API server cannot answer is a 5xx, which the worker reads as
// transient: it NAKs the message and leaves the run alone. Nothing is
// acknowledged, nothing is terminated, no dead letter is filed, and the run
// executes as soon as the database is back.
//
// The failure is a real one -- the table the claim reads is not there -- rather
// than an injected error, because what is under test is the whole chain from a
// Postgres error, through resultsAPIImpl, through the status the router chooses,
// to the disposition the worker applies to the message. Every link in that chain
// was previously tested against the next one's assumption.
func TestClaimAgainstAnUnavailableDatabaseIsRetriedNotLost(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("db-outage-runner", nil)
	scenario := h.applyScenario("db-outage-scenario", testProbSpec{Message: "db outage"}, manifest.LabelSelector{})

	h.startWorker(runner.Name)

	run := h.createRun(scenario.Name)

	// Confined to this test's schema, so it takes nothing else with it.
	require.NoError(t, h.DB.Exec("ALTER TABLE results RENAME TO results_unavailable").Error)

	h.mustRelay(1)

	// The worker cannot claim, so the message comes back. A redelivery is the
	// positive evidence that it was NAKed rather than acked away or terminated:
	// either of those would have destroyed the only dispatch for a run that is
	// still pending.
	h.eventually(60*time.Second, "the unclaimable message to be redelivered", func() bool {
		return h.consumerInfo(runner.UID).NumRedelivered >= 1
	})

	require.Empty(t, h.dispatchFailures(),
		"a database outage is not a policy refusal and must not become a dead letter")

	require.NoError(t, h.DB.Exec("ALTER TABLE results_unavailable RENAME TO results").Error)

	finished := h.awaitTerminal(run.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.EqualValues(t, 1, probeRunCount("db outage"))
}

// An outbox entry the transport can never accept becomes a dead letter, and the
// run it was written for is settled rather than left pending forever.
//
// Distinct from the unschedulable case, which never writes an entry at all: this
// is a row that exists and cannot be delivered, which the relay is the only
// component in a position to settle -- the reconciler leaves unpublished rows
// alone by design.
func TestPermanentlyUndeliverableDispatchBecomesADeadLetter(t *testing.T) {
	t.Parallel()

	h := newHarness(t, withPublisherDecorator(func(urth.DispatchPublisher) urth.DispatchPublisher {
		return refusingPublisher{}
	}))

	h.applyRunner("undeliverable-runner", nil)
	scenario := h.applyScenario("undeliverable-scenario", testProbSpec{Message: "undeliverable"}, manifest.LabelSelector{})

	run := h.createRun(scenario.Name)

	published, err := h.relayOnce()
	require.Error(t, err)
	require.Zero(t, published)

	failures := h.dispatchFailures()
	require.Len(t, failures, 1)
	require.Equal(t, urth.ReasonUndeliverableDispatch, failures[0].Spec.Reason)
	require.Equal(t, run.UID, failures[0].Spec.ResultUID)
}

// refusingPublisher rejects every dispatch in a way no retry can change.
type refusingPublisher struct{}

func (refusingPublisher) PublishDispatch(context.Context, urth.DispatchOutboxEntry) (urth.DispatchReceipt, error) {
	return urth.DispatchReceipt{}, fmt.Errorf("%w: this transport will never accept this dispatch", urth.ErrPermanentDispatch)
}
