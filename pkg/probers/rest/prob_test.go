package rest

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	httpparser "github.com/sre-norns/urth/pkg/http-parser"
	"github.com/sre-norns/urth/pkg/prob"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestIsSensitiveHeader(t *testing.T) {
	sensitive := []string{
		"Authorization",
		"authorization",
		"Proxy-Authorization",
		"Cookie",
		"Set-Cookie",
		"Www-Authenticate",
		"X-Api-Key",
		"X-Auth-Token",
		"X-Session-Id",
		"X-Client-Secret",
		"X-Request-Signature",
	}

	for _, header := range sensitive {
		t.Run("sensitive/"+header, func(t *testing.T) {
			require.True(t, isSensitiveHeader(header))
		})
	}

	// Matching on whole '-' separated segments rather than substrings: a
	// substring match reports "X-Monkey-Id" as sensitive because it contains
	// "key", and redacting ordinary headers makes a failed run harder to read.
	notSensitive := []string{
		"Content-Type",
		"Content-Length",
		"Accept",
		"User-Agent",
		"X-Request-Id",
		"X-Monkey-Id",
		"X-Donkey-Name",
		"Keep-Alive",
	}

	for _, header := range notSensitive {
		t.Run("not-sensitive/"+header, func(t *testing.T) {
			require.False(t, isSensitiveHeader(header))
		})
	}
}

func TestFormatRequestRedactsCredentials(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/thing", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("Cookie", "sid=secret-cookie-value")
	req.Header.Set("Content-Type", "application/json")

	got := formatRequest(req)

	require.NotContains(t, got, "super-secret-token")
	require.NotContains(t, got, "secret-cookie-value")
	require.Contains(t, got, "Authorization: "+redactedPlaceholder)
	require.Contains(t, got, "Cookie: "+redactedPlaceholder)

	// Redaction has to stop short of making the log useless.
	require.Contains(t, got, "Content-Type: application/json")
	require.Contains(t, got, "/thing")
}

// The credential in a response is the one the target just issued. It reaches the
// log by a different function, which the original fix did not cover.
func TestFormatResponseRedactsCredentials(t *testing.T) {
	resp := &http.Response{Proto: "HTTP/1.1", Status: "200 OK", Header: http.Header{}}
	resp.Header.Set("Set-Cookie", "session=issued-session-value")
	resp.Header.Set("Content-Type", "text/html")

	got := formatResponse(resp)

	require.NotContains(t, got, "issued-session-value")
	require.Contains(t, got, "Set-Cookie: "+redactedPlaceholder)
	require.Contains(t, got, "Content-Type: text/html")
	require.Contains(t, got, "200 OK")
}

// Credentials are routinely passed as query parameters, and there is no reliable
// way to tell which parameter holds one.
func TestFormatRequestOmitsQueryString(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com/thing?api_key=secret-in-query", nil)
	require.NoError(t, err)

	got := formatRequest(req)

	require.NotContains(t, got, "secret-in-query")
	require.Contains(t, got, "/thing")
}

// A HAR recording is captured so that a run can be replayed and diffed against
// earlier ones, which requires it to be a faithful copy of the exchange. This
// test exists to stop the redaction above from being extended here: doing so
// would leave the artifact unable to serve its only purpose.
//
// The protection for a HAR is its data class, not redaction -- see
// TestHarArtifactDeclaresItselfSecretBearing.
func TestHarArtifactPreservesTheExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "session=issued-session-value; Path=/")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/thing", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer super-secret-token")

	_, artifacts, err := RunHTTPRequests(t.Context(), []httpparser.TestRequest{{Request: req}}, prob.RunOptions{}, discardLogger())
	require.NoError(t, err)

	har := findArtifact(t, artifacts, "har")
	content := string(har.Content)

	require.Contains(t, content, "super-secret-token",
		"a redacted HAR cannot be replayed; protect it by classification instead")
	require.Contains(t, content, "issued-session-value")
}

func TestHarArtifactDeclaresItselfSecretBearing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/thing", nil)
	require.NoError(t, err)

	_, artifacts, err := RunHTTPRequests(t.Context(), []httpparser.TestRequest{{Request: req}}, prob.RunOptions{}, discardLogger())
	require.NoError(t, err)

	har := findArtifact(t, artifacts, "har")

	require.Equal(t, prob.DataClassSecretBearing, har.DataClass)
	require.True(t, har.DataClass.MayContainSecrets())
}

func findArtifact(t *testing.T, artifacts []prob.Artifact, rel string) prob.Artifact {
	t.Helper()

	for _, a := range artifacts {
		if a.Rel == rel {
			return a
		}
	}

	var rels []string
	for _, a := range artifacts {
		rels = append(rels, a.Rel)
	}
	t.Fatalf("no %q artifact produced, got: %v", rel, strings.Join(rels, ", "))

	return prob.Artifact{}
}
