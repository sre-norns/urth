package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/worker"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// claimInterceptor wraps a worker's API client so a test can interfere with the
// one call the dispatch path turns on.
//
// The failures it exists for happen on the wire: a claim that committed and
// whose response never arrived, a claim interrupted by a worker going away. Both
// leave the API server and the worker each behaving correctly, so neither can be
// provoked from inside one of them.
type claimInterceptor struct {
	urth.Service

	intercept func(ctx context.Context, next func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error)
}

func (c *claimInterceptor) Results(scenario manifest.ResourceName) urth.RunResultAPI {
	return &resultsInterceptor{
		RunResultAPI: c.Service.Results(scenario),
		intercept:    c.intercept,
	}
}

type resultsInterceptor struct {
	urth.RunResultAPI

	intercept func(ctx context.Context, next func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error)
}

func (r *resultsInterceptor) ClaimRun(ctx context.Context, resultUID manifest.ResourceID, session urth.APIToken, request urth.ClaimJobRequest) (urth.AuthJobResponse, error) {
	return r.intercept(ctx, func() (urth.AuthJobResponse, error) {
		return r.RunResultAPI.ClaimRun(ctx, resultUID, session, request)
	})
}

// interceptClaims builds the worker option that installs one.
func interceptClaims(fn func(ctx context.Context, next func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error)) workerOption {
	return withClientWrapper(func(client urth.Service) urth.Service {
		return &claimInterceptor{Service: client, intercept: fn}
	})
}

// ADR 0004, "the claim commits and its response is lost".
//
// The worker cannot tell that from a claim that failed, so it retries -- and the
// retry presents the same dispatch ID. That is the whole reason ClaimJobRequest
// carries one: the server can tell "the same worker asking twice" from "a second
// worker wants this run", recover the first authorization, and let exactly one
// probe happen.
//
// A claim that were not idempotent would show up here as two executions, or as a
// run stranded in `running` with nobody executing it.
func TestLostClaimResponseIsRecoveredByTheSameWorker(t *testing.T) {
	t.Parallel()

	var dropped atomic.Bool
	dropped.Store(true)

	h := newHarness(t)

	runner := h.applyRunner("lost-response-runner", nil)
	scenario := h.applyScenario("lost-response-scenario", testProbSpec{Message: "lost response"}, manifest.LabelSelector{})

	h.startWorker(runner.Name, interceptClaims(
		func(_ context.Context, next func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error) {
			auth, err := next()
			if err != nil {
				return auth, err
			}

			if dropped.CompareAndSwap(true, false) {
				// Shaped like a transport failure rather than an API status,
				// because that is what a lost response is. The worker's
				// classification treats an error carrying no status as
				// transient, which is the behaviour under test: a claim that may
				// well have committed must not have its message thrown away.
				return urth.AuthJobResponse{}, errors.New("read tcp 127.0.0.1: connection reset by peer")
			}

			return auth, nil
		}))

	run := h.createRun(scenario.Name)
	h.mustRelay(1)

	finished := h.awaitTerminal(run.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.False(t, dropped.Load(), "the first claim response should have been dropped")

	require.EqualValues(t, 1, probeRunCount("lost response"),
		"a recovered claim is the same claim; the probe must run once")

	// One dispatch, and the run records the one it was claimed for.
	entries := h.outbox(run.UID)
	require.Len(t, entries, 1)
	require.Equal(t, entries[0].EventUID, finished.Status.DispatchID)

	require.Empty(t, h.dispatchFailures())
}

// ADR 0004, "the worker dies before the claim resolves".
//
// A claim cut short by shutdown is not a verdict on the run: acking would lose
// its only dispatch and terminating it would strand the run permanently. So the
// message is left untouched, the broker redelivers it after AckWait, and another
// worker runs it.
func TestWorkerDyingMidClaimLeavesTheJobForAnother(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("abandon-runner", nil)
	scenario := h.applyScenario("abandon-scenario", testProbSpec{Message: "abandoned claim"}, manifest.LabelSelector{})

	claimReached := make(chan struct{})
	var once sync.Once

	// One slot, so this worker cannot fetch a second message while it is stuck
	// on the first.
	dying := h.startWorker(runner.Name,
		withWorkerConfig(func(cfg *worker.Config) { cfg.Concurrency = 1 }),
		interceptClaims(func(ctx context.Context, _ func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error) {
			once.Do(func() { close(claimReached) })

			// Never answers. The worker's own shutdown is what ends it, which is
			// the case being reproduced: the process went away with a claim in
			// flight and no answer either way.
			<-ctx.Done()

			return urth.AuthJobResponse{}, ctx.Err()
		}))

	run := h.createRun(scenario.Name)
	h.mustRelay(1)

	select {
	case <-claimReached:
	case <-time.After(60 * time.Second):
		h.dump()
		t.Fatal("the worker never reached its claim")
	}

	dying.stop(t)

	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status,
		"an interrupted claim decides nothing about the run")
	require.EqualValues(t, 0, probeRunCount("abandoned claim"))

	// The message was never acknowledged, so it is still the broker's to give
	// out. A second worker finds it there.
	h.startWorker(runner.Name)

	finished := h.awaitTerminal(run.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.EqualValues(t, 1, probeRunCount("abandoned claim"))
	require.Empty(t, h.dispatchFailures(),
		"a worker that went away mid-claim is not a dispatch failure")
}

// Task 010: a dispatch redelivered while its run is executing.
//
// The confirmed acknowledgement closes most of this window, but not the part
// where the redelivery is already on its way. The worker's in-process ownership
// set is what stops it becoming a second concurrent execution of a probe this
// process is already running -- and the duplicate is *acknowledged*, not naked:
// holding it would reserve one of the runner's MaxAckPending slots for the
// length of a probe, and naking it would bring it back until MaxDeliver filed a
// dead letter for a dispatch that was delivered perfectly well.
func TestRedeliveryDuringExecutionRunsTheProbeOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("duplicate-runner", nil)
	scenario := h.applyScenario("duplicate-scenario", testProbSpec{
		Message: "duplicate delivery",
		// Long enough that the redelivery below lands while the probe is still
		// going, which is the only interesting moment.
		Delay: 5 * time.Second,
	}, manifest.LabelSelector{})

	h.startWorker(runner.Name)

	run := h.createRun(scenario.Name)
	h.mustRelay(1)

	// Wait for the run to be claimed and executing before offering it again.
	h.eventually(60*time.Second, "the run to start executing", func() bool {
		return h.result(run.UID).Status.Status == urth.JobRunning
	})

	entries := h.outbox(run.UID)
	require.Len(t, entries, 1)
	h.redeliver(entries[0], runner.UID)

	finished := h.awaitTerminal(run.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)

	require.EqualValues(t, 1, probeRunCount("duplicate delivery"),
		"the redelivery must not start a second execution of a run this process already holds")

	// Both messages left the queue. A duplicate that was naked instead would
	// still be pending here, and would keep coming back.
	h.eventually(30*time.Second, "both deliveries to leave the runner's queue", func() bool {
		info := h.consumerInfo(runner.UID)

		return info.NumPending == 0 && info.NumAckPending == 0
	})

	require.Empty(t, h.dispatchFailures(),
		"a duplicate delivery is not a dispatch failure")
}

// ADR 0004, "a stale message is redelivered after another worker claimed it".
//
// The run is legitimately held elsewhere, so the API answers 409 and the worker
// acknowledges the message away rather than retrying it or filing a dead letter.
// The status class is the whole contract here: read as 5xx it would spin, read
// as 4xx-terminal it would dead-letter a dispatch that was handled correctly.
func TestStaleRedeliveryToAnotherWorkerIsAcknowledgedAway(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("stale-runner", nil)
	scenario := h.applyScenario("stale-scenario", testProbSpec{
		Message: "stale redelivery",
		Delay:   5 * time.Second,
	}, manifest.LabelSelector{})

	// One slot, so that once this worker is executing it cannot also fetch the
	// redelivery below -- which would be correct behaviour (its in-process
	// ownership set would drop it) but is a different scenario, and would leave
	// this one asserting nothing.
	holder := h.startWorker(runner.Name, withWorkerConfig(func(cfg *worker.Config) { cfg.Concurrency = 1 }))

	run := h.createRun(scenario.Name)
	h.mustRelay(1)

	h.eventually(60*time.Second, "the first worker to claim the run", func() bool {
		return h.result(run.UID).Status.Status == urth.JobRunning
	})
	require.Equal(t, holder.Meta().UID, h.result(run.UID).Status.Executor.WorkerID)

	// A second worker, and a second delivery of the same dispatch. The first
	// worker's in-process ownership set does not apply to it: this is a
	// different process being offered a run somebody else is executing, which is
	// what the API's 409 is for.
	var refusals atomic.Int64

	h.startWorker(runner.Name, interceptClaims(
		func(_ context.Context, next func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error) {
			auth, err := next()
			if err != nil {
				refusals.Add(1)
			}

			return auth, err
		}))

	entries := h.outbox(run.UID)
	require.Len(t, entries, 1)
	h.redeliver(entries[0], runner.UID)

	finished := h.awaitTerminal(run.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.Equal(t, holder.Meta().UID, finished.Status.Executor.WorkerID,
		"the run stays with the worker that claimed it")

	require.EqualValues(t, 1, probeRunCount("stale redelivery"),
		"a run held by another worker must not be executed a second time")

	h.eventually(30*time.Second, "the stale message to be acknowledged away", func() bool {
		info := h.consumerInfo(runner.UID)

		return info.NumPending == 0 && info.NumAckPending == 0
	})

	require.Positive(t, refusals.Load(), "the second worker's claim should have been refused")
	require.Empty(t, h.dispatchFailures(),
		"a refused claim for a run somebody else holds is stale, not a dead letter")
}

// A message that reached the wrong runner's queue is refused and reported.
//
// Executing it anyway would defeat placement outright -- the point of the whole
// design is that a probe runs inside the network segment its runner sits in --
// so the worker terminates the message, and files a dead letter first so the
// reason survives it.
func TestMisroutedDispatchIsRefusedAndReported(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	segmentA := h.applyRunner("segment-a", manifest.Labels{"segment": "a"})
	segmentB := h.applyRunner("segment-b", manifest.Labels{"segment": "b"})

	scenario := h.applyScenario("segment-b-scenario", testProbSpec{Message: "misrouted"},
		manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "b"}})

	// Only segment A has a worker, and it must not run segment B's work.
	h.startWorker(segmentA.Name)

	run := h.createRun(scenario.Name)
	require.Equal(t, segmentB.UID, run.Status.Executor.RunnerID,
		"the scenario's requirements place this run on segment B")

	h.mustRelay(1)

	entries := h.outbox(run.UID)
	require.Len(t, entries, 1)

	// The dispatch says segment B; the message arrives on segment A's queue.
	h.redeliver(entries[0], segmentA.UID)

	h.eventually(60*time.Second, "the misrouted dispatch to be reported", func() bool {
		return len(h.dispatchFailures()) > 0
	})

	failures := h.dispatchFailures()
	require.Len(t, failures, 1)
	require.Equal(t, urth.ReasonMisroutedDispatch, failures[0].Spec.Reason)
	require.Equal(t, run.UID, failures[0].Spec.ResultUID)

	require.EqualValues(t, 0, probeRunCount("misrouted"),
		"a worker of one runner must never execute another runner's run")

	// Reporting a dead letter strands the run: this dispatch is the one that was
	// supposed to start it, and the report says it never will. `errored` rather
	// than `timeout`, because nothing waited and nothing ran out of time -- and
	// the reason is a label so that triage is a query.
	stranded := h.result(run.UID)
	require.Equal(t, urth.JobErrored, stranded.Status.Status)
	require.NotEmpty(t, stranded.Labels[urth.LabelResultUnschedulable])
}

// Placement keeps two segments apart without anything having to refuse a
// message: a run required on one runner is published on that runner's subject
// and nowhere else, so a worker of another runner is never offered it.
//
// This is the ordinary case that makes the misrouting check above a backstop
// rather than the mechanism.
func TestRunPlacedOnAnotherRunnerIsNeverOfferedToThisOne(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	segmentA := h.applyRunner("isolation-a", manifest.Labels{"segment": "a"})
	segmentB := h.applyRunner("isolation-b", manifest.Labels{"segment": "b"})

	scenario := h.applyScenario("isolation-scenario", testProbSpec{Message: "isolated"},
		manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "b"}})

	onlyA := h.startWorker(segmentA.Name)

	run := h.createRun(scenario.Name)
	require.Equal(t, segmentB.UID, run.Status.Executor.RunnerID)

	h.mustRelay(1)

	// Queued on segment B's subject, where there is nobody yet -- a runner with
	// no workers is not unschedulable, it is a queue that waits. Segment A's
	// worker has been pulling throughout and has never been offered it.
	require.EqualValues(t, 1, h.streamState().Msgs)
	require.NotEqual(t, onlyA.RunnerUID(), segmentB.UID)
	require.Zero(t, h.consumerInfo(segmentA.UID).Delivered.Consumer,
		"segment A's worker was offered a run placed on segment B")
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)
	require.EqualValues(t, 0, probeRunCount("isolated"))

	// A worker of segment B finds it exactly where it was left.
	h.startWorker(segmentB.Name)

	finished := h.awaitTerminal(run.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.Equal(t, segmentB.UID, finished.Status.Executor.RunnerID)
	require.EqualValues(t, 1, probeRunCount("isolated"))

	// Re-checked after the run has been through its whole life, so this is not
	// merely a statement that segment A had not got round to it yet.
	require.Zero(t, h.consumerInfo(segmentA.UID).Delivered.Consumer)
	require.Empty(t, h.dispatchFailures())
}

// An envelope nobody can parse is terminated, but only once the control plane
// has a record of it: a terminated message is gone, and JetStream will not
// bring it back to explain itself.
func TestUnreadableMessageIsReportedBeforeItIsTerminated(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("malformed-runner", nil)
	h.startWorker(runner.Name)

	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	_, err := h.jetStream().Publish(ctx, natsq.JobSubject(runner.UID), []byte("this is not a dispatch envelope"))
	require.NoError(t, err)

	h.eventually(60*time.Second, "the unreadable message to be reported", func() bool {
		return len(h.dispatchFailures()) > 0
	})

	failures := h.dispatchFailures()
	require.Len(t, failures, 1)
	require.Equal(t, urth.ReasonMalformedEnvelope, failures[0].Spec.Reason)

	h.eventually(30*time.Second, "the unreadable message to leave the queue", func() bool {
		info := h.consumerInfo(runner.UID)

		return info.NumPending == 0 && info.NumAckPending == 0
	})
}
