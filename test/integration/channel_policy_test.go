package integration

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/sre-norns/urth/pkg/apiserver"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/worker"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

// Claim evaluates the stored placement selector against the current Runner.
func TestChannelPolicyRelabelledRunnerCannotClaim(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("policy-placement-runner", manifest.Labels{"segment": "private"})
	scenario := h.applyScenario("policy-placement-scenario", testProbSpec{}, manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "private"}})
	token := h.enrolmentToken(runner.Name)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "policy-placement-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
	require.NoError(t, err)
	run := h.createRun(scenario.Name)
	runner.Labels = manifest.Labels{"segment": "public"}
	_, err = h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	_, err = h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.Error(t, err, "a queued private-network job must not execute after the Runner moves")
	require.Equal(t, urth.JobErrored, h.result(run.UID).Status.Status)
}

func TestChannelPolicyJobAdmissionMatchesPreviewWithoutDispatch(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("job-denial", manifest.Labels{"segment": "private"})
	runner.Spec.JobRequirements.ProbeKinds = []string{"http"}
	_, err := h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	scenario := h.applyScenario("job-denial-scenario", testProbSpec{}, manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "private"}})
	preview, found, err := h.client("").Scenarios().Placement(h.ctx, scenario.Name)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, preview.Schedulable)
	run := h.createRun(scenario.Name)
	require.Equal(t, urth.JobErrored, run.Status.Status)
	require.Empty(t, h.outbox(run.UID), "a rejected concrete job has no dispatch authority")
	require.Equal(t, preview.Reason, run.Labels[urth.LabelResultUnschedulable])
}

func TestChannelPolicyProofDoesNotBypassCapabilityCoverage(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("enrollment-denial", nil)
	token := h.enrolmentToken(runner.Name)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	caps := testPolicyCapabilities()
	delete(caps.ProbeVersions, string(testProbKind))
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "incapable-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: caps}}
	_, err = h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
	require.Error(t, err, "valid proof does not supply a missing probe capability")
	require.NotContains(t, err.Error(), string(testProbKind), "Worker response hides the operator rejection reason")
	current, found, err := h.client("").Runners().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	operator, err := urth.NewRunner(current)
	require.NoError(t, err)
	require.NotNil(t, operator.Status.LastAdmissionRejection)
	require.Contains(t, operator.Status.LastAdmissionRejection.Reason, string(testProbKind))
	require.NotEmpty(t, operator.Status.LastAdmissionRejection.Fingerprint)
	require.False(t, operator.Status.LastAdmissionRejection.Time.IsZero())
	require.Equal(t, runner.Version, operator.Version, "operational rejection state does not consume the policy edit version")
	var count int64
	require.NoError(t, h.DB.Model(&urth.WorkerInstance{}).Where("name = ?", "incapable-worker").Count(&count).Error)
	require.Zero(t, count)
	entry.Spec = &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
	require.NoError(t, err)
	admitted, err := urth.NewWorkerInstance(registration.Worker)
	require.NoError(t, err)
	require.Equal(t, testPolicyCapabilities(), admitted.Status.EffectiveCapabilities)
}

func TestChannelPolicyStaleStoredCapabilityRefusesPendingClaim(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("stale-capability", nil)
	scenario := h.applyScenario("stale-capability-scenario", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "stale-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
	require.NoError(t, err)
	admitted, err := urth.NewWorkerInstance(registration.Worker)
	require.NoError(t, err)
	run := h.createRun(scenario.Name)
	admitted.Status.EffectiveCapabilities.ProbeVersions = map[string]string{"http": "1.10.0"}
	encoded, err := json.Marshal(admitted.Status.EffectiveCapabilities)
	require.NoError(t, err)
	require.NoError(t, h.DB.Model(&urth.WorkerInstance{}).Where("uid = ?", admitted.UID).Update("status_effective_capabilities", string(encoded)).Error)
	body, err := json.Marshal(map[string]any{"dispatchId": urth.DispatchEventUID(run.UID, run.Version), "resultVersion": run.Version, "capabilities": testPolicyCapabilities(), "labels": map[string]string{"probe": "urthtest"}})
	require.NoError(t, err)
	code, _ := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", run.UID), registration.Session, body)
	require.Equal(t, 503, code, "forged claim capabilities cannot replace stored authority")
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status, "another capable Worker can still execute this dispatch")
}

func TestChannelPolicyImmutablePropagationOverridesWorkerUpload(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("provenance-runner", nil)
	runner.Spec.PropagatedLabels = manifest.Labels{"vantage": "private"}
	saved, err := h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	runner, err = urth.NewRunner(saved)
	require.NoError(t, err)
	scenario := h.applyScenario("provenance-scenario", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "provenance-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
	require.NoError(t, err)
	run := h.createRun(scenario.Name)
	require.Equal(t, runner.Version, run.Status.Executor.RunnerVersion)
	runner.Spec.PropagatedLabels["vantage"] = "public"
	_, err = h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	claimed, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(t, err)
	stored := h.result(run.UID)
	require.Equal(t, run.Status.Executor.RunnerVersion, stored.Status.Executor.RunnerVersion)
	require.Equal(t, "private", stored.Status.Executor.PropagatedLabels["vantage"])
	uploaded, err := h.client("").Artifacts().Create(h.ctx, claimed.Token, urth.Artifact{ObjectMeta: manifest.ObjectMeta{Name: "forged-provenance", Labels: manifest.Labels{"vantage": "forged", urth.LabelRunnerUID: "forged"}}, Spec: urth.ArtifactSpec{Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain", Content: []byte("evidence")}}}.ToManifest())
	require.NoError(t, err)
	require.Equal(t, "private", uploaded.Metadata.Labels["vantage"])
	require.Equal(t, string(runner.UID), uploaded.Metadata.Labels[urth.LabelRunnerUID])
}

func TestChannelPolicyPlacementRefusalAcknowledgesDispatch(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("relabel-ack", manifest.Labels{"segment": "private"})
	scenario := h.applyScenario("relabel-ack-scenario", testProbSpec{Message: "must-not-execute-relabelled"}, manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "private"}})
	run := h.createRun(scenario.Name)
	runner.Labels = manifest.Labels{"segment": "public"}
	_, err := h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	h.startWorker(runner.Name)
	h.mustRelay(1)
	finished := h.awaitTerminal(run.UID, 30*time.Second)
	require.Equal(t, urth.JobErrored, finished.Status.Status)
	require.Equal(t, "runner-placement-changed", finished.Labels[urth.LabelResultUnschedulable])
	require.Eventually(t, func() bool {
		info := h.consumerInfo(runner.UID)
		return info.NumPending == 0 && info.NumAckPending == 0
	}, 10*time.Second, 25*time.Millisecond)
	require.Zero(t, h.consumerInfo(runner.UID).NumRedelivered)
	require.Zero(t, probeRunCount("must-not-execute-relabelled"))
}

func TestChannelPolicyPlacementSnapshotControlsClaims(t *testing.T) {
	for _, tc := range []struct {
		name      string
		selector  manifest.LabelSelector
		malformed bool
	}{
		{name: "empty"},
		{name: "unchanged", selector: manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "private"}}},
		{name: "malformed", malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			runner := h.applyRunner("snapshot-runner", manifest.Labels{"segment": "private"})
			scenario := h.applyScenario("snapshot-scenario", testProbSpec{}, tc.selector)
			token := h.enrolmentToken(runner.Name)
			entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "snapshot-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
			registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
			require.NoError(t, err)
			run := h.createRun(scenario.Name)
			if tc.malformed {
				run.Spec.Execution.Requirements = manifest.LabelSelector{MatchLabels: manifest.Labels{"bad key": "x"}}
				encoded, err := json.Marshal(run.Spec.Execution)
				require.NoError(t, err)
				require.NoError(t, h.DB.Model(&urth.Result{}).Where("uid = ?", run.UID).Update("execution", string(encoded)).Error)
			}
			body, err := json.Marshal(urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
			require.NoError(t, err)
			code, response := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", run.UID), registration.Session, body)
			if tc.malformed {
				require.Equal(t, 409, code)
				require.Equal(t, urth.JobErrored, h.result(run.UID).Status.Status)
				require.Equal(t, urth.ReasonInvalidRequirements, h.result(run.UID).Labels[urth.LabelResultUnschedulable])
				require.NotContains(t, string(response), "bad key")
				require.NotContains(t, string(response), "invalid-requirements")
			} else {
				require.Equal(t, 200, code)
				require.Equal(t, urth.JobRunning, h.result(run.UID).Status.Status)
			}
		})
	}
}

func TestChannelPolicyTypedVersionAndDurationEnrollment(t *testing.T) {
	for _, tc := range []struct {
		name, version, max string
		valid              bool
	}{
		{"semantic-order-denial", "1.9.0", "1m", false},
		{"semantic-order-positive", "1.10.0", "1m", true},
		{"prerelease-denial", "1.10.0-rc.1", "1m", false},
		{"unknown-denial", "devel", "1m", false},
		{"duration-denial", "1.10.0", "59s", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			runner := h.applyRunner("typed-enrollment", nil)
			runner.Spec.WorkerRequirements.Version = ">=1.10.0"
			_, err := h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
			require.NoError(t, err)
			token := h.enrolmentToken(runner.Name)
			caps := testPolicyCapabilities()
			caps.Version = tc.version
			caps.MaxDuration = tc.max
			entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "typed-worker", Labels: manifest.Labels{"version": "1.99.0"}}, Spec: &urth.WorkerInstanceSpec{Capabilities: caps}}
			_, err = h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestChannelPolicyPolicyChangeDoesNotRevokeIdempotentClaim(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("claimed-policy", nil)
	scenario := h.applyScenario("claimed-policy-scenario", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "claimed-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
	require.NoError(t, err)
	run := h.createRun(scenario.Name)
	request := urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version}
	initial, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, request)
	require.NoError(t, err)
	runner.Spec.JobRequirements.ProbeKinds = []string{"http"}
	_, err = h.client("").Runners().Update(h.ctx, runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	retry, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, request)
	require.NoError(t, err)
	require.Equal(t, initial.VersionedResourceID, retry.VersionedResourceID)
	require.WithinDuration(t, initial.Deadline, retry.Deadline, time.Microsecond)
	require.Equal(t, urth.JobRunning, h.result(run.UID).Status.Status)
}

func TestChannelPolicyIncapableWorkerLeavesDispatchForCapableWorker(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("capability-retry", nil)
	scenario := h.applyScenario("capability-retry-scenario", testProbSpec{Message: "capable-retry-once"}, manifest.LabelSelector{})
	refused := make(chan struct{})
	var once sync.Once
	stale := h.startWorker(runner.Name, withWorkerConfig(func(cfg *worker.Config) { cfg.Name = "stale-loop-worker" }), interceptClaims(func(ctx context.Context, next func() (urth.AuthJobResponse, error)) (urth.AuthJobResponse, error) {
		once.Do(func() {
			caps := testPolicyCapabilities()
			delete(caps.ProbeVersions, string(testProbKind))
			encoded, err := json.Marshal(caps)
			require.NoError(t, err)
			require.NoError(t, h.DB.Model(&urth.WorkerInstance{}).Where("name = ?", "stale-loop-worker").Update("status_effective_capabilities", string(encoded)).Error)
		})
		response, err := next()
		if err != nil {
			select {
			case <-refused:
			default:
				close(refused)
			}
		}
		return response, err
	}))
	run := h.createRun(scenario.Name)
	h.mustRelay(1)
	select {
	case <-refused:
	case <-time.After(20 * time.Second):
		t.Fatal("incapable Worker did not reach refusal")
	}
	stale.stop(t)
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)
	h.startWorker(runner.Name)
	finished := h.awaitTerminal(run.UID, 30*time.Second)
	require.Equal(t, urth.JobCompleted, finished.Status.Status)
	require.EqualValues(t, 1, probeRunCount("capable-retry-once"))
	require.Empty(t, h.dispatchFailures())
}

func TestChannelPolicyCapabilityRefreshPreservesConcurrentPause(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("refresh-pause", nil)
	token := h.enrolmentToken(runner.Name)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "refresh-pause-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
	_, err = h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, found, err := h.Server.Service.Workers().SetPaused(h.ctx, "refresh-pause-worker", false)
		require.NoError(t, err)
		require.True(t, found)
		caps := testPolicyCapabilities()
		caps.Version = fmt.Sprintf("1.10.%d", i+1)
		entry.Spec = &urth.WorkerInstanceSpec{Capabilities: caps}
		signed := h.signedWorker(token, entry, key)
		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() { <-start; _, err := h.client("").Runners().AuthWorker(h.ctx, token, signed); errs <- err }()
		go func() {
			<-start
			_, _, err := h.Server.Service.Workers().SetPaused(h.ctx, "refresh-pause-worker", true)
			errs <- err
		}()
		close(start)
		require.NoError(t, <-errs)
		require.NoError(t, <-errs)
		var stored urth.WorkerInstance
		require.NoError(t, h.DB.Where("name = ?", "refresh-pause-worker").First(&stored).Error)
		require.True(t, stored.Status.IsPaused, "registration preserves concurrent operator pause")
		require.Equal(t, caps.Version, stored.Status.EffectiveCapabilities.Version, "pause preserves refreshed effective capabilities")
	}
}

func TestChannelPolicyDurationFilterRetainsEligibleRunner(t *testing.T) {
	h := newHarness(t)
	bad := h.applyRunner("duration-too-short", manifest.Labels{"segment": "private"})
	bad.Spec.JobRequirements.MaxDuration = "30s"
	_, err := h.client("").Runners().Update(h.ctx, bad.GetVersionedID(), bad.ToManifest())
	require.NoError(t, err)
	good := h.applyRunner("duration-covers-job", manifest.Labels{"segment": "private"})
	scenario := h.applyScenario("duration-filter", testProbSpec{}, manifest.LabelSelector{MatchLabels: manifest.Labels{"segment": "private"}})
	preview, found, err := h.client("").Scenarios().Placement(h.ctx, scenario.Name)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, preview.Schedulable)
	require.Equal(t, 2, preview.MatchingRunners)
	require.Equal(t, 1, preview.EligibleRunners)
	run := h.createRun(scenario.Name)
	require.Equal(t, urth.JobPending, run.Status.Status)
	require.Equal(t, good.UID, run.Status.Executor.RunnerID)
	require.Len(t, h.outbox(run.UID), 1)
}

func TestChannelPolicyConcreteDurationControlsClaimLease(t *testing.T) {
	for _, tc := range []struct {
		name         string
		request, max time.Duration
		disposition  int
		terminal     bool
	}{
		{name: "insufficient-request", request: 30 * time.Second, max: time.Minute, disposition: 503},
		{name: "oversized-request", request: 5 * time.Minute, max: time.Minute, disposition: 200},
		{name: "server-ceiling", request: time.Minute, max: 30 * time.Second, disposition: 409, terminal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, withConfig(func(c *apiserver.Config) { c.MaxRunDuration = tc.max }))
			runner := h.applyRunner("duration-claim", nil)
			scenario := h.applyScenario("duration-claim-scenario", testProbSpec{}, manifest.LabelSelector{})
			token := h.enrolmentToken(runner.Name)
			entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "duration-worker"}, Spec: &urth.WorkerInstanceSpec{Capabilities: testPolicyCapabilities()}}
			registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
			require.NoError(t, err)
			run := h.createRun(scenario.Name)
			body, err := json.Marshal(urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version, Timeout: tc.request})
			require.NoError(t, err)
			before := time.Now()
			code, response := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", run.UID), registration.Session, body)
			after := time.Now()
			require.Equal(t, tc.disposition, code)
			stored := h.result(run.UID)
			if code == 200 {
				var authority urth.AuthJobResponse
				require.NoError(t, json.Unmarshal(response, &authority))
				require.False(t, authority.Deadline.Before(before.Add(time.Minute)))
				require.False(t, authority.Deadline.After(after.Add(time.Minute)))
				require.Equal(t, urth.JobRunning, stored.Status.Status)
			} else {
				require.True(t, stored.Status.Deadline.IsZero(), "refusal spends no execution lease")
				if tc.terminal {
					require.Equal(t, urth.JobErrored, stored.Status.Status)
				} else {
					require.Equal(t, urth.JobPending, stored.Status.Status)
				}
			}
		})
	}
}

func TestChannelPolicyObsoleteRunnerRequirementsRefusedByHTTP(t *testing.T) {
	h := newHarness(t)
	body, err := json.Marshal(map[string]any{"apiVersion": urth.APIVersion, "kind": urth.KindRunner, "metadata": map[string]any{"name": "obsolete-runner"}, "spec": map[string]any{"active": true, "requirements": map[string]any{}}})
	require.NoError(t, err)
	code, response := h.httpRequest("POST", fmt.Sprintf("/v1/accounts/%s/runners", h.scope.Account), h.token, body)
	require.Equal(t, 400, code)
	require.Contains(t, string(response), "obsolete")
	var count int64
	require.NoError(t, h.DB.Model(&urth.Runner{}).Where("name = ?", "obsolete-runner").Count(&count).Error)
	require.Zero(t, count)
}

func TestChannelPolicyRunnerAdmissionHistoryIsServerOwned(t *testing.T) {
	h := newHarness(t)
	runner := urth.Runner{ObjectMeta: manifest.ObjectMeta{Name: "forged-admission-history"}, Spec: urth.RunnerSpec{IsActive: true, JobRequirements: urth.JobRequirements{ProbeKinds: []string{string(testProbKind)}}}, Status: urth.RunnerStatus{LastAdmissionRejection: &urth.AdmissionRejection{Fingerprint: "forged", Reason: "forged operator history", Time: time.Now().UTC()}}}
	value, err := h.client("").Runners().Create(h.ctx, runner.ToManifest())
	require.NoError(t, err)
	stored, err := urth.NewRunner(value)
	require.NoError(t, err)
	require.Nil(t, stored.Status.LastAdmissionRejection)
	stored.Status = runner.Status
	value, err = h.client("").Runners().Update(h.ctx, stored.GetVersionedID(), stored.ToManifest())
	require.NoError(t, err)
	latest, err := urth.NewRunner(value)
	require.NoError(t, err)
	require.Nil(t, latest.Status.LastAdmissionRejection)
}

// A misspelled probe kind used to be stored, and the coverage rule then refused
// every Worker at enrollment because none could declare the made-up prober.
func TestChannelPolicyUnknownProbeKindRefusedByHTTP(t *testing.T) {
	h := newHarness(t)
	body, err := json.Marshal(map[string]any{"apiVersion": urth.APIVersion, "kind": urth.KindRunner, "metadata": map[string]any{"name": "misspelled-runner"}, "spec": map[string]any{"active": true, "jobRequirements": map[string]any{"probeKinds": []string{"htpp"}}}})
	require.NoError(t, err)
	code, response := h.httpRequest("POST", fmt.Sprintf("/v1/accounts/%s/runners", h.scope.Account), h.token, body)
	require.Equal(t, 400, code)
	require.Contains(t, string(response), "unknown probe kind")
	var count int64
	require.NoError(t, h.DB.Model(&urth.Runner{}).Where("name = ?", "misspelled-runner").Count(&count).Error)
	require.Zero(t, count)
}
