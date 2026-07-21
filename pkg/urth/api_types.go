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
