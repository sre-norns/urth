package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

// Change current PostgreSQL authority, not the signed claims. Both mounted
// reporting scopes must reject a capability issued for the original executor.
func TestM9RunCapabilityRejectsChangedStoredExecutor(t *testing.T) {
	for _, binding := range []string{"runner", "worker"} {
		for _, scenarioPath := range []string{"name", "uid"} {
			t.Run(binding+"/scenario-"+scenarioPath, func(t *testing.T) {
				h := newHarness(t, securedWorkerHarness(t, time.Minute))
				run, registration, capability := h.m9Claim("executor-binding")
				require.Equal(t, urth.JobRunning, run.Status.Status)
				require.Equal(t, registration.Worker.Metadata.UID, run.Status.Executor.WorkerID)
				require.NotEmpty(t, run.Status.Executor.RunnerID)
				require.NotEmpty(t, run.Status.DispatchID)

				// A valid replacement resource prevents an unknown-resource error
				// from standing in for an executor-binding authorization denial.
				column := "status_executor_runner_id"
				originalID := run.Status.Executor.RunnerID
				var replacementID manifest.ResourceID
				if binding == "runner" {
					replacementID = h.applyRunner("replacement-runner", nil).UID
				} else {
					column, originalID = "status_executor_worker_id", run.Status.Executor.WorkerID
					token := h.enrolmentToken(run.Status.Executor.RunnerName)
					entry := urth.WorkerInstance{ObjectMeta: manifest.ObjectMeta{Name: "replacement-worker"}, Spec: urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}.ToManifest()
					entry = h.signedWorker(token, entry, nil)
					replacement, err := h.client("").Runners().AuthWorker(h.ctx, token, entry)
					require.NoError(t, err)
					replacementID = replacement.Worker.Metadata.UID
				}
				require.NotEmpty(t, replacementID)
				require.NotEqual(t, originalID, replacementID)

				artifactBody := func(name manifest.ResourceName) []byte {
					t.Helper()
					artifact := urth.Artifact{ObjectMeta: manifest.ObjectMeta{Name: name}, Spec: urth.ArtifactSpec{Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain", Content: []byte("executor binding control")}}}
					body, err := json.Marshal(artifact.ToManifest())
					require.NoError(t, err)
					return body
				}
				artifactCount := func() int64 {
					t.Helper()
					var count int64
					require.NoError(t, h.DB.Model(&urth.Artifact{}).Where("result_id = ?", run.UID).Count(&count).Error)
					return count
				}
				upload := func(name manifest.ResourceName) {
					t.Helper()
					code, data := h.httpRequest(http.MethodPost, "/v1/artifacts", capability.Token, artifactBody(name))
					require.Equal(t, http.StatusCreated, code, string(data))
					var created manifest.ResourceManifest
					require.NoError(t, json.Unmarshal(data, &created))
					require.Equal(t, run.Account, created.Metadata.Account)
					require.Equal(t, run.Project, created.Metadata.Project)
					require.Equal(t, string(run.UID), created.Metadata.Labels[urth.LabelResultUID])
					require.Equal(t, string(run.Status.Executor.RunnerID), created.Metadata.Labels[urth.LabelRunnerUID])
					require.Equal(t, string(run.Status.Executor.WorkerID), created.Metadata.Labels[urth.LabelWorkerUID])
				}
				scenario := string(run.Spec.Execution.ScenarioName)
				if scenarioPath == "uid" {
					scenario = string(run.Spec.Execution.ScenarioUID)
				}
				statusPath := fmt.Sprintf("/v1/scenarios/%s/results/%s/status?version=%d", scenario, run.UID, run.Version)
				statusBody, err := json.Marshal(urth.ResultStatus{Result: prob.RunFinishedSuccess})
				require.NoError(t, err)
				setBinding := func(id manifest.ResourceID) {
					t.Helper()
					update := h.DB.Model(&urth.Result{}).Where("uid = ?", run.UID).UpdateColumn(column, id)
					require.NoError(t, update.Error)
					require.EqualValues(t, 1, update.RowsAffected)
				}

				upload("before-binding-change")
				require.EqualValues(t, 1, artifactCount())
				setBinding(replacementID)
				changed := h.result(run.UID)
				expectedExecutor := run.Status.Executor
				if binding == "runner" {
					expectedExecutor.RunnerID = replacementID
				} else {
					expectedExecutor.WorkerID = replacementID
				}
				require.Equal(t, expectedExecutor, changed.Status.Executor)
				require.Equal(t, run.Version, changed.Version)
				require.Equal(t, run.Account, changed.Account)
				require.Equal(t, run.Project, changed.Project)
				require.Equal(t, run.Status.DispatchID, changed.Status.DispatchID)
				require.Equal(t, run.Status.Deadline, changed.Status.Deadline)

				code, data := h.httpRequest(http.MethodPut, statusPath, capability.Token, statusBody)
				require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
				statusDenial := code
				code, data = h.httpRequest(http.MethodPost, "/v1/artifacts", capability.Token, artifactBody("changed-binding-denied"))
				require.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, code, string(data))
				artifactDenial := code
				denied := h.result(run.UID)
				require.Equal(t, expectedExecutor, denied.Status.Executor)
				require.Equal(t, run.Version, denied.Version)
				require.Equal(t, urth.JobRunning, denied.Status.Status)
				require.Equal(t, run.Status.Result, denied.Status.Result)
				require.Equal(t, run.Spec.TimeEnded, denied.Spec.TimeEnded)
				require.EqualValues(t, 1, artifactCount(), "denied upload must not insert an artifact")

				// Restore only the stored field. The original issued token and
				// exact request path must succeed without re-claim or re-signing.
				setBinding(originalID)
				require.Equal(t, run.Status.Executor, h.result(run.UID).Status.Executor)
				upload("after-binding-restored")
				require.EqualValues(t, 2, artifactCount())
				code, data = h.httpRequest(http.MethodPut, statusPath, capability.Token, statusBody)
				require.Equal(t, http.StatusOK, code, string(data))
				completed := h.result(run.UID)
				require.Equal(t, urth.JobCompleted, completed.Status.Status)
				require.Equal(t, prob.RunFinishedSuccess, completed.Status.Result)
				require.Equal(t, run.Version+1, completed.Version)
				require.Equal(t, run.Status.Executor, completed.Status.Executor)
				t.Logf("stored %s binding change: status %d and artifact %d; original capability succeeds after binding restoration", binding, statusDenial, artifactDenial)
			})
		}
	}
}
