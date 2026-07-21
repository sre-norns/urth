package urth

import (
	"time"

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
	AuthJobRequest struct {
		WorkerID manifest.VersionedResourceID `form:"workerId" json:"workerId" yaml:"workerId" xml:"workerId"`
		RunnerID manifest.VersionedResourceID `form:"runnerId" json:"runnerId" yaml:"runnerId" xml:"runnerId"`
		Timeout  time.Duration                `form:"timeout" json:"timeout" yaml:"timeout" xml:"timeout"`
		Labels   manifest.Labels              `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
	}

	AuthJobResponse struct {
		bark.CreatedResponse `uri:",inline" form:",inline"`
		Token                APIToken `form:"token" json:"token" yaml:"token" xml:"token"`
	}
)
