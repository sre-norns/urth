package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"golang.org/x/sync/errgroup"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// execute runs a claimed job and reports its outcome.
func (w *worker) execute(ctx context.Context, envelope natsq.DispatchEnvelope, auth urth.AuthJobResponse) {
	if auth.Prob.Spec == nil {
		// The execution snapshot arrives with the claim, so an empty one means
		// the server authorised a run it could not describe. Reporting it as a
		// failed run is more useful than silently doing nothing: the Result
		// stops sitting in `running` until its lease expires.
		log.Printf("run %v was authorised with no prob spec", envelope.ResultUID)
		w.reportFailure(ctx, auth, "server returned no prob spec for this run")
		return
	}

	timeout := w.runTimeout(auth)

	// Bounded by the server's deadline, not only by local configuration: past
	// that point the run capability stops working, so continuing to execute
	// produces results that cannot be uploaded.
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var playOptions []runner.PlayOption
	if w.config.StreamLogs && w.conn != nil {
		playOptions = append(playOptions,
			runner.WithLogPublisher(natsq.NewLogPublisher(w.conn, w.runnerUID, envelope.ResultUID)))
	}

	log.Printf("running %v: kind=%v timeout=%v", envelope.ResultUID, auth.Prob.Kind, timeout)

	runResult, artifacts, err := runner.Play(runCtx, auth.Prob,
		prob.RunOptions{
			HTTP: prob.HTTPOptions{
				CaptureResponseBody: false,
				CaptureRequestBody:  false,
				IgnoreRedirects:     false,
			},
			Puppeteer: prob.PuppeteerOptions{
				Headless:         true,
				WorkingDirectory: w.config.WorkingDirectory,
				TempDirPrefix:    string(envelope.ResultUID),
			},
		},
		playOptions...)
	if err != nil {
		// Not a reason to stop: the logs and status explaining the failure are
		// exactly what needs uploading. A probe that fails is a result, not an
		// error in the worker.
		log.Printf("run %v failed: %v", envelope.ResultUID, err)
	}

	w.report(ctx, envelope, auth, runResult, artifacts)
}

// runTimeout picks how long to allow the probe, respecting the server's lease.
func (w *worker) runTimeout(auth urth.AuthJobResponse) time.Duration {
	timeout := w.config.RunnerConfig.Timeout

	if auth.Prob.Timeout > 0 && auth.Prob.Timeout < timeout {
		timeout = auth.Prob.Timeout
	}

	// The server's deadline is a hard ceiling. Reserve a little of it for the
	// upload that follows, so a run that uses its whole budget still has time
	// to say what happened rather than being cut off mid-report.
	if !auth.Deadline.IsZero() {
		remaining := time.Until(auth.Deadline) - uploadReserve
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}

	if timeout <= 0 {
		timeout = time.Minute
	}

	return timeout
}

// uploadReserve is how much of the run's lease is held back for reporting.
const uploadReserve = 15 * time.Second

// report uploads artifacts and the final status under the run capability.
func (w *worker) report(ctx context.Context, envelope natsq.DispatchEnvelope, auth urth.AuthJobResponse, runResult urth.ResultStatus, artifacts []urth.ArtifactSpec) {
	// Reporting gets its own context, deliberately not derived from the run's.
	// The run context is cancelled the moment the probe finishes or times out,
	// and uploading through it means a timed-out run reports nothing at all --
	// losing precisely the logs that would explain the timeout.
	//
	// It is derived from context.Background rather than the worker's shutdown
	// context for the same reason: a worker asked to stop should still finish
	// telling the server about the run it already executed.
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
	defer cancel()

	labels := manifest.MergeLabels(
		w.config.LabelJob(w.runnerMeta, w.workerMeta, urth.Job{
			ResultName:   manifest.ResourceName(envelope.ResultUID),
			ScenarioName: envelope.ScenarioName,
		}),
		manifest.Labels{
			urth.LabelResultMessageID: envelope.DispatchID,
		},
	)

	// errgroup rather than grace.NewWorkgroup: that helper drops the error its
	// work items return (wyrd's Workgroup.run has a standing "TODO: Get error
	// and handle it"), which is how the asynq worker came to report a run as
	// complete whose artifacts had all failed to upload.
	group, groupCtx := errgroup.WithContext(reportCtx)
	group.SetLimit(4)

	artifactsAPI := w.apiClient.Artifacts()
	for _, a := range artifacts {
		artifact := a
		group.Go(func() error {
			_, err := artifactsAPI.Create(groupCtx, auth.Token, manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					APIVersion: "v1",
					Kind:       urth.KindArtifact,
				},
				Metadata: manifest.ObjectMeta{
					Name:   manifest.ResourceName(fmt.Sprintf("%v.%v", envelope.ResultUID, artifact.Artifact.Rel)),
					Labels: labels,
				},
				Spec: artifact,
			})
			if err != nil {
				return fmt.Errorf("failed to post artifact %q: %w", artifact.Artifact.Rel, err)
			}

			return nil
		})
	}

	resultsAPI := w.apiClient.Results(envelope.ScenarioName)
	group.Go(func() error {
		if _, err := resultsAPI.UpdateStatus(groupCtx, auth.VersionedResourceID, auth.Token, runResult); err != nil {
			return fmt.Errorf("failed to post run status: %w", err)
		}

		return nil
	})

	// The errors are surfaced rather than discarded. A run whose artifacts
	// failed to upload looks, from the UI, like a run that produced none --
	// and the asynq worker threw this return value away, so that failure mode
	// was invisible.
	if err := group.Wait(); err != nil {
		log.Printf("run %v: failed to report fully: %v", envelope.ResultUID, err)
		return
	}

	log.Printf("run %v completed: %v", envelope.ResultUID, runResult.Result)
}

// reportTimeout bounds how long the worker spends uploading a finished run.
const reportTimeout = 2 * time.Minute

// reportFailure records a run the worker could not even start.
func (w *worker) reportFailure(ctx context.Context, auth urth.AuthJobResponse, reason string) {
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
	defer cancel()

	status := urth.NewRunResults(prob.RunFinishedError, urth.WithStatus(urth.JobErrored))
	if _, err := w.apiClient.Results("").UpdateStatus(reportCtx, auth.VersionedResourceID, auth.Token, status); err != nil {
		log.Printf("failed to report run failure (%v): %v", reason, err)
	}
}
