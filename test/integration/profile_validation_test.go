package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// Exercise the custom mounted profile handler, not only the generic resource
// validation helper. identity/v0.7.2 fixes this route's canonical field paths.
func TestProfileValidationUsesCanonicalPaths(t *testing.T) {
	h := newHarness(t)
	code, headers, body := h.identityRequest("GET", "/v1/profile", nil, nil)
	require.Equal(t, http.StatusOK, code, string(body))
	require.NotEmpty(t, headers.Get("ETag"))
	code, _, body = h.identityRequest("PATCH", "/v1/profile", map[string]string{"If-Match": headers.Get("ETag")}, map[string]any{"spec": map[string]any{"displayName": "   "}})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(body))
	require.Contains(t, string(body), `"spec.displayName"`)
	require.NotContains(t, string(body), `"display_name"`)
	code, _, body = h.identityRequest("PATCH", "/v1/profile", map[string]string{"If-Match": headers.Get("ETag")}, map[string]any{"spec": map[string]any{"displayName": "Research owner"}})
	require.Equal(t, http.StatusOK, code, string(body))
	require.Contains(t, string(body), `"displayName":"Research owner"`)
}
