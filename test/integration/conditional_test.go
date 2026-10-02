package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

// Resource edits are optimistic (ADR 0001 §5): a PUT says which version it was
// based on with If-Match, and an edit of a superseded version changes nothing.

// scenarioBody is a scenario manifest as a client would PUT it.
func (h *harness) scenarioBody(scenario urth.Scenario, active bool) map[string]any {
	h.t.Helper()
	scenario.Spec.IsActive = active
	data, err := json.Marshal(scenario.ToManifest())
	require.NoError(h.t, err)
	var body map[string]any
	require.NoError(h.t, json.Unmarshal(data, &body))
	body["metadata"] = map[string]any{"name": string(scenario.Name)}
	return body
}

func (h *harness) storedScenario(name manifest.ResourceName) urth.Scenario {
	h.t.Helper()
	value, found, err := h.Server.Service.Scenarios().Get(h.ctx, name)
	require.NoError(h.t, err)
	require.True(h.t, found)
	scenario, err := urth.NewScenario(value)
	require.NoError(h.t, err)
	return scenario
}

func TestScenarioUpdateRequiresIfMatch(t *testing.T) {
	h := newHarness(t)
	scenario := h.applyScenario("guarded", testProbSpec{}, manifest.LabelSelector{})
	path := fmt.Sprintf("/v1/projects/%s/scenarios/guarded", h.scope.Project)

	code, headers, _ := h.identityRequest("GET", path, nil, nil)
	require.Equal(t, http.StatusOK, code)
	read := headers.Get("ETag")
	require.Equal(t, `"1"`, read, "a read carries the version as its ETag")

	code, _, _ = h.identityRequest("PUT", path, nil, h.scenarioBody(scenario, false))
	require.Equal(t, http.StatusPreconditionRequired, code, "an update without If-Match is refused")
	code, _, _ = h.identityRequest("PUT", path, map[string]string{"If-Match": "1"}, h.scenarioBody(scenario, false))
	require.Equal(t, http.StatusBadRequest, code, "an unquoted If-Match is malformed")
	require.True(t, h.storedScenario("guarded").Spec.IsActive, "refused updates change nothing")

	code, headers, data := h.identityRequest("PUT", path, map[string]string{"If-Match": read}, h.scenarioBody(scenario, false))
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, `"2"`, headers.Get("ETag"), "an update returns the new version")
	require.False(t, h.storedScenario("guarded").Spec.IsActive, "disabling -- a zero value -- is written")

	code, _, _ = h.identityRequest("PUT", path, map[string]string{"If-Match": read}, h.scenarioBody(scenario, true))
	require.Equal(t, http.StatusPreconditionFailed, code, "an edit of a superseded version is refused")
	require.False(t, h.storedScenario("guarded").Spec.IsActive, "and changes nothing")

	code, _, _ = h.identityRequest("PUT", path, map[string]string{"If-Match": "*"}, h.scenarioBody(scenario, true))
	require.Equal(t, http.StatusOK, code, "`*` is an unconditional update")
	require.True(t, h.storedScenario("guarded").Spec.IsActive)

	missing := scenario
	missing.Name = "not-there"
	missingPath := fmt.Sprintf("/v1/projects/%s/scenarios/not-there", h.scope.Project)
	code, _, _ = h.identityRequest("PUT", missingPath, map[string]string{"If-Match": `"3"`}, h.scenarioBody(missing, true))
	require.Equal(t, http.StatusPreconditionFailed, code, "no stored version matches a missing resource")
	code, headers, _ = h.identityRequest("PUT", missingPath, map[string]string{"If-Match": "*"}, h.scenarioBody(missing, true))
	require.Equal(t, http.StatusCreated, code, "`*` may create")
	require.Equal(t, `"1"`, headers.Get("ETag"))
}

// The version check is on the write, not only the read: of several edits of
// one version racing, exactly one lands. A check on the read alone lets every
// one that read before the first write pass.
func TestConcurrentEditsOfOneVersionOneLands(t *testing.T) {
	h := newHarness(t)
	scenario := h.applyScenario("contended", testProbSpec{}, manifest.LabelSelector{})
	path := fmt.Sprintf("/v1/projects/%s/scenarios/contended", h.scope.Project)

	const writers = 8
	codes := make([]int, writers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range writers {
		wg.Go(func() {
			<-start
			body := h.scenarioBody(scenario, i%2 == 0)
			body["metadata"] = map[string]any{"name": "contended", "labels": map[string]string{"writer": fmt.Sprint(i)}}
			codes[i], _, _ = h.identityRequest("PUT", path, map[string]string{"If-Match": `"1"`}, body)
		})
	}
	close(start)
	wg.Wait()

	landed := 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			landed++
		case http.StatusPreconditionFailed, http.StatusConflict:
		default:
			t.Fatalf("unexpected status %d among %v", code, codes)
		}
	}
	require.Equal(t, 1, landed, "exactly one edit of version 1 may land, got %v", codes)
	require.EqualValues(t, 2, h.storedScenario("contended").Version)
}

func TestRunnerAndGrantUpdatesRefuseAStaleVersion(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("guarded-runner", nil)
	runnerPath := fmt.Sprintf("/v1/accounts/%s/runners/guarded-runner", h.scope.Account)
	body := map[string]any{"apiVersion": "urth.sre-norns.com/v1", "kind": "runners", "metadata": map[string]any{"name": "guarded-runner"}, "spec": map[string]any{"active": false}}

	code, _, _ := h.identityRequest("PUT", runnerPath, nil, body)
	require.Equal(t, http.StatusPreconditionRequired, code)
	code, headers, data := h.identityRequest("PUT", runnerPath, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, runner.Version)}, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotEmpty(t, headers.Get("ETag"))
	code, _, _ = h.identityRequest("PUT", runnerPath, map[string]string{"If-Match": fmt.Sprintf(`"%d"`, runner.Version)}, body)
	require.Equal(t, http.StatusPreconditionFailed, code, "a runner edit of a superseded version is refused")

	grants := fmt.Sprintf("/v1/projects/%s/runner-authorizations", h.scope.Project)
	grantBody := map[string]any{"apiVersion": "urth.sre-norns.com/v1", "kind": "runner-authorizations", "metadata": map[string]any{"name": "guarded-grant"},
		"spec": map[string]any{"runnerRef": string(runner.UID), "roles": []string{"runner"}}}
	code, headers, data = h.identityRequest("POST", grants, nil, grantBody)
	require.Equal(t, http.StatusCreated, code, string(data))
	require.NotEmpty(t, headers.Get("ETag"), "a create returns the version it made")
	var grant manifest.ResourceManifest
	require.NoError(t, json.Unmarshal(data, &grant))
	grantPath := grants + "/guarded-grant"
	code, _, _ = h.identityRequest("PUT", grantPath, nil, grantBody)
	require.Equal(t, http.StatusPreconditionRequired, code)
	stale := fmt.Sprintf(`"%d"`, grant.Metadata.Version+5)
	code, _, _ = h.identityRequest("PUT", grantPath, map[string]string{"If-Match": stale}, grantBody)
	require.Equal(t, http.StatusPreconditionFailed, code, "a grant edit of another version is refused")
	code, _, data = h.identityRequest("PUT", grantPath, map[string]string{"If-Match": headers.Get("ETag")}, grantBody)
	require.Equal(t, http.StatusOK, code, string(data))
}

// urthctl apply goes through the client: unconditional without a version, and
// guarded by the manifest's own version when it carries one.
func TestApplyIsUnconditionalUnlessTheManifestCarriesAVersion(t *testing.T) {
	h := newHarness(t)
	client := h.client("")
	scenario := h.applyScenario("applied", testProbSpec{}, manifest.LabelSelector{})

	body := scenario.ToManifest()
	body.APIVersion = urth.APIVersion // as every manifest file says; the client requires it
	body.Metadata = manifest.ObjectMeta{Name: "applied"}
	for range 2 {
		_, _, err := client.Scenarios().CreateOrUpdate(h.ctx, body)
		require.NoError(t, err, "apply without a version may be repeated: %v", err)
	}

	stale := scenario.ToManifest()
	stale.APIVersion = urth.APIVersion
	stale.Metadata = manifest.ObjectMeta{Name: "applied", Version: 1}
	_, _, err := client.Scenarios().CreateOrUpdate(h.ctx, stale)
	require.Error(t, err, "applying a stale copy that carries its version is refused")

	current := h.storedScenario("applied").ToManifest()
	current.APIVersion = urth.APIVersion
	_, _, err = client.Scenarios().CreateOrUpdate(h.ctx, current)
	require.NoError(t, err, "applying the current copy succeeds")
}
