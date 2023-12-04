package urth

import (
	"fmt"
	"time"

	"github.com/sre-norns/urth/pkg/wyrd"
)

type (

	// TODO: Replace with GORM pagination middleware
	Pagination struct {
		Offset uint `uri:"offset" form:"offset" json:"offset" yaml:"offset" xml:"offset"`
		Limit  uint `uri:"limit" form:"limit" json:"limit" yaml:"limit" xml:"limit"`
	}

	SearchQuery struct {
		Pagination `uri:",inline" form:",inline"`
		Labels     string `uri:"labels" form:"labels" json:"labels" yaml:"labels" xml:"labels"`
	}

	VersionQuery struct {
		Version uint64 `uri:"version" form:"version" binding:"required"`
	}

	ResourceRequest struct {
		ID wyrd.ResourceID `uri:"id" form:"id" binding:"required"`
	}

	ScenarioRunResultsRequest struct {
		ResourceRequest `uri:",inline" form:",inline" binding:"required"`
		RunId           wyrd.ResourceID `uri:"runId" form:"runId" binding:"required"`
	}

	ScenarioRunResultArtifactRequest struct {
		ScenarioRunResultsRequest `uri:",inline" form:",inline" binding:"required"`
		ArtifactID                wyrd.ResourceID `uri:"artifactId" form:"artifactId" binding:"required"`
	}

	PaginatedResponse[T any] struct {
		Pagination `form:",inline" json:",inline" yaml:",inline"`

		Count int `form:"count" json:"count" yaml:"count" xml:"count"`
		Data  []T `form:"data" json:"data" yaml:"data" xml:"data"`
	}

	ErrorResponse struct {
		Code    int
		Message string
	}

	CreatedResponse struct {
		// Gives us kind info
		wyrd.TypeMeta `json:",inline" yaml:",inline"`

		VersionedResourceId `json:",inline" yaml:",inline"`
	}

	CreatedRunResponse struct {
		CreatedResponse `uri:",inline" form:",inline"`
		Token           ApiToken `form:"token" json:"token" yaml:"token" xml:"token"`
	}

	AuthRunRequest struct {
		RunnerID VersionedResourceId `form:"runnerId" json:"runnerId" yaml:"runnerId" xml:"runnerId"`
		Timeout  time.Duration       `form:"timeout" json:"timeout" yaml:"timeout" xml:"timeout"`
		Labels   wyrd.Labels         `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
		// Token    ApiToken   `form:"token" json:"token" yaml:"token" xml:"token"`
	}
)

func NewErrorResponse(statusCode int, err error) ErrorResponse {
	return ErrorResponse{
		Code:    statusCode,
		Message: err.Error(),
	}
}

func NewPaginatedResponse(data []PartialObjectMetadata, paginationInfo Pagination) PaginatedResponse[PartialObjectMetadata] {
	return PaginatedResponse[PartialObjectMetadata]{
		Pagination: paginationInfo,
		Count:      len(data),
		Data:       data,
	}
}

func (p *Pagination) ClampLimit(maxLimit uint) {
	if p.Limit > maxLimit || p.Limit == 0 {
		p.Limit = maxLimit
	}
}

// ErrorResponse implements error interface
func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %s", e.Code, e.Message)
}
