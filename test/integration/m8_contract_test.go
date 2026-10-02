package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/sre-norns/urth/pkg/client"
	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

// The name-based CLI helper now goes through identity's resource operation,
// including its replay transaction; a retry cannot issue a second credential.
func TestCanonicalRunnerTokenReplay(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("canonical-token", nil)
	ctx := client.WithRequestOptions(h.ctx, client.RequestOptions{IdempotencyKey: "token-retry"})
	token, found, err := h.client("").Runners().GetToken(ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, token)
	replay, found, err := h.client("").Runners().GetToken(ctx, runner.Name)
	require.True(t, found)
	require.ErrorContains(t, err, "one-time secret")
	require.Empty(t, replay)
	rows, _, err := h.Server.Identity.MachineTokens().List(h.ctx, model.AgentIdentityID(runner.UID), manifest.SearchQuery{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	code, _, data := h.identityRequest("GET", "/v1/agent-identity-tokens/"+rows[0].ID, nil, nil)
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(data), string(token))
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Equal(t, "identity.sre-norns.com/v1", doc["apiVersion"])
	require.Equal(t, "agent-identity-tokens", doc["kind"])
	require.NotContains(t, doc, "token")
	require.NotContains(t, doc, "id")
	code, _, _ = h.identityRequest("POST", fmt.Sprintf("/v1/accounts/%s/runners/%s/tokens", h.scope.Account, runner.Name), nil, nil)
	require.Equal(t, http.StatusNotFound, code, "the plain-text token endpoint is retired")
}

func TestCanonicalProjectWritesAndProblems(t *testing.T) {
	h := newHarness(t)
	path := fmt.Sprintf("/v1/accounts/%s/projects", h.scope.Account)
	headers := map[string]string{"Idempotency-Key": "canonical-project"}
	code, _, data := h.identityRequest("POST", path, headers, map[string]any{"name": "flat"})
	require.Equal(t, http.StatusUnprocessableEntity, code, string(data))
	body := map[string]any{"apiVersion": "identity.sre-norns.com/v1", "kind": "projects", "metadata": map[string]any{"name": "canonical"}, "spec": map[string]any{"description": "canonical create"}}
	code, _, data = h.identityRequest("POST", path, headers, body)
	require.Equal(t, http.StatusConflict, code, "a changed retry needs a new key: %s", data)
	headers["Idempotency-Key"] = "canonical-project-valid"
	code, readHeaders, data := h.identityRequest("POST", path, headers, body)
	require.Equal(t, http.StatusCreated, code, string(data))
	var doc struct {
		Metadata struct {
			UID string `json:"uid"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.NotEmpty(t, doc.Metadata.UID)
	path = "/v1/projects/" + doc.Metadata.UID
	patch := map[string]any{"spec": map[string]any{"description": ""}}
	code, _, data = h.identityRequest("PATCH", path, nil, patch)
	require.Equal(t, http.StatusPreconditionRequired, code, string(data))
	code, _, data = h.identityRequest("PATCH", path, map[string]string{"If-Match": readHeaders.Get("ETag")}, patch)
	require.Equal(t, http.StatusOK, code, string(data))
	code, _, data = h.identityRequest("PATCH", path, map[string]string{"If-Match": readHeaders.Get("ETag")}, patch)
	require.Equal(t, http.StatusPreconditionFailed, code, string(data))
	require.Contains(t, string(data), "requestId")
}
