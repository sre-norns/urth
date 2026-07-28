package urth_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/probers/rest"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// These tests need the whole resource schema, so they run against a real
// Postgres for the reason described in service_outbox_test.go.

// probeA and probeB are two visibly different scenarios, so a test can say which
// one a worker was handed rather than only that it was handed something.
const (
	probeA = "GET https://probe-a.example.com/health\n"
	probeB = "GET https://probe-b.example.com/health\n"
)

func restProb(script string) prob.Manifest {
	return prob.Manifest{
		Kind: rest.Kind,
		Spec: &rest.Spec{Script: script},
	}
}

// seedScenarioWithProb creates an active runner and an active scenario running
// the given probe, and returns the scenario.
func seedScenarioWithProb(t *testing.T, store *dbstore.DBStore, probManifest prob.Manifest) urth.Scenario {
	t.Helper()

	ctx := context.Background()

	runner := urth.Runner{
		ObjectMeta: manifest.ObjectMeta{Name: "test-runner"},
		Spec:       urth.RunnerSpec{IsActive: true},
	}
	require.NoError(t, store.Create(ctx, &runner))

	scenario := urth.Scenario{
		ObjectMeta: manifest.ObjectMeta{Name: "test-scenario"},
		Spec: urth.ScenarioSpec{
			IsActive: true,
			Prob:     probManifest,
		},
	}
	require.NoError(t, store.Create(ctx, &scenario))

	return scenario
}

// claimedScript reads the script a worker was actually handed.
func claimedScript(t *testing.T, claim urth.AuthJobResponse) string {
	t.Helper()

	spec, ok := claim.Prob.Spec.(*rest.Spec)
	require.True(t, ok, "the claim should carry a typed rest prob, got %T", claim.Prob.Spec)

	return spec.Script
}

// Editing a Scenario must not change what an already-scheduled run executes.
//
// This is the whole point of the snapshot. Claim authorization used to reload the
// Scenario by UID, so a run created on Monday and claimed on Tuesday executed
// Tuesday's script -- without a change of Result version, and with a history that
// still claimed it ran the version it was created from.
func TestScenarioEditDoesNotChangeAScheduledRun(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{}, urth.WithSigningKeys(testKeys(t)))
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)
	require.Equal(t, scenario.Version, created.Spec.Execution.ScenarioVersion)

	// The scenario is edited while the run waits in the queue.
	scenario.Spec.Prob = restProb(probeB)
	_, err = store.CreateOrUpdate(ctx, &scenario)
	require.NoError(t, err)

	claim := claimRun(t, srv, created)
	require.Equal(t, probeA, claimedScript(t, claim),
		"the run must execute the scenario revision it was created from")

	// And the run's own audit trail must agree with what it ran, or the label
	// query that answers "which runs used the bad script" answers it wrongly.
	stored := loadResult(t, store, created.UID)
	require.Equal(t, scenario.Version-1, stored.Spec.Execution.ScenarioVersion)
	require.Equal(t, stored.Spec.Execution.ScenarioVersion.String(),
		stored.Labels[urth.LabelScenarioVersion])
}

// A run already committed to happen survives its scenario being disabled: it was
// scheduled while the scenario was active, and the decision to stop scheduling is
// about future runs. Nothing new is scheduled from it, though.
func TestDisabledScenarioStillClaimsAlreadyScheduledRun(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{}, urth.WithSigningKeys(testKeys(t)))
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	scenario.Spec.IsActive = false
	_, err = store.CreateOrUpdate(ctx, &scenario)
	require.NoError(t, err)

	claim := claimRun(t, srv, created)
	require.Equal(t, probeA, claimedScript(t, claim))

	_, err = srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.Error(t, err, "a disabled scenario schedules nothing new")
}

// The stronger version: the scenario is gone entirely and the run still claims.
//
// Deleting a scenario used to destroy every scheduled run of it -- authorization
// reloaded the scenario and answered "obsolete" when it was missing -- so a run
// that had been dispatched simply died in the queue.
func TestDeletedScenarioStillClaimsAlreadyScheduledRun(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{}, urth.WithSigningKeys(testKeys(t)))
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	require.NoError(t, db.Delete(&urth.Scenario{}, "uid = ?", string(scenario.UID)).Error)

	claim := claimRun(t, srv, created)
	require.Equal(t, probeA, claimedScript(t, claim))
	require.Equal(t, scenario.Name, claim.Scenario,
		"the run keeps naming the scenario it came from")
}

// A probe definition is a script, and a script may carry credentials. Listing
// runs is an ordinary, widely-permitted read; it must not hand out the scripts
// those runs execute.
func TestResultSerializationHidesTheExecutionSnapshot(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{}, urth.WithSigningKeys(testKeys(t)))
	secret := "GET https://example.com/api\nAuthorization: Bearer hunter2\n"
	scenario := seedScenarioWithProb(t, store, restProb(secret))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	fetched, found, err := srv.Results(scenario.Name).Get(ctx, created.Name)
	require.NoError(t, err)
	require.True(t, found)
	require.False(t, fetched.Spec.Execution.IsZero(), "the snapshot is stored, just not published")

	// Both encodings the API and urthctl produce.
	asJSON, err := json.Marshal(fetched.ToManifest())
	require.NoError(t, err)
	asYAML, err := yaml.Marshal(fetched.ToManifest())
	require.NoError(t, err)

	for name, encoded := range map[string]string{"json": string(asJSON), "yaml": string(asYAML)} {
		require.NotContains(t, encoded, "hunter2", "%s serialization leaked the probe script", name)
		require.NotContains(t, encoded, "execution", "%s serialization exposed the snapshot", name)
	}

	// The kind is not secret and stays visible: it is what an operator reads in a
	// run listing.
	require.Contains(t, string(asJSON), string(rest.Kind))
}

// A Result written before snapshots existed cannot be executed, and must not be
// repaired from the scenario as it stands now -- that would run something nobody
// asked for and record it as history.
func TestLegacyResultWithoutSnapshotFailsClosed(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{}, urth.WithSigningKeys(testKeys(t)))
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	// What migrating an existing database looks like: the row predates the
	// column, so the column is NULL.
	require.NoError(t, db.Model(&urth.Result{}).
		Where("uid = ?", string(created.UID)).
		UpdateColumn("execution", nil).Error)

	stale := loadResult(t, store, created.UID)
	require.True(t, stale.Spec.Execution.IsZero())

	_, err = claimRunErr(t, srv, stale)
	require.Error(t, err, "a run with nothing to execute must not be claimable")

	disposition, explicit := urth.ClaimDispositionOf(err)
	require.True(t, explicit)
	require.Equal(t, urth.ClaimObsolete, disposition,
		"redelivery cannot give the run a snapshot, so the dispatch is finished with")

	// Fail *closed*, not silently: the run is terminal and says why, so it does
	// not sit pending forever and an operator can select every run in this state.
	refused := loadResult(t, store, created.UID)
	require.Equal(t, urth.JobErrored, refused.Status.Status)
	require.Equal(t, prob.RunFinishedError, refused.Status.Result)
	require.True(t, refused.Status.Executor.WorkerID == "",
		"no worker executed it, so none is recorded")
	require.Equal(t, urth.ReasonNoExecutionSnapshot, refused.Labels[urth.LabelResultUnschedulable])
}

// The relay must not hand a snapshot-less Result to a transport that carries the
// job: there is nothing to carry, and no retry will produce one.
func TestLegacyResultWithoutSnapshotIsNotDispatched(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	require.NoError(t, db.Model(&urth.Result{}).
		Where("uid = ?", string(created.UID)).
		UpdateColumn("execution", nil).Error)

	entry := loadDispatch(t, db, created.UID)

	scheduler := &stubScheduler{}
	publisher := urth.NewSchedulerDispatchPublisher(scheduler, urth.NewStoreResultLoader(store))

	_, err = publisher.PublishDispatch(ctx, entry)
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)
	require.ErrorIs(t, err, urth.ErrNoExecutionSnapshot)
	require.Empty(t, scheduler.scheduled)
}

// The legacy transport puts the whole prob in its queue message, so it is the
// other place a scenario edit could change a queued run. It publishes what the
// run was created with, not what the scenario has become.
func TestLegacyDispatchPublishesTheSnapshotNotTheScenario(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	scenario.Spec.Prob = restProb(probeB)
	_, err = store.CreateOrUpdate(ctx, &scenario)
	require.NoError(t, err)

	scheduler := &stubScheduler{}
	publisher := urth.NewSchedulerDispatchPublisher(scheduler, urth.NewStoreResultLoader(store))

	_, err = publisher.PublishDispatch(ctx, loadDispatch(t, db, created.UID))
	require.NoError(t, err)
	require.Len(t, scheduler.scheduled, 1)

	published := scheduler.scheduled[0]
	require.Equal(t, probeA, published.Spec.Execution.Prob.Spec.(*rest.Spec).Script)
	require.Equal(t, scenario.Name, published.Spec.Execution.ScenarioName)
}

// claimRunErr is claimRun without the assertion that the claim succeeds, for the
// tests whose subject is the refusal.
func claimRunErr(t *testing.T, srv urth.Service, result urth.Result) (urth.AuthJobResponse, error) {
	t.Helper()

	ctx := context.Background()

	enrolment, found, err := srv.Runners().GetToken(ctx, "test-runner")
	require.NoError(t, err)
	require.True(t, found)

	registration, err := srv.Runners().AuthWorker(ctx, enrolment, manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance},
		Metadata: manifest.ObjectMeta{Name: "test-worker"},
		Spec:     &urth.WorkerInstanceSpec{},
	})
	require.NoError(t, err)

	return srv.Results("").ClaimRun(ctx, result.UID, registration.Session, urth.ClaimJobRequest{
		DispatchID:    urth.DispatchEventUID(result.UID, result.Version),
		ResultVersion: result.Version,
	})
}
