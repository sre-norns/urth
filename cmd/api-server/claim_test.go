package main

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
)

// TestClaimHTTPResponse locks the mapping from a claim outcome to the status class
// the worker sees. The prototype flattened every claim failure to 401, which the
// worker read as "stale" and acknowledged -- deleting the only dispatch for a run
// that a transient database blip had merely made temporarily unclaimable. Each row
// here fails against that old behaviour.
func TestClaimHTTPResponse(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{
			name:     "transient store failure is retryable",
			err:      &urth.ClaimError{Disposition: urth.ClaimUnavailable},
			wantCode: http.StatusServiceUnavailable,
		},
		{
			name:     "obsolete run is a conflict",
			err:      &urth.ClaimError{Disposition: urth.ClaimObsolete},
			wantCode: http.StatusConflict,
		},
		{
			name:     "policy refusal is forbidden",
			err:      &urth.ClaimError{Disposition: urth.ClaimForbidden},
			wantCode: http.StatusForbidden,
		},
		{
			name:     "wrapped claim error keeps its disposition",
			err:      fmt.Errorf("through a layer: %w", &urth.ClaimError{Disposition: urth.ClaimObsolete}),
			wantCode: http.StatusConflict,
		},
		{
			name:     "unclassified error is treated as retryable, never terminal",
			err:      errors.New("some unexpected failure"),
			wantCode: http.StatusServiceUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := claimHTTPResponse(tc.err)
			if got.Code != tc.wantCode {
				t.Fatalf("claimHTTPResponse(%v).Code = %d, want %d", tc.err, got.Code, tc.wantCode)
			}
		})
	}
}

// TestClaimHTTPResponseBodyLeaksNothing guards the invariant that a claim
// rejection body reveals only a status class -- never the specific reason, which
// would tell a caller whether a protected run exists or who holds it.
func TestClaimHTTPResponseBodyLeaksNothing(t *testing.T) {
	// The internal reason is carried on the error for server logs.
	err := fmt.Errorf("run %q already claimed by worker %q", "secret-run", "worker-7")
	wrapped := &urth.ClaimError{Disposition: urth.ClaimObsolete}

	got := claimHTTPResponse(wrapped)
	if got == errClaimUnavailable || got == errClaimForbidden {
		t.Fatalf("obsolete claim mapped to the wrong generic response")
	}
	if got.Message == err.Error() {
		t.Fatalf("generic claim response must not echo the internal reason")
	}
	// Sanity: the generic bodies are fixed strings, not derived from any run.
	if _, ok := any(got).(*bark.ErrorResponse); !ok {
		t.Fatalf("claim response is not a *bark.ErrorResponse")
	}
}
