package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The whole path, once, with nothing going wrong.
//
// It is the scenario every other one here is a deviation from, and it is also
// the one nothing in this repository had: apply a runner and a scenario, let a
// worker register over HTTP and bind its queue, commit a run with its outbox
// entry, relay it, have the worker claim it against a real API server, execute
// it, and upload what it produced. Each of those steps had a unit test; none of
// them had a test that the next step agreed.
func TestHappyPathRunExecutesEndToEnd(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	runner := h.applyRunner("happy-runner", nil)
	scenario := h.applyScenario("happy-scenario", testProbSpec{
		Message:   "happy path",
		Artifacts: 1,
	}, manifest.LabelSelector{})

	instance := h.startWorker(runner.Name)

	run := h.createRun(scenario.Name)
	require.Equal(t, urth.JobPending, run.Status.Status)
	require.Equal(t, runner.UID, run.Status.Executor.RunnerID,
		"placement records the runner before the Result is persisted")
	require.Empty(t, run.Status.Executor.WorkerID,
		"no worker holds a run until it claims one")

	// Nothing has been published yet: creating a Result commits a dispatch, and
	// the relay is what makes it happen.
	entries := h.outbox(run.UID)
	require.Len(t, entries, 1)
	require.Nil(t, entries[0].PublishedAt)
	require.Equal(t, urth.DispatchEventUID(run.UID, run.Version), entries[0].EventUID)

	h.mustRelay(1)

	finished := h.awaitTerminal(run.UID, 30*time.Second)

	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.Equal(t, prob.RunFinishedSuccess, finished.Status.Result)

	// Executor identity is captured at claim time, which is the only moment the
	// association is certain.
	require.Equal(t, runner.UID, finished.Status.Executor.RunnerID)
	require.Equal(t, runner.Name, finished.Status.Executor.RunnerName)
	require.Equal(t, instance.Meta().UID, finished.Status.Executor.WorkerID)
	require.Equal(t, instance.Meta().Name, finished.Status.Executor.WorkerName)
	require.Equal(t, entries[0].EventUID, finished.Status.DispatchID)

	require.NotNil(t, finished.Spec.TimeStarted)
	require.NotNil(t, finished.Spec.TimeEnded)

	// The same identity is on the run as labels, which is the search surface.
	require.Equal(t, string(runner.Name), finished.Labels[urth.LabelRunnerName])
	require.Equal(t, string(instance.Meta().Name), finished.Labels[urth.LabelWorkerName])

	// The outbox row knows it was published, and knows where.
	published := h.outbox(run.UID)
	require.Len(t, published, 1, "one run, one dispatch")
	require.NotNil(t, published[0].PublishedAt)
	require.NotZero(t, published[0].PublishedSeq)
	require.Equal(t, 1, published[0].Attempts)

	// Artifacts are linked to the run by the server-derived label, not by a name
	// the worker chose. The prober asked for one; runner.Play adds the metrics
	// snapshot and the run log to every run.
	//
	// Waited for rather than read once: the worker uploads artifacts and posts
	// the final status concurrently, deliberately -- one artifact that cannot be
	// stored must not also lose the status. So a terminal Result does not mean
	// the uploads have landed.
	h.eventually(30*time.Second, "the run's artifacts to be uploaded", func() bool {
		return len(h.artifacts(run.UID)) >= 3
	})

	artifacts := h.artifacts(run.UID)
	rels := make(map[string]bool, len(artifacts))
	for _, artifact := range artifacts {
		spec, ok := artifact.Spec.(*urth.ArtifactSpec)
		require.True(t, ok, "artifact spec came back as %T", artifact.Spec)
		rels[spec.Artifact.Rel] = true
	}
	require.True(t, rels[testArtifactRel+"-0"], "the prober's own artifact is missing, got %v", rels)

	require.Empty(t, h.dispatchFailures(), "nothing about this run is a dead letter")
}

// A scenario that matches no active runner is terminal at creation, and files
// nothing on the queue.
//
// The POST still succeeds, because a scheduled trigger has no caller to hand a
// refusal to -- the refusal has to be recorded on the run itself.
func TestUnplaceableRunIsTerminalAndNeverQueued(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	h.applyRunner("windows-runner", manifest.Labels{"os": "windows"})
	scenario := h.applyScenario("linux-only", testProbSpec{Message: "unplaceable"},
		manifest.LabelSelector{MatchLabels: manifest.Labels{"os": "linux"}})

	run := h.createRun(scenario.Name)

	require.Equal(t, urth.JobErrored, run.Status.Status)
	require.Equal(t, string(urth.ReasonNoEligibleRunner), run.Labels[urth.LabelResultUnschedulable])
	require.Empty(t, h.outbox(run.UID), "an unplaceable run writes no dispatch for the relay to spend attempts on")

	published, err := h.relayOnce()
	require.NoError(t, err)
	require.Zero(t, published)
}
