package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/worker"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ADR 0004, "the worker dies after its acknowledgement was confirmed".
//
// This is the case the execution lease exists for and the one nothing else can
// see: the message is gone -- correctly, it was acknowledged -- the claim is
// committed, and the process that held it is not coming back. Without the
// reconciler the Result reads as a run still in progress for as long as the
// database keeps it.
//
// It is settled, not resumed. The attempt happened and is history; a retry
// creates a new Result rather than pretending this one did not occur, which is
// what keeps a run's record an account of one execution.
func TestWorkerDyingAfterAckLeavesALeaseTheReconcilerSettles(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("lease-runner", nil)
	scenario := h.applyScenario("lease-scenario", testProbSpec{Message: "abandoned run"}, manifest.LabelSelector{})

	claimed := make(chan struct{})

	// The probe is replaced by one that starts and never reports, which is what a
	// worker looks like from the outside when its process disappears: the
	// acknowledgement was confirmed, the run is leased, and nothing further
	// arrives. A probe that returned normally on shutdown would report a
	// cancelled run instead, and settle the Result itself.
	dying := h.startWorker(runner.Name, withProbeRunner(
		func(ctx context.Context, _ natsq.DispatchEnvelope, _ urth.AuthJobResponse) {
			close(claimed)
			<-ctx.Done()
		}))

	run := h.createRun(scenario.Name)
	h.mustRelay(1)

	select {
	case <-claimed:
	case <-time.After(60 * time.Second):
		h.dump()
		t.Fatal("the run was never claimed")
	}

	running := h.result(run.UID)
	require.Equal(t, urth.JobRunning, running.Status.Status)
	require.False(t, running.Status.Deadline.IsZero(), "a claimed run carries its lease")
	require.Equal(t, dying.Meta().UID, running.Status.Executor.WorkerID)

	// The message is gone: it was acknowledged before execution started, so
	// nothing on the queue will ever produce this run again.
	h.eventually(30*time.Second, "the claimed message to have left the queue", func() bool {
		info := h.consumerInfo(runner.UID)

		return info.NumPending == 0 && info.NumAckPending == 0
	})

	dying.stop(t)

	// Nothing repairs itself. The Result is still `running` and will stay that
	// way until a scan looks at it.
	require.Equal(t, urth.JobRunning, h.result(run.UID).Status.Status)
	require.Zero(t, h.reconcileOnce().ExpiredRunning,
		"a run whose lease has not expired is a slow run, not an abandoned one")

	// Past the deadline by more than the grace the capability keeps for uploads.
	h.backdate("results", "status_deadline", "uid", run.UID, time.Hour)

	report := h.reconcileOnce()
	require.Equal(t, 1, report.ExpiredRunning)

	expired := h.result(run.UID)
	require.Equal(t, urth.JobExpired, expired.Status.Status)
	require.Equal(t, dying.Meta().UID, expired.Status.Executor.WorkerID,
		"who was holding it stays on the record")

	// Never reopened, on this scan or any later one.
	require.Zero(t, h.reconcileOnce().ExpiredRunning)
	require.Equal(t, urth.JobExpired, h.result(run.UID).Status.Status)

	// A retry is a new Result, which a live worker executes normally.
	h.startWorker(runner.Name)

	retry := h.createRun(scenario.Name)
	require.NotEqual(t, run.UID, retry.UID, "a retry must not reuse the expired run")
	h.mustRelay(1)

	finished := h.awaitTerminal(retry.UID, 90*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.Equal(t, urth.JobExpired, h.result(run.UID).Status.Status,
		"the expired run is untouched by the retry")
}

// A worker with nothing to do stops when it is asked to, not when its pull
// window happens to close.
//
// The consume loop spends almost all its life inside a pull, so "is the fetch
// bounded by the worker's context" decides whether a shutdown takes milliseconds
// or waits out `fetchMaxWait` -- and, when the pull is one the broker will never
// answer, whether the worker stops at all. It did not, before: a pull lost to a
// reconnect left the loop blocked indefinitely, taking no work and ignoring
// cancellation, while the worker went on registering and heartbeating. Every
// other signal said the fleet was healthy.
//
// The bound is well under the pull window on purpose: matching it would pass
// against the behaviour this guards.
func TestWorkerStopsPromptlyWhileIdle(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("prompt-stop-runner", nil)
	idle := h.startWorker(runner.Name)

	// Mid-pull rather than between pulls, which is where a worker almost always
	// is and is the only interesting case.
	time.Sleep(500 * time.Millisecond)

	started := time.Now()
	idle.stop(t)

	require.Less(t, time.Since(started), 2*time.Second,
		"a shutdown must not wait out the pull window")
}

// Two workers behind one runner share its queue, and a work-queue stream is what
// makes that safe: one message, one worker.
//
// Nothing here says which worker takes which run -- that is the broker's to
// decide and would be a fiction to assert. What must hold is that every run is
// executed exactly once, by a worker of the runner it was placed on.
func TestTwoWorkersOfOneRunnerEachExecuteDistinctRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("shared-runner", nil)

	// Concurrency 1 apiece, so the two runs cannot both be taken by one worker
	// before the other has a chance to fetch -- which would still be correct, but
	// would leave this test asserting nothing about sharing.
	first := h.startWorker(runner.Name, withWorkerConfig(func(cfg *worker.Config) { cfg.Concurrency = 1 }))
	second := h.startWorker(runner.Name, withWorkerConfig(func(cfg *worker.Config) { cfg.Concurrency = 1 }))

	messages := []string{"shared run one", "shared run two"}

	runs := make([]urth.Result, 0, len(messages))
	for i, message := range messages {
		scenario := h.applyScenario(
			manifest.ResourceName("shared-scenario-"+message[len(message)-3:]),
			testProbSpec{Message: message, Delay: 2 * time.Second},
			manifest.LabelSelector{})

		runs = append(runs, h.createRun(scenario.Name))
		require.Equal(t, runner.UID, runs[i].Status.Executor.RunnerID)
	}

	h.mustRelay(len(messages))

	workers := make(map[manifest.ResourceID]int, 2)
	for i, run := range runs {
		finished := h.awaitTerminal(run.UID, 90*time.Second)
		require.Equal(t, urth.JobCompleted, finished.Status.Status)
		require.Equal(t, runner.UID, finished.Status.Executor.RunnerID)

		workers[finished.Status.Executor.WorkerID]++

		require.EqualValues(t, 1, probeRunCount(messages[i]),
			"a work-queue stream delivers each message to exactly one worker")
	}

	// Whichever way the broker split them, both executors are workers of this
	// runner and nothing else claimed anything.
	registered := map[manifest.ResourceID]bool{
		first.Meta().UID:  true,
		second.Meta().UID: true,
	}
	for executor := range workers {
		require.True(t, registered[executor], "run executed by an unregistered worker %v", executor)
	}

	require.Empty(t, h.dispatchFailures())
}
