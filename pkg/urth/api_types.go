package urth

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

	ResourceRequest struct {
		ID ResourceID `uri:"id" form:"id" binding:"required"`
	}

	ScenarioRunResultsRequest struct {
		ResourceRequest `uri:",inline" form:",inline" binding:"required"`
		RunResultsID    ResourceID `uri:"runId" form:"runId" binding:"required"`
	}

	PaginatedResponse[T any] struct {
		Pagination `form:",inline" json:",inline" yaml:",inline"`

		Count int `form:"count" json:"count" yaml:"count" xml:"count"`
		Data  []T `form:"data" json:"data" yaml:"data" xml:"data"`
	}

	ErrorRepose struct {
		Code    string
		Message string
	}

	CreatedResponse struct {
		// Gives us kind info
		TypeMeta `json:",inline" yaml:",inline"`

		VersionedResourceId `json:",inline" yaml:",inline"`
	}

	CreatedRunResponse struct {
		CreatedResponse `uri:",inline" form:",inline"`
		Token           ApiToken `form:"token" json:"token" yaml:"token" xml:"token"`
	}

	CreateScenarioManualRunRequest struct {
		Token ApiToken `form:"token" json:"token" yaml:"token" xml:"token"`
	}

	ManualRunRequestResponse struct {
		RunId RunId `form:"id" json:"id" yaml:"id" xml:"id"`
	}
)

func NewErrorRepose(httpCode string, err error) ErrorRepose {
	return ErrorRepose{
		Code:    httpCode,
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

func (r ResourceRequest) ResourceID() ResourceID {
	return ResourceID(r.ID)
}

func (p *Pagination) ClampLimit(maxLimit uint) {
	if p.Limit > maxLimit || p.Limit == 0 {
		p.Limit = maxLimit
	}
}
