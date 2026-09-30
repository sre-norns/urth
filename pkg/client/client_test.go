package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// recorder answers every request with status and body, and keeps what it saw.
type recorder struct {
	mu       sync.Mutex
	requests []*http.Request
	status   int
	body     string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	_, _ = w.Write([]byte(r.body))
}

func (r *recorder) last(t *testing.T) *http.Request {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("the server saw no request")
	}
	return r.requests[len(r.requests)-1]
}

func newTestClient(t *testing.T, status int, body string) (*RestAPIClient, *recorder) {
	t.Helper()
	rec := &recorder{status: status, body: body}
	server := httptest.NewServer(rec)
	t.Cleanup(server.Close)
	c, err := NewRestAPIClient(server.URL, APIClientConfig{Account: "acme", Project: "web", Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return c, rec
}

func scenarioManifest(version manifest.Version) manifest.ResourceManifest {
	return manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{APIVersion: "v1", Kind: "scenarios"},
		Metadata: manifest.ObjectMeta{Name: "probe", Version: version},
	}
}

func TestRequestsAreScopedToTheirAccountOrProject(t *testing.T) {
	c, rec := newTestClient(t, http.StatusOK, `{"items":[]}`)
	ctx := context.Background()

	for _, tc := range []struct {
		call func() error
		path string
	}{
		{func() error { _, _, err := c.Runners().List(ctx, manifest.SearchQuery{}); return err }, "/v1/accounts/acme/runners"},
		{func() error { _, _, err := c.Scenarios().List(ctx, manifest.SearchQuery{}); return err }, "/v1/projects/web/scenarios"},
		{func() error {
			_, _, err := c.Labels(urth.KindRunner).ListNames(ctx, manifest.SearchQuery{})
			return err
		}, "/v1/accounts/acme/search/runners/names"},
		{func() error {
			_, _, err := c.Scenarios().List(urth.WithScope(ctx, manifest.ScopeRef{Project: "other"}), manifest.SearchQuery{})
			return err
		}, "/v1/projects/other/scenarios"},
	} {
		if err := tc.call(); err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if got := rec.last(t).URL.Path; got != tc.path {
			t.Errorf("requested %s, want %s", got, tc.path)
		}
		if got := rec.last(t).Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("%s: Authorization %q", tc.path, got)
		}
	}
}

// A worker's own calls are authorised by its session, which already says whose
// they are; scoping them would address a route the session cannot use.
func TestWorkerSessionCallsAreNotScoped(t *testing.T) {
	c, rec := newTestClient(t, http.StatusOK, `{}`)
	if _, err := c.Workers().Heartbeat(context.Background(), "session", urth.WorkerHeartbeatRequest{}); err != nil {
		t.Fatal(err)
	}
	req := rec.last(t)
	if req.URL.Path != "/v1/auth/workers/heartbeat" {
		t.Errorf("heartbeat went to %s", req.URL.Path)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer session" {
		t.Errorf("heartbeat authenticated as %q, want the session", got)
	}
}

func TestWritesCarryTheirPrecondition(t *testing.T) {
	c, rec := newTestClient(t, http.StatusOK, `{"metadata":{"name":"probe"}}`)
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func(context.Context) error
		want string
	}{
		{"apply without a version", func(ctx context.Context) error {
			_, _, err := c.ApplyObjectDefinition(ctx, scenarioManifest(0))
			return err
		}, "*"},
		{"apply of a version read back", func(ctx context.Context) error {
			_, _, err := c.ApplyObjectDefinition(ctx, scenarioManifest(7))
			return err
		}, bark.ETag(7)},
		{"update", func(ctx context.Context) error {
			_, err := c.Scenarios().Update(ctx, manifest.NewVersionedID("probe", 3), scenarioManifest(0))
			return err
		}, bark.ETag(3)},
		{"an explicit If-Match replaces the derived one", func(ctx context.Context) error {
			_, _, err := c.ApplyObjectDefinition(WithRequestOptions(ctx, RequestOptions{IfMatch: `"9"`}), scenarioManifest(7))
			return err
		}, `"9"`},
	} {
		if err := tc.call(ctx); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := rec.last(t).Header.Get(bark.HTTPHeaderIfMatch); got != tc.want {
			t.Errorf("%s: If-Match %q, want %q", tc.name, got, tc.want)
		}
	}

	// A read takes no precondition, even when the caller supplied one for writes.
	if _, _, err := c.Scenarios().Get(WithRequestOptions(ctx, RequestOptions{IfMatch: `"9"`}), "probe"); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(t).Header.Get(bark.HTTPHeaderIfMatch); got != "" {
		t.Errorf("a read sent If-Match %q", got)
	}
}

func TestPostsCarryAnIdempotencyKey(t *testing.T) {
	c, rec := newTestClient(t, http.StatusCreated, `{"metadata":{"name":"probe"}}`)
	ctx := context.Background()

	if _, err := c.Scenarios().Create(ctx, scenarioManifest(0)); err != nil {
		t.Fatal(err)
	}
	first := rec.last(t).Header.Get(bark.HTTPHeaderIdempotencyKey)
	if _, err := c.Scenarios().Create(ctx, scenarioManifest(0)); err != nil {
		t.Fatal(err)
	}
	second := rec.last(t).Header.Get(bark.HTTPHeaderIdempotencyKey)
	if first == "" || first == second {
		t.Errorf("two POSTs sent keys %q and %q; each operation needs its own", first, second)
	}

	retry := WithRequestOptions(ctx, RequestOptions{IdempotencyKey: "create-probe"})
	if _, err := c.Scenarios().Create(retry, scenarioManifest(0)); err != nil {
		t.Fatal(err)
	}
	if got := rec.last(t).Header.Get(bark.HTTPHeaderIdempotencyKey); got != "create-probe" {
		t.Errorf("a retry sent key %q, want the caller's", got)
	}
}

// The worker tells a stale dispatch from a failure by the status of a
// bark.ErrorResponse, so a refusal must arrive as one.
func TestRefusalsAreErrorResponses(t *testing.T) {
	c, _ := newTestClient(t, http.StatusConflict, `{"code":409,"message":"superseded"}`)
	_, err := c.Results("probe").ClaimRun(context.Background(), "uid", "session", urth.ClaimJobRequest{})
	apiErr, ok := errors.AsType[*bark.ErrorResponse](err)
	if !ok || apiErr.Code != http.StatusConflict {
		t.Fatalf("claim refusal returned %#v, want a 409 bark.ErrorResponse", err)
	}
}
