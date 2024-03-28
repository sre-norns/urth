package bark_test

import (
	"fmt"
	"testing"

	"github.com/sre-norns/urth/pkg/bark"
	"github.com/stretchr/testify/require"
)

func Test_NewErrorResponse(t *testing.T) {
	type givenT struct {
		Status  int
		Error   error
		Options []bark.HResponseOption
	}

	testCases := map[string]struct {
		given  givenT
		expect *bark.ErrorResponse
	}{
		"base": {
			given: givenT{
				Status: 1,
				Error:  fmt.Errorf("hello"),
			},
			expect: &bark.ErrorResponse{
				Code:    1,
				Message: "hello",
			},
		},
		"nil-error-wo-options": {
			given: givenT{
				Status: 200,
				Error:  nil,
			},
			expect: nil,
		},
		"nil-error-w-options": {
			given: givenT{
				Status: 200,
				Error:  nil,
				Options: []bark.HResponseOption{
					bark.WithLink("details", bark.HLink{Reference: "321", Relationship: "xyz"}),
				},
			},
			expect: &bark.ErrorResponse{
				Code:    200,
				Message: "",

				HResponse: bark.HResponse{
					Links: map[string]bark.HLink{
						"details": {
							Reference:    "321",
							Relationship: "xyz",
						},
					},
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := bark.NewErrorResponse(test.given.Status, test.given.Error, test.given.Options...)

			require.Equal(t, test.expect, got)
		})
	}
}

func TestPagination_ClampLimit(t *testing.T) {
	testCases := map[string]struct {
		given      bark.Pagination
		givenLimit uint
		expect     bark.Pagination
	}{
		"all-defaults": {
			given:      bark.Pagination{},
			givenLimit: 0,
			expect:     bark.Pagination{},
		},
		"default-limit": {
			given: bark.Pagination{
				Offset: 120,
			},
			givenLimit: 32,
			expect: bark.Pagination{
				Offset: 120,
				Limit:  32,
			},
		},
		"request-above-limit": {
			given: bark.Pagination{
				Offset: 0,
				Limit:  2000,
			},
			givenLimit: 100,
			expect: bark.Pagination{
				Offset: 0,
				Limit:  100,
			},
		},
		"given-under-limit": {
			given: bark.Pagination{
				Offset: 0,
				Limit:  100,
			},
			givenLimit: 2000,
			expect: bark.Pagination{
				Offset: 0,
				Limit:  100,
			},
		},
		"limit-zero": {
			given: bark.Pagination{
				Offset: 32,
				Limit:  2000,
			},
			givenLimit: 0,
			expect: bark.Pagination{
				Offset: 32,
				Limit:  0,
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := test.given.ClampLimit(test.givenLimit)

			require.Equal(t, test.expect, got)
		})
	}
}
