package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/apiserver"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func oauthBoundaryHarness(t *testing.T, proxies ...string) *harness {
	t.Helper()
	return newHarness(t, withConfig(func(cfg *apiserver.Config) {
		id := identity.DefaultConfig()
		id.AuthenticationRateLimit = 10
		id.Clients = map[string][]string{apiserver.CLIClientID: {}}
		cfg.Identity = &id
		cfg.TrustedProxies = proxies
	}))
}

func (h *harness) m9Claim(name manifest.ResourceName) (urth.Result, urth.WorkerRegistrationResponse, urth.AuthJobResponse) {
	h.t.Helper()
	runner := h.applyRunner(name, nil)
	scenario := h.applyScenario(name, testProbSpec{}, manifest.LabelSelector{})
	run := h.createRun(scenario.Name)
	token := h.enrolmentToken(runner.Name)
	entry := urth.WorkerInstance{ObjectMeta: manifest.ObjectMeta{Name: name}}.ToManifest()
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, entry)
	require.NoError(h.t, err)
	auth, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(h.t, err)
	return h.result(run.UID), registration, auth
}

func (h *harness) m9Status(run urth.Result, token urth.APIToken, result prob.RunStatus) (int, []byte) {
	h.t.Helper()
	body, err := json.Marshal(urth.ResultStatus{Result: result})
	require.NoError(h.t, err)
	path := fmt.Sprintf("/v1/scenarios/%s/results/%s/status?version=%d", run.Spec.Execution.ScenarioName, run.UID, run.Version)
	return h.httpRequest(http.MethodPut, path, token, body)
}

func TestM9ClaimCapabilityCannotRewriteTerminalRun(t *testing.T) {
	h := newHarness(t)
	run, _, auth := h.m9Claim("terminal-capability")
	code, data := h.m9Status(run, auth.Token, prob.RunFinishedSuccess)
	require.Equal(t, http.StatusOK, code, string(data))
	completed := h.result(run.UID)
	code, data = h.m9Status(completed, auth.Token, prob.RunFinishedError)
	require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
	require.Equal(t, prob.RunFinishedSuccess, h.result(run.UID).Status.Result, "the original result must stay unchanged")
	// Final artifacts often arrive after completion. Their authority ends at
	// the original capability expiry, rather than at the status transition.
	artifact := urth.Artifact{ObjectMeta: manifest.ObjectMeta{Name: "final-artifact"}, Spec: urth.ArtifactSpec{Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain", Content: []byte("final log")}}}
	body, err := json.Marshal(artifact.ToManifest())
	require.NoError(t, err)
	code, data = h.httpRequest(http.MethodPost, "/v1/artifacts", auth.Token, body)
	require.Equal(t, http.StatusCreated, code, string(data))
}

func TestM9CredentialPurposesAndWrongResult(t *testing.T) {
	h := newHarness(t)
	run, worker, auth := h.m9Claim("credential-purposes")
	enrolment := h.enrolmentToken("credential-purposes")
	for _, credential := range []urth.APIToken{h.token, enrolment, worker.Session} {
		code, data := h.m9Status(run, credential, prob.RunFinishedSuccess)
		require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
	}
	for _, credential := range []urth.APIToken{enrolment, worker.Session, auth.Token} {
		code, data := h.httpRequest(http.MethodGet, fmt.Sprintf("/v1/accounts/%s/runners", h.scope.Account), credential, nil)
		require.Equal(t, http.StatusUnauthorized, code, string(data))
	}
	other := h.createRun("credential-purposes")
	code, data := h.m9Status(other, auth.Token, prob.RunFinishedSuccess)
	require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
	require.Equal(t, urth.JobPending, h.result(other.UID).Status.Status)
	// Neither a user nor a run credential can obtain internal claim authority.
	claim, err := json.Marshal(urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(other.UID, other.Version), ResultVersion: other.Version})
	require.NoError(t, err)
	for _, credential := range []urth.APIToken{h.token, enrolment, auth.Token} {
		code, data = h.httpRequest(http.MethodPost, fmt.Sprintf("/v1/auth/runs/%s/claim", other.UID), credential, claim)
		require.Equal(t, http.StatusForbidden, code, string(data))
	}
	code, data = h.m9Status(run, auth.Token, prob.RunFinishedSuccess)
	require.Equal(t, http.StatusOK, code, string(data))
}

func TestM9ExpiredRunRefusesStatusAndArtifacts(t *testing.T) {
	h := newHarness(t)
	run, _, auth := h.m9Claim("expired-capability")
	require.NoError(t, h.DB.Model(&urth.Result{}).Where("uid = ?", run.UID).UpdateColumns(map[string]any{"status_status": urth.JobExpired, "status_deadline": time.Now().Add(-10 * time.Minute)}).Error)
	run = h.result(run.UID)
	code, data := h.m9Status(run, auth.Token, prob.RunFinishedSuccess)
	require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
	body, err := json.Marshal(urth.Artifact{ObjectMeta: manifest.ObjectMeta{Name: "too-late"}, Spec: urth.ArtifactSpec{Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain", Content: []byte("late")}}}.ToManifest())
	require.NoError(t, err)
	code, data = h.httpRequest(http.MethodPost, "/v1/artifacts", auth.Token, body)
	require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
}

func TestM9ConcurrentStatusWritesKeepOneTerminalResult(t *testing.T) {
	h := newHarness(t)
	run, _, auth := h.m9Claim("concurrent-capability")
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	responses := make(chan int, 2)
	for _, result := range []prob.RunStatus{prob.RunFinishedSuccess, prob.RunFinishedError} {
		go func() {
			ready.Done()
			<-start
			code, _ := h.m9Status(run, auth.Token, result)
			responses <- code
		}()
	}
	ready.Wait()
	close(start)
	first, second := <-responses, <-responses
	require.True(t, (first == 200) != (second == 200), "exactly one concurrent completion succeeds: %d, %d", first, second)
	if first != 200 {
		require.Contains(t, []int{401, 403, 409}, first)
	} else {
		require.Contains(t, []int{401, 403, 409}, second)
	}
	stored := h.result(run.UID)
	require.Equal(t, urth.JobCompleted, stored.Status.Status)
	require.Equal(t, run.Version+1, stored.Version)
}

func TestM9ArtifactAdmissionLocksRunUntilInsert(t *testing.T) {
	h := newHarness(t)
	run, _, auth := h.m9Claim("artifact-admission")
	var checked bool
	callback := "m9:artifact-admission"
	require.NoError(t, h.DB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table != "artifacts" {
			return
		}
		checked = true
		// A competing expiry transaction must not obtain the Result lock
		// between artifact admission and insertion. NOWAIT makes this check
		// deterministic without assuming a scheduler or a timeout delay.
		err := h.DB.Transaction(func(other *gorm.DB) error {
			return other.Exec("SELECT uid FROM results WHERE uid = ? FOR UPDATE NOWAIT", run.UID).Error
		})
		require.ErrorContains(t, err, "55P03")
	}))
	t.Cleanup(func() { _ = h.DB.Callback().Create().Remove(callback) })
	body, err := json.Marshal(urth.Artifact{ObjectMeta: manifest.ObjectMeta{Name: "locked-artifact"}, Spec: urth.ArtifactSpec{Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain", Content: []byte("locked")}}}.ToManifest())
	require.NoError(t, err)
	code, data := h.httpRequest(http.MethodPost, "/v1/artifacts", auth.Token, body)
	require.Equal(t, http.StatusCreated, code, string(data))
	require.True(t, checked, "the insert must reach the concurrency assertion")
	require.NoError(t, h.DB.Transaction(func(tx *gorm.DB) error {
		return tx.Exec("SELECT uid FROM results WHERE uid = ? FOR UPDATE NOWAIT", run.UID).Error
	}), "the Result lock must end after the upload transaction")
}

func TestM9ProjectItemAndCatalogueIsolation(t *testing.T) {
	h := newHarness(t)
	scenario := h.applyScenario("secret-scenario", testProbSpec{}, manifest.LabelSelector{})
	other, err := h.Server.Identity.Projects().Create(h.ctx, im.Project{Resource: im.Resource{Name: "other-project", AccountID: im.AccountID(h.scope.Account)}})
	require.NoError(t, err)
	for _, id := range []string{string(scenario.Name), string(scenario.UID)} {
		code, data := h.httpRequest(http.MethodGet, fmt.Sprintf("/v1/projects/%s/scenarios/%s", h.scope.Project, id), h.token, nil)
		if id == string(scenario.Name) {
			require.Equal(t, http.StatusOK, code, string(data))
		} else {
			// Product read paths currently select names; run mutation paths
			// select UIDs. An unsupported UID must not widen project scope.
			require.Equal(t, http.StatusNotFound, code, string(data))
		}
		code, data = h.httpRequest(http.MethodGet, fmt.Sprintf("/v1/projects/%s/scenarios/%s", other.ID, id), h.token, nil)
		require.Equal(t, http.StatusNotFound, code, string(data))
	}
	for _, path := range []string{"scenarios", "search/scenarios/names", "search/scenarios/labels"} {
		code, data := h.httpRequest(http.MethodGet, fmt.Sprintf("/v1/projects/%s/%s", other.ID, path), h.token, nil)
		require.Equal(t, http.StatusOK, code, string(data))
		require.NotContains(t, string(data), "secret-scenario")
	}
}

func TestM9OAuthTrustedProxyBoundary(t *testing.T) {
	h := oauthBoundaryHarness(t, "192.0.2.0/24", "203.0.113.8")
	for i := 0; i < 10; i++ {
		// The rightmost trusted hop cannot make an attacker-controlled
		// leftmost address authoritative beyond the first untrusted hop.
		res := oauthBoundaryRequest(h, "192.0.2.20:4321", fmt.Sprintf("198.51.100.%d, 198.51.100.201, 203.0.113.8", i+1), "")
		require.Equal(t, http.StatusOK, res.Code)
	}
	res := oauthBoundaryRequest(h, "192.0.2.20:4321", "198.51.100.99, 198.51.100.201, 203.0.113.8", "")
	require.Equal(t, http.StatusTooManyRequests, res.Code)
	res = oauthBoundaryRequest(h, "192.0.2.20:4321", "198.51.100.202, 203.0.113.8", "")
	require.Equal(t, http.StatusOK, res.Code, "a separate proxied client must retain its own window")
}

func TestM9InvalidTrustedProxyFailsBeforeComposition(t *testing.T) {
	for _, proxy := range []string{"not-an-address", "192.0.2.0/99"} {
		_, err := apiserver.New(t.Context(), nil, apiserver.Config{TrustedProxies: []string{proxy}})
		require.ErrorContains(t, err, "invalid trusted proxy configuration")
	}
}

func oauthBoundaryRequest(h *harness, address, forwarded, origin string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/oauth/device_authorization", strings.NewReader("client_id=urthctl"))
	req.RemoteAddr = address
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-For", forwarded)
	req.Header.Set("X-Real-IP", forwarded)
	req.Header.Set("Origin", origin)
	res := httptest.NewRecorder()
	h.Server.Router.ServeHTTP(res, req)
	return res
}

func TestM9OAuthUntrustedForwardedAddressCannotResetLimit(t *testing.T) {
	h := oauthBoundaryHarness(t)
	for i := 0; i < 10; i++ {
		res := oauthBoundaryRequest(h, "192.0.2.10:4321", fmt.Sprintf("198.51.100.%d", i+1), "")
		require.Equal(t, http.StatusOK, res.Code, "valid device request %d must succeed", i)
	}
	res := oauthBoundaryRequest(h, "192.0.2.10:4321", "198.51.100.200", "")
	require.Equal(t, http.StatusTooManyRequests, res.Code, "changing untrusted forwarding headers must not reset the mounted OAuth limiter")
	res = oauthBoundaryRequest(h, "192.0.2.11:4321", "198.51.100.200", "")
	require.Equal(t, http.StatusOK, res.Code, "a different direct peer has a separate limit")
}

func TestM9OAuthOriginAndLimiterFailure(t *testing.T) {
	h := oauthBoundaryHarness(t)
	for _, origin := range []string{"https://attacker.example", "http://localhost:8080.attacker.example", "null"} {
		res := oauthBoundaryRequest(h, "192.0.2.12:4321", "", origin)
		require.Equal(t, http.StatusForbidden, res.Code, "origin %q", origin)
	}
	res := oauthBoundaryRequest(h, "192.0.2.12:4321", "", "http://localhost:8080")
	require.Equal(t, http.StatusOK, res.Code, "the configured issuer origin must succeed")
	require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	require.NoError(t, h.DB.Exec("DROP TABLE request_windows").Error)
	res = oauthBoundaryRequest(h, "192.0.2.12:4321", "", "http://localhost:8080")
	require.Equal(t, http.StatusInternalServerError, res.Code, "unavailable rate-limit storage must not issue a device grant")
	var grants int64
	require.NoError(t, h.DB.Table("oauth_grants").Count(&grants).Error)
	require.EqualValues(t, 1, grants, "only the positive control may create a device grant")
}
