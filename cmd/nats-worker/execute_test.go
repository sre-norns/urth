package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// cancelGrace is how long a work item waits to see whether the first upload
// failure cancelled the group it belongs to. A fail-fast group cancels within
// microseconds of the failing item returning, so a scheduling quantum is
// plenty; a group that never cancels pays this wait once, concurrently.
const cancelGrace = 250 * time.Millisecond

// reportRecorder is the shared state behind the stub artifact and results APIs:
// it fails one nominated artifact upload and records what the rest of the report
// managed to do afterwards.
//
// Both stubs honour the context they are given, as a real API client does -- an
// HTTP request on a cancelled context fails before it reaches the wire. That is
// what makes the difference between reporting policies observable: under a
// fail-fast group the survivors are handed an already-cancelled context.
type reportRecorder struct {
	failRel string

	// failed is closed once the failing upload has returned its error, so the
	// other work items only reach their context check when there is a failure
	// for a fail-fast group to have reacted to.
	failed    chan struct{}
	closeOnce sync.Once

	mu           sync.Mutex
	attempted    []string
	uploaded     []string
	statusPosted bool
	statusErr    error
}

func newReportRecorder(failRel string) *reportRecorder {
	return &reportRecorder{
		failRel: failRel,
		failed:  make(chan struct{}),
	}
}

// awaitFailure blocks until the nominated upload has failed, then reports
// whether this work item's own context survived that failure.
func (r *reportRecorder) awaitFailure(ctx context.Context) error {
	select {
	case <-r.failed:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(cancelGrace):
		return nil
	}
}

func (r *reportRecorder) record(dst *[]string, rel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	*dst = append(*dst, rel)
}

type stubArtifacts struct {
	urth.ArtifactAPI
	rec *reportRecorder
}

func (s stubArtifacts) Create(ctx context.Context, _ urth.APIToken, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	spec, ok := entry.Spec.(urth.ArtifactSpec)
	if !ok {
		return entry, errors.New("artifact create called with a non-artifact spec")
	}

	rel := spec.Artifact.Rel
	s.rec.record(&s.rec.attempted, rel)

	if rel == s.rec.failRel {
		s.rec.closeOnce.Do(func() { close(s.rec.failed) })
		return entry, errors.New("artifact storage is full")
	}

	if err := s.rec.awaitFailure(ctx); err != nil {
		return entry, err
	}

	s.rec.record(&s.rec.uploaded, rel)
	return entry, nil
}

type stubStatusResults struct {
	urth.RunResultAPI
	rec *reportRecorder
}

func (s stubStatusResults) UpdateStatus(ctx context.Context, _ manifest.VersionedResourceID, _ urth.APIToken, _ urth.ResultStatus) (bark.CreatedResponse, error) {
	err := s.rec.awaitFailure(ctx)

	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.statusErr = err
	s.rec.statusPosted = err == nil

	return bark.CreatedResponse{}, err
}

func newReportingWorker(rec *reportRecorder) *worker {
	return &worker{
		config: &workerConfig{RunnerConfig: runner.NewDefaultConfig()},
		apiClient: stubService{
			results:   stubStatusResults{rec: rec},
			artifacts: stubArtifacts{rec: rec},
		},
	}
}

func artifact(rel string) urth.ArtifactSpec {
	return urth.ArtifactSpec{
		Artifact: prob.Artifact{
			Rel:       rel,
			MimeType:  "text/plain",
			DataClass: prob.DataClassRedacted,
			Content:   []byte("content of " + rel),
		},
	}
}

// TestReportSurvivesArtifactFailure is the regression that motivated moving off
// a fail-fast group: one artifact that cannot be uploaded must not take the run
// down with it.
//
// The final status post is the part that matters. Without it the run sits in
// `running` until its lease expires, so a transient storage failure on a single
// log file turns into a run that never visibly finished -- a far worse outcome
// than a run missing one artifact.
func TestReportSurvivesArtifactFailure(t *testing.T) {
	rec := newReportRecorder("logs")
	w := newReportingWorker(rec)

	artifacts := []urth.ArtifactSpec{
		artifact("logs"), // fails
		artifact("har"),
		artifact("screenshot"),
	}

	w.report(context.Background(),
		natsq.DispatchEnvelope{ResultUID: "run-1", ScenarioName: "scenario-1", DispatchID: "dispatch-1"},
		urth.AuthJobResponse{},
		urth.NewRunResults(prob.RunFinishedSuccess),
		artifacts,
	)

	rec.mu.Lock()
	defer rec.mu.Unlock()

	if !rec.statusPosted {
		t.Errorf("run status was not posted after an artifact upload failed: %v", rec.statusErr)
	}

	if len(rec.attempted) != len(artifacts) {
		t.Errorf("attempted %v uploads, want all %d attempted", rec.attempted, len(artifacts))
	}

	if len(rec.uploaded) != len(artifacts)-1 {
		t.Errorf("uploaded %v, want the two artifacts that did not fail", rec.uploaded)
	}
}
