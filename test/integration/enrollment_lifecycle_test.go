package integration

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

// Each state change is exercised after a valid proof has been issued. Rejecting
// only new challenges would leave an already issued proof usable for enrollment.
func TestEnrollmentLifecycleRejectsFreshAndRefresh(t *testing.T) {
	for _, state := range []string{"disabled", "deleted", "identity-suspended"} {
		for _, operation := range []string{"fresh", "refresh"} {
			t.Run(state+"/"+operation, func(t *testing.T) {
				h := newHarness(t)
				runner := h.applyRunner("lifecycle-runner", nil)
				token := h.enrolmentToken(runner.Name)
				_, key, err := ed25519.GenerateKey(rand.Reader)
				require.NoError(t, err)
				entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "lifecycle-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
				registered, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
				require.NoError(t, err, "active Runner permits enrollment")
				renewed, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
				require.NoError(t, err, "active Runner permits refresh")
				require.Equal(t, registered.Worker.Metadata.UID, renewed.Worker.Metadata.UID)
				if operation == "fresh" {
					_, key, err = ed25519.GenerateKey(rand.Reader)
					require.NoError(t, err)
					entry.Metadata.Name = "fresh-worker"
				}
				proof := h.signedWorker(token, entry, key)
				current, found, err := h.client("").Runners().Get(h.ctx, runner.Name)
				require.NoError(t, err)
				require.True(t, found)
				switch state {
				case "disabled":
					updated, err := urth.NewRunner(current)
					require.NoError(t, err)
					updated.Spec.IsActive = false
					_, err = h.client("").Runners().Update(h.ctx, current.Metadata.GetVersionedID(), updated.ToManifest())
					require.NoError(t, err)
				case "deleted":
					deleted, err := h.client("").Runners().Delete(h.ctx, current.Metadata.GetVersionedID())
					require.NoError(t, err)
					require.True(t, deleted)
				case "identity-suspended":
					path := "/v1/agent-identities/" + string(runner.UID)
					code, headers, _ := h.identityRequest("GET", path, nil, nil)
					require.Equal(t, http.StatusOK, code)
					code, _, _ = h.identityRequest("PATCH", path, map[string]string{"If-Match": headers.Get("ETag")}, map[string]string{"operation": "deactivate"})
					require.Equal(t, http.StatusOK, code)
				}
				var before []urth.WorkerInstance
				require.NoError(t, h.DB.Order("uid").Find(&before).Error)
				body, err := json.Marshal(proof)
				require.NoError(t, err)
				for _, path := range []string{"/v1/auth/workers/challenge", "/v1/auth/workers"} {
					code, response := h.httpRequest("POST", path, token, body)
					require.Equal(t, http.StatusUnauthorized, code, "state must deny %s", path)
					require.False(t, strings.Contains(string(response), string(token)), "denial must exclude the enrollment secret")
				}
				var after []urth.WorkerInstance
				require.NoError(t, h.DB.Order("uid").Find(&after).Error)
				require.Equal(t, before, after, "denial must not create or refresh a Worker")
			})
		}
	}
}

// The published identity module stores SHA-256 verifiers for high-entropy
// random tokens. Neither the verifier nor resource/credential metadata grants
// authority at the mounted enrollment routes.
func TestEnrollmentStoredStateAndManifestsExcludeBearerSecret(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("stored-token-runner", nil)
	token := h.enrolmentToken(runner.Name)
	tokens, _, err := h.Server.Identity.MachineTokens().List(h.ctx, im.AgentIdentityID(runner.UID), manifest.SearchQuery{})
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	require.Empty(t, tokens[0].Token)
	var stored struct {
		ID, OwnerID, Kind, Verifier string
		Used                        bool
	}
	require.NoError(t, h.DB.Table("credentials").Where("owner_id = ?", tokens[0].ID).Take(&stored).Error)
	require.Equal(t, "agent", stored.Kind)
	require.False(t, stored.Used)
	digest := sha256.Sum256([]byte(token))
	require.True(t, stored.Verifier == hex.EncodeToString(digest[:]), "stored value must be the shared token verifier")
	var credentialJSON, tokenJSON string
	require.NoError(t, h.DB.Raw("SELECT row_to_json(c)::text FROM credentials c WHERE id = ?", stored.ID).Scan(&credentialJSON).Error)
	require.NoError(t, h.DB.Raw("SELECT row_to_json(t)::text FROM agent_identity_tokens t WHERE id = ?", tokens[0].ID).Scan(&tokenJSON).Error)
	for _, data := range []string{credentialJSON, tokenJSON} {
		require.NotEmpty(t, data)
		require.False(t, strings.Contains(data, string(token)), "stored row must exclude the bearer secret")
	}
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "stored-token-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
	proof := h.signedWorker(token, entry, nil)
	body, err := json.Marshal(proof)
	require.NoError(t, err)
	for label, candidate := range map[string]string{"verifier": stored.Verifier, "credential-id": stored.ID, "token-id": stored.OwnerID, "identity-id": string(runner.UID), "credential-row": credentialJSON, "token-row": tokenJSON} {
		t.Run(label, func(t *testing.T) {
			for _, path := range []string{"/v1/auth/workers/challenge", "/v1/auth/workers"} {
				code, _ := h.httpRequest("POST", path, urth.APIToken(candidate), body)
				require.Equal(t, http.StatusUnauthorized, code)
			}
		})
	}
	registered, err := h.client("").Runners().AuthWorker(h.ctx, token, proof)
	require.NoError(t, err, "the original secret and unused proof must still enroll")
	for _, value := range []any{runner.ToManifest(), registered.Runner, registered.Worker, tokens} {
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.False(t, strings.Contains(string(data), string(token)), "serialized resources must exclude the enrollment secret")
	}
	for _, path := range []string{
		fmt.Sprintf("/v1/accounts/%s/runners/%s", h.scope.Account, runner.Name),
		"/v1/agent-identities/" + string(runner.UID),
		"/v1/agent-identity-tokens/" + tokens[0].ID,
		"/v1/agent-identities/" + string(runner.UID) + "/tokens",
	} {
		code, _, data := h.identityRequest("GET", path, nil, nil)
		require.Equal(t, http.StatusOK, code, "ordinary resource read %s", path)
		require.False(t, strings.Contains(string(data), string(token)), "resource read must exclude the enrollment secret")
	}
}
