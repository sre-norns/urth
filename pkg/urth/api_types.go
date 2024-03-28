package urth

import (
	"time"

	"github.com/sre-norns/urth/pkg/bark"
	"github.com/sre-norns/urth/pkg/wyrd"
)

// Common domain-agnostic types used to create rich APIs
type (
	ScenarioRunResultsRequest struct {
		bark.ResourceRequest `uri:",inline" form:",inline" binding:"required"`
		RunId                wyrd.ResourceID `uri:"runId" form:"runId" binding:"required"`
	}

	ScenarioRunResultArtifactRequest struct {
		ScenarioRunResultsRequest `uri:",inline" form:",inline" binding:"required"`
		ArtifactID                wyrd.ResourceID `uri:"artifactId" form:"artifactId" binding:"required"`
	}

	// Job authorization request
	// Worker sends this authZ request to take a job, if allowed
	AuthJobRequest struct {
		RunnerID wyrd.VersionedResourceId `form:"runnerId" json:"runnerId" yaml:"runnerId" xml:"runnerId"`
		Timeout  time.Duration            `form:"timeout" json:"timeout" yaml:"timeout" xml:"timeout"`
		Labels   wyrd.Labels              `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
	}

	AuthJobResponse struct {
		bark.CreatedResponse `uri:",inline" form:",inline"`
		Token                ApiToken `form:"token" json:"token" yaml:"token" xml:"token"`
	}
)

func NewPaginatedResponse(data []PartialObjectMetadata, paginationInfo bark.Pagination) bark.PaginatedResponse[PartialObjectMetadata] {
	return bark.PaginatedResponse[PartialObjectMetadata]{
		Pagination: paginationInfo,
		Count:      len(data),
		Data:       data,
	}
}
