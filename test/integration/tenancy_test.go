package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestTenancyPlacementRequiresProjectGrant(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("shared-runner", nil)
	scenario := h.applyScenario("granted", testProbSpec{Message: "granted"}, manifest.LabelSelector{})
	p2, err := h.Server.Identity.Projects().Create(h.ctx, im.Project{Resource: im.Resource{Name: "ungranted", AccountID: im.AccountID(h.scope.Account)}})
	require.NoError(t, err)
	ctx := urth.WithScope(h.ctx, manifest.ScopeRef{Account: h.scope.Account, Project: manifest.ResourceID(p2.ID)})
	body := scenario.ToManifest()
	body.Metadata = manifest.ObjectMeta{Name: "ungranted"}
	_, err = h.Server.Service.Scenarios().Create(ctx, body)
	require.NoError(t, err)
	run, err := h.Server.Service.Results("ungranted").Create(ctx, manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{Kind: urth.KindResult}, Spec: &urth.ResultSpec{}})
	require.NoError(t, err)
	require.Equal(t, urth.JobErrored, run.Status.Status)
	require.Equal(t, "no-eligible-runner", run.Labels[urth.LabelResultUnschedulable])
	require.Empty(t, h.outbox(run.UID))
	granted := h.createRun(scenario.Name)
	require.Equal(t, runner.UID, granted.Status.Executor.RunnerID)
	require.Equal(t, h.scope.Project, granted.Project)
}

func TestTenancyGrantRevokedBeforeClaim(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("revoked-runner", nil)
	scenario := h.applyScenario("revoked-grant", testProbSpec{Message: "revoked-grant"}, manifest.LabelSelector{})
	run := h.createRun(scenario.Name)
	grant, ok, err := h.Server.Service.RunnerAuthorizations().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, ok)
	deleted, err := h.Server.Service.RunnerAuthorizations().Delete(h.ctx, grant.Metadata.GetVersionedID())
	require.NoError(t, err)
	require.True(t, deleted)
	h.startWorker(runner.Name)
	h.mustRelay(1)
	finished := h.awaitTerminal(run.UID, 15*time.Second)
	require.Equal(t, urth.JobErrored, finished.Status.Status)
	require.Equal(t, "runner-not-authorized", finished.Labels[urth.LabelResultUnschedulable])
	require.Zero(t, probeRunCount("revoked-grant"))
}

func (h *harness) httpRequest(method, path string, token urth.APIToken, body []byte) (int, []byte) {
	h.t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, method, h.HTTP.URL+path, bytes.NewReader(body))
	require.NoError(h.t, err)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+string(token))
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(h.t, err)
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	require.NoError(h.t, err)
	return res.StatusCode, data
}

func TestTenancyScopeIsolationAndCursorHTTP(t *testing.T) {
	h := newHarness(t)
	h.applyScenario("visible-one", testProbSpec{}, manifest.LabelSelector{})
	h.applyScenario("visible-two", testProbSpec{}, manifest.LabelSelector{})
	base := fmt.Sprintf("/v1/projects/%s/scenarios", h.scope.Project)
	code, _ := h.httpRequest("GET", base, "", nil)
	require.Equal(t, 401, code)
	code, _ = h.httpRequest("GET", base+"?offset=1", h.token, nil)
	require.Equal(t, 400, code)
	code, _ = h.httpRequest("GET", base+"?cursor=malformed", h.token, nil)
	require.Equal(t, 400, code)
	code, data := h.httpRequest("GET", base+"?limit=1", h.token, nil)
	require.Equal(t, 200, code, string(data))
	var first struct {
		Items []manifest.ResourceManifest `json:"items"`
		manifest.Page
	}
	require.NoError(t, json.Unmarshal(data, &first))
	require.Len(t, first.Items, 1)
	require.NotEmpty(t, first.Next)
	rows, page, err := h.client("").Scenarios().List(h.ctx, manifest.SearchQuery{Limit: 1, Cursor: first.Next})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Empty(t, page.Next)
	require.NotEqual(t, first.Items[0].Metadata.UID, rows[0].Metadata.UID)
	values, labelsPage, err := h.client("").Labels(urth.KindScenario).ListNames(h.ctx, manifest.SearchQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, values, 1)
	require.NotEmpty(t, labelsPage.Next)
	// An account admin who is not a project member cannot read project content.
	require.NoError(t, h.DB.Where("project_id = ?", h.scope.Project).Delete(&im.ProjectMembership{}).Error)
	code, data = h.httpRequest("GET", base, h.token, nil)
	require.Contains(t, []int{403, 404}, code, string(data))
	code, data = h.httpRequest("GET", fmt.Sprintf("/v1/projects/%s/search/scenarios/names", h.scope.Project), h.token, nil)
	require.Contains(t, []int{403, 404}, code, string(data))
	code, _ = h.httpRequest("GET", "/v1/auth/runners/visible-one", "", nil)
	require.Equal(t, 404, code)
}

func TestTenancySameRunnerNameAcrossAccounts(t *testing.T) {
	h := newHarness(t)
	first := h.applyRunner("same-name", nil)
	require.NoError(t, h.Server.Identity.ProvisionUser(context.Background(), "other@example.test", "strong-password-for-tests", false))
	var user im.User
	require.NoError(t, h.DB.Where("email = ?", "other@example.test").First(&user).Error)
	var member im.AccountMembership
	require.NoError(t, h.DB.Where("user_id = ?", user.ID).First(&member).Error)
	ctx := identity.WithPrincipal(h.ctx, im.Principal{Type: "user", Scope: im.ScopeAccount, UserID: user.ID, AccountID: member.AccountID})
	ctx = urth.WithScope(ctx, manifest.ScopeRef{Account: manifest.ResourceID(member.AccountID)})
	second, err := h.Server.Service.Runners().Create(ctx, urth.Runner{ObjectMeta: manifest.ObjectMeta{Name: first.Name}, Spec: urth.RunnerSpec{IsActive: true}}.ToManifest())
	require.NoError(t, err)
	require.NotEqual(t, first.Account, second.Metadata.Account)
	require.NotEqual(t, natsq.JobSubject(first.Account, first.Name), natsq.JobSubject(second.Metadata.Account, second.Metadata.Name))
	// A scoped UID mutation cannot target the other account's runner.
	ok, err := h.Server.Service.Runners().Delete(h.ctx, second.Metadata.GetVersionedID())
	require.False(t, ok)
	require.NoError(t, err)
	code, _ := h.httpRequest("GET", fmt.Sprintf("/v1/accounts/%s/runners", second.Metadata.Account), h.token, nil)
	require.Contains(t, []int{403, 404}, code)
	body := first.ToManifest()
	body.Metadata = manifest.ObjectMeta{Name: "injected", Account: second.Metadata.Account}
	_, err = h.Server.Service.Runners().Create(h.ctx, body)
	require.Error(t, err)
}

func TestTenancyRevocationDoesNotInterruptClaimedRun(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("running-runner", nil)
	scenario := h.applyScenario("running-grant", testProbSpec{}, manifest.LabelSelector{})
	run := h.createRun(scenario.Name)
	client := h.client("")
	token, found, err := client.Runners().GetToken(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	worker := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "claiming-worker"}, Spec: &urth.WorkerInstanceSpec{}}
	registration, err := client.Runners().AuthWorker(h.ctx, token, worker)
	require.NoError(t, err)
	request := urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version}
	auth, err := client.Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, request)
	require.NoError(t, err)
	grant, found, err := client.RunnerAuthorizations().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	deleted, err := client.RunnerAuthorizations().Delete(h.ctx, grant.Metadata.GetVersionedID())
	require.NoError(t, err)
	require.True(t, deleted)
	recovered, err := client.Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, request)
	require.NoError(t, err)
	require.Equal(t, auth.VersionedResourceID, recovered.VersionedResourceID)
	_, err = client.Results(scenario.Name).UpdateStatus(h.ctx, auth.VersionedResourceID, auth.Token, urth.ResultStatus{Status: urth.JobCompleted})
	require.NoError(t, err)
	require.Equal(t, urth.JobCompleted, h.result(run.UID).Status.Status)
	// User, enrolment and worker credentials have disjoint authority.
	code, _ := h.httpRequest("GET", fmt.Sprintf("/v1/accounts/%s/runners", h.scope.Account), registration.Session, nil)
	require.Equal(t, 401, code)
	_, err = client.Runners().AuthWorker(h.ctx, h.token, worker)
	require.Error(t, err)
	tokens, _, err := h.Server.Identity.MachineTokens().List(h.ctx, im.AgentIdentityID(runner.UID), manifest.SearchQuery{})
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	revoked := tokens[0]
	revoked.Status = "revoked"
	ctx := identity.WithRequest(h.ctx, identity.Request{IfMatch: identity.ETag(revoked.Revision)})
	_, _, err = h.Server.Identity.MachineTokens().CreateOrUpdate(ctx, revoked)
	require.NoError(t, err)
	_, err = client.Runners().AuthWorker(h.ctx, token, worker)
	require.Error(t, err)
}

func TestTenancyGrantMutationRequiresBothAuthorities(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("authority-runner", nil)
	h.applyScenario("authority-scenario", testProbSpec{}, manifest.LabelSelector{})
	grant, found, err := h.Server.Service.RunnerAuthorizations().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	// Keep project membership but remove account administration in the fixture.
	require.NoError(t, h.DB.Model(&im.AccountMembership{}).Where("account_id = ?", h.scope.Account).Update("role", "member").Error)
	_, err = h.Server.Service.RunnerAuthorizations().Delete(h.ctx, grant.Metadata.GetVersionedID())
	require.Error(t, err)
	// The shared identity grant service must not bypass the product policy.
	var raw im.AgentAuthorization
	require.NoError(t, h.DB.Where("id = ?", grant.Metadata.UID).First(&raw).Error)
	raw.Status = "revoked"
	ctx := identity.WithRequest(h.ctx, identity.Request{IfMatch: identity.ETag(raw.Revision)})
	_, _, err = h.Server.Identity.MachineGrants().CreateOrUpdate(ctx, raw)
	require.Error(t, err)
	// Restore account administration, then remove project membership instead.
	require.NoError(t, h.DB.Model(&im.AccountMembership{}).Where("account_id = ?", h.scope.Account).Update("role", "owner").Error)
	require.NoError(t, h.DB.Where("project_id = ?", h.scope.Project).Delete(&im.ProjectMembership{}).Error)
	_, err = h.Server.Service.RunnerAuthorizations().Delete(h.ctx, grant.Metadata.GetVersionedID())
	require.Error(t, err)
	_, _, err = h.Server.Identity.MachineGrants().CreateOrUpdate(ctx, raw)
	require.Error(t, err)
	require.NoError(t, h.DB.Where("id = ?", grant.Metadata.UID).First(&raw).Error)
	require.Equal(t, "active", raw.Status)
}
