package urth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Placement is the first thing that can refuse a run, and until this suite
// existed it was also the quietest: a run nothing could take was created
// `pending`, given an outbox row no transport would accept, and left there.
//
// These tests need the whole resource schema, so they run on Postgres for the
// reason given in service_outbox_test.go.

// seedPlacement creates one runner and one scenario, each configured by the
// caller, and returns the scenario's name.
//
// Written directly through the store rather than through the service, because
// several of these cases are ones the service is now expected to refuse to
// create -- a scenario with a selector that does not parse is a record that
// predates validation, not one an operator can still make.
func seedPlacement(t *testing.T, store *dbstore.DBStore, runner urth.Runner, requirements manifest.LabelSelector) manifest.ResourceName {
	t.Helper()

	ctx := context.Background()

	require.NoError(t, store.Create(ctx, &runner))

	scenario := urth.Scenario{
		ObjectMeta: manifest.ObjectMeta{Name: "test-scenario"},
		Spec: urth.ScenarioSpec{
			IsActive:     true,
			Requirements: requirements,
			Prob: prob.Manifest{
				Kind: "http",
				Spec: map[string]any{"target": "http://example.com"},
			},
		},
	}
	require.NoError(t, store.Create(ctx, &scenario))

	return scenario.Name
}

// labelledRunner is a runner carrying labels a scenario can select on.
func labelledRunner(name manifest.ResourceName, active bool, labels manifest.Labels) urth.Runner {
	return urth.Runner{
		ObjectMeta: manifest.ObjectMeta{Name: name, Labels: labels},
		Spec:       urth.RunnerSpec{IsActive: active},
	}
}

// requireUnplaceable asserts the shape every unplaceable run must have.
func requireUnplaceable(t *testing.T, db *gorm.DB, result urth.Result, reason string) {
	t.Helper()

	require.Equal(t, urth.JobErrored, result.Status.Status,
		"a run nothing can take must not be left pending")
	require.Equal(t, prob.RunFinishedError, result.Status.Result)
	require.NotNil(t, result.Spec.TimeEnded, "a terminal run has an end time")
	require.Equal(t, reason, result.Labels[urth.LabelResultUnschedulable])
	require.Equal(t, string(urth.JobErrored), result.Labels[urth.LabelResultJobState])
	require.Empty(t, result.Status.Executor.RunnerID)

	require.Zero(t, countOutbox(t, db),
		"an unplaceable run must not leave an entry the relay will retry forever")
}

// A run of a scenario no active runner matches is terminal on creation.
//
// It used to be created `pending` with an outbox row the routing transport
// refused, which the relay then retried hourly for as long as the row existed:
// the run never moved, and the only report of why was a log line per attempt.
// Nothing can repair it either -- the reconciler leaves an unpublished entry to
// the relay by design -- so the decision has to be made here, where the reason
// is known.
func TestRunIsTerminalWhenNoRunnerMatches(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})

	scenarioName := seedPlacement(t, store,
		labelledRunner("other-runner", true, manifest.Labels{"os": "windows"}),
		manifest.LabelSelector{MatchLabels: manifest.Labels{"os": "linux"}},
	)

	result, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err, "the run is recorded; it is the run that failed, not the request")

	requireUnplaceable(t, db, result, urth.ReasonNoEligibleRunner)

	// And it is that way in the store, not only in the returned value.
	var stored urth.Result
	found, err := store.GetByUID(context.Background(), &stored, result.UID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, urth.JobErrored, stored.Status.Status)
}

// A matching runner that is disabled is not a placement.
//
// Dispatching to it would queue work that the claim rules then refuse to hand to
// any of its workers, which is the same dead end reached by a longer route.
func TestRunIsTerminalWhenOnlyMatchingRunnerIsDisabled(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})

	scenarioName := seedPlacement(t, store,
		labelledRunner("disabled-runner", false, manifest.Labels{"os": "linux"}),
		manifest.LabelSelector{MatchLabels: manifest.Labels{"os": "linux"}},
	)

	result, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	requireUnplaceable(t, db, result, urth.ReasonNoEligibleRunner)
}

// A scenario whose requirements do not parse cannot place either, and says so in
// the same way rather than failing the request.
//
// The selector is stored, so the failure belongs to the run: refusing the POST
// would leave a scheduled trigger -- which has no caller to receive the refusal
// -- with nothing recorded at all.
func TestRunIsTerminalWhenRequirementsDoNotParse(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})

	scenarioName := seedPlacement(t, store,
		labelledRunner("any-runner", true, nil),
		// A key with a space in it is not label grammar, so the selector this
		// renders to cannot be parsed.
		manifest.LabelSelector{MatchLabels: manifest.Labels{"not a key": "linux"}},
	)

	result, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	requireUnplaceable(t, db, result, urth.ReasonInvalidRequirements)
}

// The placed case still works: this is the regression guard for the change
// above, which sits directly on the path every healthy run takes.
func TestMatchingRunnerStillPlacesAndDispatches(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})

	scenarioName := seedPlacement(t, store,
		labelledRunner("linux-runner", true, manifest.Labels{"os": "linux"}),
		manifest.LabelSelector{MatchLabels: manifest.Labels{"os": "linux"}},
	)

	result, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	require.Equal(t, urth.JobPending, result.Status.Status)
	require.Equal(t, manifest.ResourceName("linux-runner"), result.Status.Executor.RunnerName)
	require.EqualValues(t, 1, countOutbox(t, db))
}

// The preview answers the same question placement does, before a run exists.
//
// The counts are separated because they prompt different actions: a selector
// that matches nothing needs a runner or an edit, matching runners that are all
// disabled needs one enabled, and eligible runners with no workers needs nobody
// -- the dispatch waits in the queue.
func TestPlacementPreviewCountsRunnersAndWorkers(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{})
	ctx := context.Background()

	scenarioName := seedPlacement(t, store,
		labelledRunner("test-runner", true, manifest.Labels{"os": "linux"}),
		manifest.LabelSelector{MatchLabels: manifest.Labels{"os": "linux"}},
	)
	// A second runner the selector matches but which is disabled, and a third it
	// does not match at all.
	require.NoError(t, store.Create(ctx, ptr(labelledRunner("disabled-runner", false, manifest.Labels{"os": "linux"}))))
	require.NoError(t, store.Create(ctx, ptr(labelledRunner("windows-runner", true, manifest.Labels{"os": "windows"}))))

	preview, exists, err := srv.Scenarios().Placement(ctx, scenarioName)
	require.NoError(t, err)
	require.True(t, exists)

	require.Equal(t, "os=linux", preview.Requirements)
	require.Equal(t, 2, preview.MatchingRunners)
	require.Equal(t, 1, preview.EligibleRunners)
	require.True(t, preview.Schedulable)
	require.Empty(t, preview.Reason)
	require.Zero(t, preview.RegisteredWorkers, "no worker has registered yet")

	// A worker registers against the eligible runner, and is then paused.
	session := workerSession(t, srv, "test-worker")
	require.NotEmpty(t, session)

	preview, _, err = srv.Scenarios().Placement(ctx, scenarioName)
	require.NoError(t, err)
	require.Equal(t, 1, preview.RegisteredWorkers)
	require.Equal(t, 1, preview.ReadyWorkers)

	_, found, err := srv.Workers().SetPaused(ctx, "test-worker", true)
	require.NoError(t, err)
	require.True(t, found)

	preview, _, err = srv.Scenarios().Placement(ctx, scenarioName)
	require.NoError(t, err)
	require.Equal(t, 1, preview.RegisteredWorkers)
	require.Zero(t, preview.ReadyWorkers, "a paused worker is registered but will take no job")
	require.True(t, preview.Schedulable,
		"a runner with no ready worker still queues the dispatch")
}

// Nothing eligible is reported with the reason a run would carry, so a client
// shows the same words the run will.
func TestPlacementPreviewReportsWhyItCannotSchedule(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{})

	scenarioName := seedPlacement(t, store,
		labelledRunner("windows-runner", true, manifest.Labels{"os": "windows"}),
		manifest.LabelSelector{MatchLabels: manifest.Labels{"os": "linux"}},
	)

	preview, exists, err := srv.Scenarios().Placement(context.Background(), scenarioName)
	require.NoError(t, err)
	require.True(t, exists)

	require.False(t, preview.Schedulable)
	require.Equal(t, urth.ReasonNoEligibleRunner, preview.Reason)
	require.Zero(t, preview.MatchingRunners)
}

// A selector that does not parse is reported, not returned as a failed request:
// this endpoint exists to explain why a run cannot be made, and that is one of
// the answers.
func TestPlacementPreviewReportsUnparseableRequirements(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{})

	scenarioName := seedPlacement(t, store,
		labelledRunner("any-runner", true, nil),
		manifest.LabelSelector{MatchLabels: manifest.Labels{"not a key": "linux"}},
	)

	preview, exists, err := srv.Scenarios().Placement(context.Background(), scenarioName)
	require.NoError(t, err)
	require.True(t, exists)

	require.False(t, preview.Schedulable)
	require.Equal(t, urth.ReasonInvalidRequirements, preview.Reason)
	require.NotEmpty(t, preview.Detail)
}

// An unknown scenario is reported as absent rather than as an empty preview,
// which would read as "nothing can run it".
func TestPlacementPreviewReportsUnknownScenario(t *testing.T) {
	srv, _, _ := newTestService(t, &stubScheduler{})

	_, exists, err := srv.Scenarios().Placement(context.Background(), "no-such-scenario")
	require.NoError(t, err)
	require.False(t, exists)
}

// A scenario with a selector that cannot parse is refused on the way in, so the
// case above stays a migration condition rather than something an operator can
// still create today.
func TestScenarioWithUnparseableRequirementsIsRefused(t *testing.T) {
	srv, _, _ := newTestService(t, &stubScheduler{})

	scenario := urth.Scenario{
		ObjectMeta: manifest.ObjectMeta{Name: "bad-requirements"},
		Spec: urth.ScenarioSpec{
			IsActive:     true,
			Requirements: manifest.LabelSelector{MatchLabels: manifest.Labels{"not a key": "linux"}},
			Prob: prob.Manifest{
				Kind: "http",
				Spec: map[string]any{"target": "http://example.com"},
			},
		},
	}

	_, err := srv.Scenarios().Create(context.Background(), scenario.ToManifest())
	require.Error(t, err)
}
