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
		RunId manifest.ResourceName `uri:"runId" form:"runId" binding:"required"`
	}

	ScenarioRunResultArtifactRequest struct {
		ScenarioRunResultsRequest `uri:",inline" form:",inline" binding:"required"`
		ArtifactID                manifest.ResourceName `uri:"artifactId" form:"artifactId" binding:"required"`
	}

	// Job authorization request
	// Worker sends this authZ request to take a job, if allowed
	AuthJobRequest struct {
		WorkerID manifest.VersionedResourceID `form:"workerId" json:"workerId" yaml:"workerId" xml:"workerId"`
		RunnerID manifest.VersionedResourceID `form:"runnerId" json:"runnerId" yaml:"runnerId" xml:"runnerId"`
		Timeout  time.Duration                `form:"timeout" json:"timeout" yaml:"timeout" xml:"timeout"`
		Labels   manifest.Labels              `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
	}

	AuthJobResponse struct {
		bark.CreatedResponse `uri:",inline" form:",inline"`
		Token                ApiToken `form:"token" json:"token" yaml:"token" xml:"token"`
	}
)
