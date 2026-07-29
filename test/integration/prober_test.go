package integration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// testProbKind is the one prob kind this suite runs.
//
// Registered rather than faked, because the api-server decodes a stored prob
// spec strictly against its registered type: a kind nobody registered falls back
// to an untyped map, which is not what a real scenario goes through. Registering
// it means every scenario here is authored, stored, snapshotted, claimed and
// executed exactly as an http or tcp scenario would be.
const testProbKind manifest.Kind = "urthtest"

// testProbSpec drives the prober from the scenario, not from a global.
//
// This is what makes the suite parallel-safe. A package-level "next run fails"
// switch would be read by whichever run happened to be executing, so two tests
// running at once would steer each other's probes; a spec field travels with
// the run, through the execution snapshot, to the worker that claimed it.
type testProbSpec struct {
	// Outcome is the run status to report. Empty means success.
	Outcome prob.RunStatus `form:"outcome,omitempty" json:"outcome,omitempty" yaml:"outcome,omitempty"`

	// Delay holds the probe for this long before reporting, so a test can
	// arrange for a run to still be executing when something else happens to it.
	Delay time.Duration `form:"delay,omitempty" json:"delay,omitempty" yaml:"delay,omitempty"`

	// Message is written to the run log, which is how a test tells one run's
	// stored log artifact from another's.
	Message string `form:"message,omitempty" json:"message,omitempty" yaml:"message,omitempty"`

	// Artifacts is how many artifacts to produce beyond the log and metrics
	// every run leaves behind.
	Artifacts int `form:"artifacts,omitempty" json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
}

// testProbVersion is reported as this prober's build, and reaches a worker's
// capability labels through runner.ProberAsLabels. It has to satisfy the label
// grammar or every worker in this suite fails registration.
const testProbVersion = "v0.0.1-test"

// registerTestProb makes the kind available to every component in the test
// binary: the api-server's manifest decoding, the worker's capability labels,
// and runner.Play's dispatch to a run function.
func registerTestProb() error {
	return prob.RegisterProbKind(testProbKind, &testProbSpec{}, prob.ProbRegistration{
		RunFunc:     runTestProb,
		Version:     testProbVersion,
		ContentType: "",
		Produce:     []string{testArtifactRel},
	})
}

// testArtifactRel names the artifacts this prober produces on request.
const testArtifactRel = "test-artifact"

// probeRuns counts executions, keyed by the spec's Message.
//
// "The probe ran exactly once" is the assertion half this suite turns on -- a
// lost claim response must not run a scenario twice, and neither must a
// redelivery -- and it can only be made where the probe actually runs. Keyed
// rather than a single counter for the same reason the prober is spec-driven:
// every scenario here sets a Message of its own, so two tests running in
// parallel count their own probes and not each other's.
var probeRuns sync.Map // string -> *atomic.Int64

// probeRunCount reports how many times a probe with this message has run.
func probeRunCount(message string) int64 {
	counter, ok := probeRuns.Load(message)
	if !ok {
		return 0
	}

	return counter.(*atomic.Int64).Load()
}

func countProbeRun(message string) {
	counter, _ := probeRuns.LoadOrStore(message, &atomic.Int64{})
	counter.(*atomic.Int64).Add(1)
}

// runTestProb is the deterministic probe: it does exactly what its spec says and
// reaches nothing outside the process.
//
// Requirement from the task: no scenario may depend on the public internet or on
// developer machine state. A prob that resolves a name or opens a socket would
// make a red suite ambiguous between "the dispatch path broke" and "the network
// is having a day".
func runTestProb(ctx context.Context, spec any, _ prob.RunOptions, _ *prometheus.Registry, logger *slog.Logger) (prob.RunStatus, []prob.Artifact, error) {
	probSpec, ok := spec.(*testProbSpec)
	if !ok {
		return prob.RunFinishedError, nil, fmt.Errorf("test prob received a %T, not a *testProbSpec", spec)
	}

	countProbeRun(probSpec.Message)

	logger.Info("test prob starting", "message", probSpec.Message, "delay", probSpec.Delay)

	if probSpec.Delay > 0 {
		select {
		case <-ctx.Done():
			// A cancelled probe is a canceled run, not an error in the worker.
			// This is the path a test takes when it kills a worker mid-run.
			return prob.RunFinishedCanceled, nil, ctx.Err()
		case <-time.After(probSpec.Delay):
		}
	}

	artifacts := make([]prob.Artifact, 0, probSpec.Artifacts)
	for i := range probSpec.Artifacts {
		artifacts = append(artifacts, prob.Artifact{
			Rel:      fmt.Sprintf("%s-%d", testArtifactRel, i),
			MimeType: "text/plain",
			// Declared clean because it is: this content is generated here and
			// derives from nothing an operator gave us. An artifact that made no
			// declaration would be labelled as possibly secret-bearing, which is
			// the correct default and the wrong claim for this one.
			DataClass: prob.DataClassClean,
			Content:   []byte(fmt.Sprintf("%s artifact %d", probSpec.Message, i)),
		})
	}

	outcome := probSpec.Outcome
	if outcome == prob.RunNotFinished {
		outcome = prob.RunFinishedSuccess
	}

	logger.Info("test prob finished", "outcome", outcome)

	return outcome, artifacts, nil
}
