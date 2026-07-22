package urth

import (
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Common domain-agnostic types used to create rich APIs
// `uri:",inline" form:",inline"`
type (
	ScenarioRunResultsRequest struct {
		bark.ResourceRequest
		RunID manifest.ResourceName `uri:"runId" form:"runId" binding:"required"`
	}

	ScenarioRunResultArtifactRequest struct {
		ScenarioRunResultsRequest `uri:",inline" form:",inline" binding:"required"`
		ArtifactID                manifest.ResourceName `uri:"artifactId" form:"artifactId" binding:"required"`
	}

	// ProbKindInfo describes a kind of prob the server knows about.
	//
	// The server owns this registry. A scenario's prob is not an arbitrary
	// script: it is one of a known set of kinds with a known spec shape, and a
	// client offering someone a choice should be offering the server's list
	// rather than one it carries itself and has to keep in step.
	ProbKindInfo struct {
		// Kind identifies the prob type, as used in a scenario manifest.
		Kind string `form:"kind" json:"kind" yaml:"kind" xml:"kind"`

		// Version of the prober module that implements this kind.
		Version string `form:"version,omitempty" json:"version,omitempty" yaml:"version,omitempty" xml:"version,omitempty"`

		// ContentType of the prob's script, where the kind takes one. Kinds that
		// are configured rather than scripted leave this empty, which is how a
		// client can tell a script editor is wanted.
		ContentType string `form:"contentType,omitempty" json:"contentType,omitempty" yaml:"contentType,omitempty" xml:"contentType,omitempty"`

		// Produce lists the kinds of artifact a run of this prob is expected to
		// leave behind.
		Produce []string `form:"produce,omitempty" json:"produce,omitempty" yaml:"produce,omitempty" xml:"produce,omitempty"`
	}

	// SetPausedRequest asks the server to stop or resume a worker taking new
	// jobs. It is a request of its own rather than an update to the worker
	// resource, because a worker rewrites its own record whenever it registers:
	// an operator's decision has to be applied to a field the worker cannot
	// reach.
	SetPausedRequest struct {
		IsPaused bool `form:"paused" json:"paused" yaml:"paused" xml:"paused"`
	}

	// AuthJobRequest is a job authorization request: a worker sends it to take
	// a job, if allowed.
	//
	// WorkerID and RunnerID are honoured only on the legacy, unauthenticated
	// claim route used by the asynq prototype. The session-authenticated route
	// derives both from the bearer token and ignores whatever is in the body,
	// because a request body is not evidence of identity.
	AuthJobRequest struct {
		WorkerID manifest.VersionedResourceID `form:"workerId" json:"workerId" yaml:"workerId" xml:"workerId"`
		RunnerID manifest.VersionedResourceID `form:"runnerId" json:"runnerId" yaml:"runnerId" xml:"runnerId"`
		Timeout  time.Duration                `form:"timeout" json:"timeout" yaml:"timeout" xml:"timeout"`
		Labels   manifest.Labels              `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
	}

	// ClaimJobRequest is a worker's request to take a dispatched job.
	//
	// The worker presents its session as a bearer token and names the dispatch
	// it is responding to. Nothing here identifies the worker: that comes from
	// the session.
	ClaimJobRequest struct {
		// DispatchID is the dispatch this claim answers, taken from the queue
		// message. It is the idempotency key: a worker whose claim succeeded
		// but whose response was lost presents the same value again and
		// recovers the same authorization rather than starting a second run.
		DispatchID string `form:"dispatchId" json:"dispatchId" yaml:"dispatchId" xml:"dispatchId" binding:"required"`

		// ResultVersion is the Result version the dispatch was published for,
		// letting the server reject a message overtaken by newer state.
		ResultVersion manifest.Version `form:"resultVersion" json:"resultVersion" yaml:"resultVersion" xml:"resultVersion"`

		// Timeout is how long the worker intends to spend. The server clamps it
		// to its own limit: a worker may ask for less than the server allows,
		// never more.
		Timeout time.Duration `form:"timeout,omitempty" json:"timeout,omitempty" yaml:"timeout,omitempty" xml:"timeout,omitempty"`

		// Labels the worker offers about this run. Advisory only -- server-owned
		// labels are merged last and win.
		Labels manifest.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
	}

	AuthJobResponse struct {
		bark.CreatedResponse `uri:",inline" form:",inline"`

		// Token is the run capability: authority to update this Result's status
		// and post its artifacts, and nothing else.
		Token APIToken `form:"token" json:"token" yaml:"token" xml:"token"`

		// Prob is the execution snapshot -- what to actually run.
		//
		// It is delivered here, in the response to an authenticated claim,
		// rather than in the queue message. That is the whole reason the
		// dispatch envelope is nearly empty: possession of a queue message
		// should not reveal a scenario's script, so the job is only disclosed
		// to a worker that has proved it is entitled to run it.
		Prob prob.Manifest `form:"prob,omitempty" json:"prob,omitempty" yaml:"prob,omitempty" xml:"prob,omitempty"`

		// Scenario names the scenario this run belongs to.
		Scenario manifest.ResourceName `form:"scenario,omitempty" json:"scenario,omitempty" yaml:"scenario,omitempty" xml:"scenario,omitempty"`

		// Deadline is when the server stops accepting writes for this run. It
		// is the server's number, not the worker's request, and the same value
		// is recorded on the Result.
		Deadline time.Time `form:"deadline,omitempty" json:"deadline,omitempty" yaml:"deadline,omitempty" xml:"deadline,omitempty"`
	}
)
