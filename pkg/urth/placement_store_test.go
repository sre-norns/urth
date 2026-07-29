package urth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Capacity-aware placement against the real schema. These need the whole
// resource schema, so they run on Postgres for the reason given in
// service_outbox_test.go.

// UIDs are assigned explicitly by these tests rather than generated.
//
// Placement's fallback ordering is by UID, so a test that lets the store mint
// random ones cannot say whether the winner won on capacity or on a coin toss --
// it would pass against the very behaviour it is meant to catch about half the
// time. Fixing the order makes "capacity beat UID" an assertion rather than a
// hope.
const (
	firstRunnerUID  = "00000000-0000-0000-0000-0000000000a1"
	secondRunnerUID = "00000000-0000-0000-0000-0000000000b2"
)

// seedRunnerWithWorkers creates a runner and the given number of workers, all of
// them reporting as reachable a moment ago.
func seedRunnerWithWorkers(t *testing.T, store *dbstore.DBStore, db *gorm.DB, uid string, name manifest.ResourceName, online int) urth.Runner {
	t.Helper()

	ctx := context.Background()

	runner := urth.Runner{
		ObjectMeta: manifest.ObjectMeta{UID: manifest.ResourceID(uid), Name: name},
		Spec:       urth.RunnerSpec{IsActive: true},
	}
	require.NoError(t, store.Create(ctx, &runner))

	presence := urth.NewWorkerPresenceStore(db)

	for i := range online {
		worker := urth.WorkerInstance{
			ObjectMeta: manifest.ObjectMeta{
				Name: manifest.ResourceName(string(name) + "-worker-" + string(rune('a'+i))),
				// Placement finds a runner's workers by label, as the rest of the
				// system does; see placement.workerCapacity.
				Labels: manifest.Labels{urth.LabelRunnerUID: string(runner.UID)},
			},
			Spec: urth.WorkerInstanceSpec{RunnerID: runner.UID},
		}
		require.NoError(t, store.Create(ctx, &worker))

		// Both signals, because capacity counts only workers that are reachable
		// on both -- one alone would leave them api- or nats-unreachable.
		_, err := presence.RecordContact(ctx, worker.UID, time.Now(), urth.WorkerContactHeartbeat, false)
		require.NoError(t, err)
		_, err = presence.RecordNATSPresence(ctx, worker.UID, runner.UID, time.Now())
		require.NoError(t, err)
	}

	return runner
}

// seedOpenScenario creates a scenario with no requirements, so every runner is
// eligible and placement is choosing purely on capacity.
func seedOpenScenario(t *testing.T, store *dbstore.DBStore, name manifest.ResourceName) urth.Scenario {
	t.Helper()

	scenario := urth.Scenario{
		ObjectMeta: manifest.ObjectMeta{Name: name},
		Spec: urth.ScenarioSpec{
			IsActive: true,
			// A field the registered http prober actually accepts. Prob specs are
			// decoded strictly in this package's tests, so a made-up field fails
			// with "unknown field" rather than round-tripping unnoticed.
			Prob: prob.Manifest{
				Kind: "http",
				Spec: map[string]any{"target": "http://example.com"},
			},
		},
	}
	require.NoError(t, store.Create(context.Background(), &scenario))

	return scenario
}

// placementService builds a service that reads capacity.
func placementService(t *testing.T) (urth.Service, *gorm.DB, *dbstore.DBStore) {
	t.Helper()

	_, db, store := newTestService(t, &stubScheduler{})

	srv := urth.NewService(store, &stubScheduler{},
		urth.WithSigningKeys(testKeys(t)),
		urth.WithWorkerPresence(urth.NewWorkerPresenceStore(db)),
		urth.WithRunnerLoad(urth.NewRunnerLoadStore(db)),
	)

	return srv, db, store
}

// runnerOf reads which runner a run was placed on.
func runnerOf(t *testing.T, store *dbstore.DBStore, uid manifest.ResourceID) manifest.ResourceName {
	t.Helper()

	var result urth.Result
	found, err := store.GetByUID(context.Background(), &result, uid)
	require.NoError(t, err)
	require.True(t, found)

	return result.Status.Executor.RunnerName
}

// The whole point of the change: successive runs spread across the fleet.
//
// Before this, placement sorted eligible runners by UID and took the first, so
// every run of a broadly-selecting scenario landed on the same runner while its
// siblings sat idle. It is a Postgres test rather than a ranking test because
// the self-spreading property depends on a placed run being visible as committed
// work to the next placement -- which is a database round trip, not arithmetic.
func TestPlacementSpreadsRunsAcrossEquivalentRunners(t *testing.T) {
	srv, db, store := placementService(t)

	seedRunnerWithWorkers(t, store, db, firstRunnerUID, "runner-aaa", 2)
	seedRunnerWithWorkers(t, store, db, secondRunnerUID, "runner-bbb", 2)
	scenario := seedOpenScenario(t, store, "spread-scenario")

	ctx := context.Background()

	placed := map[manifest.ResourceName]int{}
	for range 4 {
		created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
		require.NoError(t, err)

		placed[runnerOf(t, store, created.UID)]++
	}

	require.Equal(t, 2, placed["runner-aaa"], "four runs over two runners of two workers each")
	require.Equal(t, 2, placed["runner-bbb"])
}

// Capacity, not identity, decides. The runner that would have won on UID order
// has no reachable workers at all.
func TestPlacementAvoidsARunnerWithNoReachableWorkers(t *testing.T) {
	srv, db, store := placementService(t)

	// Registered but never reported: presence `unknown`, so no capacity.
	seedRunnerWithWorkers(t, store, db, firstRunnerUID, "runner-aaa", 0)
	seedRunnerWithWorkers(t, store, db, secondRunnerUID, "runner-bbb", 1)
	scenario := seedOpenScenario(t, store, "avoid-scenario")

	created, err := srv.Results(scenario.Name).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	require.Equal(t, manifest.ResourceName("runner-bbb"), runnerOf(t, store, created.UID),
		"a runner nobody is on must not win merely because its UID sorts first")
}

// An entirely offline fleet still takes work. The queue is durable, so a run
// waits for the fleet to come back; refusing it here would be a different bug
// from the one this change fixes.
func TestPlacementQueuesWhenTheWholeFleetIsOffline(t *testing.T) {
	srv, db, store := placementService(t)

	seedRunnerWithWorkers(t, store, db, firstRunnerUID, "runner-aaa", 0)
	seedRunnerWithWorkers(t, store, db, secondRunnerUID, "runner-bbb", 0)
	scenario := seedOpenScenario(t, store, "offline-scenario")

	created, err := srv.Results(scenario.Name).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	var result urth.Result
	found, err := store.GetByUID(context.Background(), &result, created.UID)
	require.NoError(t, err)
	require.True(t, found)

	require.Equal(t, urth.JobPending, result.Status.Status, "an offline fleet queues, it does not refuse")
	require.NotEmpty(t, result.Status.Executor.RunnerID, "a queued run still records where it is queued")
	require.Empty(t, result.Labels[urth.LabelResultUnschedulable])
}

// A pending run is never attributed to a worker. Placement binds a run to a
// runner and nothing more; the worker is recorded at claim, which is the only
// moment the association is certain.
func TestPlacementRecordsNoWorkerOnAPendingRun(t *testing.T) {
	srv, db, store := placementService(t)

	seedRunnerWithWorkers(t, store, db, firstRunnerUID, "runner-aaa", 2)
	scenario := seedOpenScenario(t, store, "no-worker-scenario")

	created, err := srv.Results(scenario.Name).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	var result urth.Result
	found, err := store.GetByUID(context.Background(), &result, created.UID)
	require.NoError(t, err)
	require.True(t, found)

	require.NotEmpty(t, result.Status.Executor.RunnerID)
	require.Empty(t, result.Status.Executor.WorkerID, "nothing is scheduled to a worker")
	require.Empty(t, result.Status.Executor.WorkerName)
}

// InFlightRuns is the query placement leans on, so what it counts matters as
// much as that it runs: unfinished work only, attributed per runner, and nothing
// from runs that were never placed.
func TestInFlightRunsCountsUnfinishedWorkPerRunner(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})

	ctx := context.Background()
	runnerA := seedRunnerWithWorkers(t, store, db, firstRunnerUID, "runner-aaa", 1)
	runnerB := seedRunnerWithWorkers(t, store, db, secondRunnerUID, "runner-bbb", 1)
	// Every run belongs to a scenario: scenario_id is a uuid column and will not
	// take an empty string.
	scenario := seedOpenScenario(t, store, "load-scenario")

	newResult := func(name string, status urth.JobStatus, runner urth.Runner) urth.Result {
		result := urth.Result{
			ObjectMeta: manifest.ObjectMeta{Name: manifest.ResourceName(name)},
			Spec:       urth.ResultSpec{ScenarioID: scenario.UID},
			Status: urth.ResultStatus{
				Status: status,
				Executor: urth.ExecutorRef{
					RunnerID:   runner.UID,
					RunnerName: runner.Name,
				},
			},
		}
		require.NoError(t, store.Create(ctx, &result))

		return result
	}

	newResult("a-pending-1", urth.JobPending, runnerA)
	newResult("a-pending-2", urth.JobPending, runnerA)
	newResult("a-running-1", urth.JobRunning, runnerA)
	newResult("b-running-1", urth.JobRunning, runnerB)

	// Finished work is not load: it is history.
	newResult("a-done", urth.JobCompleted, runnerA)
	newResult("a-errored", urth.JobErrored, runnerA)
	newResult("a-timeout", urth.JobExpired, runnerA)

	// A run nothing could take carries no runner and is nobody's load.
	unplaced := urth.Result{
		ObjectMeta: manifest.ObjectMeta{Name: "unplaced"},
		Spec:       urth.ResultSpec{ScenarioID: scenario.UID},
		Status:     urth.ResultStatus{Status: urth.JobPending},
	}
	require.NoError(t, store.Create(ctx, &unplaced))

	// A deleted run stops counting against the runner it was on.
	deleted := newResult("a-deleted", urth.JobPending, runnerA)
	_, err := store.Delete(ctx, &urth.Result{}, deleted.UID, deleted.Version)
	require.NoError(t, err)

	load, err := urth.NewRunnerLoadStore(db).InFlightRuns(ctx)
	require.NoError(t, err)

	require.Equal(t, urth.RunnerLoad{Queued: 2, Running: 1}, load[runnerA.UID])
	require.Equal(t, urth.RunnerLoad{Running: 1}, load[runnerB.UID])
	require.Len(t, load, 2, "an unplaced run must not appear as a runner's load")
}

// A capacity signal that cannot be read must not stop runs being created. It
// degrades to the deterministic choice that predates this change.
func TestPlacementSurvivesACapacityFailure(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})

	srv := urth.NewService(store, &stubScheduler{},
		urth.WithSigningKeys(testKeys(t)),
		urth.WithWorkerPresence(urth.NewWorkerPresenceStore(db)),
		urth.WithRunnerLoad(failingRunnerLoad{}),
	)

	seedRunnerWithWorkers(t, store, db, firstRunnerUID, "runner-aaa", 1)
	seedRunnerWithWorkers(t, store, db, secondRunnerUID, "runner-bbb", 1)
	scenario := seedOpenScenario(t, store, "degraded-scenario")

	created, err := srv.Results(scenario.Name).Create(context.Background(), newRunRequest())
	require.NoError(t, err, "a run must still be created when capacity cannot be read")

	require.Equal(t, manifest.ResourceName("runner-aaa"), runnerOf(t, store, created.UID),
		"the fallback is the deterministic lowest-UID choice")
}

// failingRunnerLoad stands in for a database that will not answer.
type failingRunnerLoad struct{}

func (failingRunnerLoad) InFlightRuns(context.Context) (map[manifest.ResourceID]urth.RunnerLoad, error) {
	return nil, context.DeadlineExceeded
}
