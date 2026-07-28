package urth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The tests below need the whole resource schema, which only migrates on
// Postgres -- wyrd's ResourceMeta carries a hardcoded `index:idx_name` that
// every model embeds, so the second CREATE INDEX collides on SQLite. That is
// also why they are the tests that matter most: the atomicity being asserted is
// a property of a real transaction, not of a store abstraction.

func newTestService(t *testing.T, scheduler urth.Scheduler, options ...urth.ServiceOption) (urth.Service, *gorm.DB, *dbstore.DBStore) {
	t.Helper()

	db := openPostgres(t)

	models := []any{
		&urth.WorkerInstance{},
		&urth.Runner{},
		&urth.Scenario{},
		&urth.Result{},
		&urth.Artifact{},
		&urth.DispatchOutboxEntry{},
		&urth.ReconcileLease{},
	}

	// Dropped in reverse dependency order so a rerun starts clean; leftover rows
	// from a previous run would make "exactly one outbox entry" meaningless.
	for i := len(models) - 1; i >= 0; i-- {
		_ = db.Migrator().DropTable(models[i])
	}
	require.NoError(t, db.AutoMigrate(models...))
	t.Cleanup(func() {
		for i := len(models) - 1; i >= 0; i-- {
			_ = db.Migrator().DropTable(models[i])
		}
	})

	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	require.NoError(t, err)

	return urth.NewService(store, scheduler, options...), db, store
}

// seedScenario creates an active runner and an active scenario that will place
// on it, and returns the scenario's name.
func seedScenario(t *testing.T, store *dbstore.DBStore) manifest.ResourceName {
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
			// A spec the registered http prober actually accepts. The prober
			// packages are linked into this test binary (see execution_test.go)
			// exactly as they are into the API server, so a stored prob is
			// decoded strictly against its registered type rather than falling
			// back to an untyped map.
			Prob: prob.Manifest{
				Kind: "http",
				Spec: map[string]any{"target": "http://example.com"},
			},
		},
	}
	require.NoError(t, store.Create(ctx, &scenario))

	return scenario.Name
}

func newRunRequest() manifest.ResourceManifest {
	return manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{Kind: urth.KindResult},
		Metadata: manifest.ObjectMeta{Name: "manual-"},
		Spec:     &urth.ResultSpec{},
	}
}

func countOutbox(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var total int64
	require.NoError(t, db.Model(&urth.DispatchOutboxEntry{}).Count(&total).Error)

	return total
}

// A pending Result and its dispatch must become durable in the same commit.
//
// Committing the Result and then publishing is a dual write: crash in between
// and authoritative state says a run is pending while nothing exists to wake a
// worker. JetStream's deduplication cannot help -- it suppresses a repeated
// publication, and here there was never a first one.
func TestCreateResultCommitsDispatchAtomically(t *testing.T) {
	scheduler := &stubScheduler{}
	srv, db, store := newTestService(t, scheduler)
	scenarioName := seedScenario(t, store)

	result, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)
	require.Equal(t, urth.JobPending, result.Status.Status)
	require.NotEmpty(t, result.Status.Executor.RunnerID, "the run should have been placed on the seeded runner")

	var entries []urth.DispatchOutboxEntry
	require.NoError(t, db.Find(&entries).Error)
	require.Len(t, entries, 1)

	entry := entries[0]
	require.Equal(t, urth.DispatchEventUID(result.UID, result.Version), entry.EventUID)
	require.Equal(t, result.UID, entry.ResultUID)
	require.Equal(t, result.Version, entry.ResultVersion)
	require.Equal(t, result.Status.Executor.RunnerID, entry.RunnerUID)
	require.Nil(t, entry.PublishedAt)
	require.Zero(t, entry.Attempts)

	require.Empty(t, scheduler.scheduled,
		"creating a Result must not publish; the relay owns publication")
}

// The other half of atomicity: if the dispatch cannot be written, the Result
// must not exist either. A Result nobody can dispatch is worse than no Result --
// it reports a run as pending that will never happen.
func TestCreateResultRollsBackWhenDispatchCannotBeWritten(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	// Schema drift is the realistic version of this failure: an API server
	// deployed ahead of its migration writes a Result and then fails to write
	// the outbox row.
	require.NoError(t, db.Migrator().DropTable(&urth.DispatchOutboxEntry{}))

	_, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.Error(t, err)

	var results []urth.Result
	require.NoError(t, db.Find(&results).Error)
	require.Empty(t, results, "the Result must have rolled back with its dispatch")
}

// A Result whose dispatch cannot be published stays pending.
//
// The prototype rewrote it as `errored`, which is a lie in two directions: no
// execution was attempted, so there is no error to report, and marking it
// terminal removes the only record that would let a retry happen once the broker
// returns.
func TestPublicationFailureLeavesResultPending(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	created, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	relay := urth.NewDispatchRelay(
		urth.NewDispatchOutbox(db),
		&recordingPublisher{err: errors.New("nats: no servers available for connection")},
	)

	published, err := relay.RunOnce(context.Background())
	require.Error(t, err)
	require.Zero(t, published)

	var stored urth.Result
	found, err := store.GetByUID(context.Background(), &stored, created.UID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, urth.JobPending, stored.Status.Status,
		"a broker outage is not an execution failure")

	var entry urth.DispatchOutboxEntry
	require.NoError(t, db.First(&entry).Error)
	require.Nil(t, entry.PublishedAt)
	require.Equal(t, 1, entry.Attempts)
	require.Contains(t, entry.LastError, "no servers available")

	stats, err := urth.NewDispatchOutbox(db).Stats(context.Background(), time.Now())
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Pending)
	require.EqualValues(t, 1, stats.Failing)
}

// Once the transport recovers, the committed entry is published without anyone
// re-creating the Result. This is the whole promise of the outbox: a dispatch
// survives the broker being unreachable at the moment the run was requested.
func TestCommittedDispatchIsPublishedAfterTransportRecovers(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	created, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	outbox := urth.NewDispatchOutbox(db)
	publisher := &recordingPublisher{err: errors.New("nats: no servers available for connection")}
	// No backoff: the point under test is recovery, not the retry schedule.
	relay := urth.NewDispatchRelay(outbox, publisher, urth.WithRelayBackoff(0, 0))

	_, err = relay.RunOnce(context.Background())
	require.Error(t, err)

	// The broker comes back.
	publisher.err = nil

	published, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, published)
	require.Equal(t, []string{urth.DispatchEventUID(created.UID, created.Version)}, publisher.uids())

	var entry urth.DispatchOutboxEntry
	require.NoError(t, db.First(&entry).Error)
	require.NotNil(t, entry.PublishedAt)

	stats, err := outbox.Stats(context.Background(), time.Now())
	require.NoError(t, err)
	require.Zero(t, stats.Pending)
}

// A run of a disabled scenario is recorded but never dispatched, so it must not
// leave an entry the relay would spend attempts on.
func TestInactiveScenarioWritesNoDispatch(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)

	ctx := context.Background()

	var scenario urth.Scenario
	found, err := store.GetByName(ctx, &scenario, "test-scenario")
	require.NoError(t, err)
	require.True(t, found)

	scenario.Spec.IsActive = false
	_, err = store.CreateOrUpdate(ctx, &scenario)
	require.NoError(t, err)

	_, err = srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.Error(t, err, "an inactive scenario refuses to run")
	require.Zero(t, countOutbox(t, db))
}
