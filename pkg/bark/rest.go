package bark

import (
	"fmt"
)

// Common domain-agnostic types used to create rich REST APIs
type (

	// Pagination is a set of common pagination query params
	Pagination struct {
		Offset uint `uri:"offset" form:"offset" json:"offset" yaml:"offset" xml:"offset"`
		Limit  uint `uri:"limit" form:"limit" json:"limit" yaml:"limit" xml:"limit"`
	}

	// SearchQuery provides grouping of of query parameters commonly used by REST endpoint that perform search
	SearchQuery struct {
		Pagination `uri:",inline" form:",inline"`
		Filter     string `uri:"labels" form:"labels" json:"labels" yaml:"labels" xml:"labels"`
	}
)

// API Response types
type (
	// Semantic web links representing action that can be performed on response item
	HLink struct {
		Reference    string `form:"ref" json:"ref" yaml:"ref" xml:"ref"`
		Relationship string `form:"rel" json:"rel" yaml:"rel" xml:"rel"`
	}

	// A response that has semantic references
	HResponse struct {
		Links map[string]HLink `form:"_links" json:"_links" yaml:"_links" xml:"_links"`
	}

	// PaginatedResponse repsents common frame used to produce response that returns a collection of results
	PaginatedResponse[T any] struct {
		Count int `form:"count" json:"count" yaml:"count" xml:"count"`
		Data  []T `form:"data" json:"data" yaml:"data" xml:"data"`

		HResponse  `form:",inline" json:",inline" yaml:",inline"`
		Pagination `form:",inline" json:",inline" yaml:",inline"`
	}

	// ErrorResponse represents a single error response with human readable reason and a code.
	ErrorResponse struct {
		// Error code reprents error ID from a relevant domain
		Code int

		// Human readable representation of the error, suitable for display
		Message string

		HResponse `form:",inline" json:",inline" yaml:",inline"`
	}
)

type HResponseOption func(r *HResponse)

func WithLink(role string, link HLink) HResponseOption {
	return func(r *HResponse) {
		if r == nil {
			return
		}

		if r.Links == nil {
			r.Links = make(map[string]HLink)
		}

		r.Links[role] = link
	}
}

// Create a new error response object from an error
func NewErrorResponse(statusCode int, err error, options ...HResponseOption) (result *ErrorResponse) {
	if err == nil && len(options) == 0 {
		return
	}

	message := ""
	if err != nil {
		message = err.Error()
	}

	result = &ErrorResponse{
		Code:    statusCode,
		Message: message,
	}

	for _, o := range options {
		o(&result.HResponse)
	}

	return
}

// ErrorResponse implements error interface
func (e *ErrorResponse) Error() string {
	return fmt.Sprintf("%v %s", e.Code, e.Message)
}

// Createa a new pagination object that has .Limit
// set to in between [0, maxLimit].
// If current value of of .Limit is within the range then it is used,
// if the value outside of [0, maxLimit] range, maxLimit is used.
func (p Pagination) ClampLimit(maxLimit uint) Pagination {
	result := Pagination{
		Offset: p.Offset,
		Limit:  p.Limit,
	}

	if result.Limit > maxLimit || result.Limit == 0 {
		result.Limit = maxLimit
	}

	return result
}
