package urth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/sre-norns/urth/pkg/urth"
)

// PostgresTestURLEnv names the environment variable carrying a disposable
// Postgres for the tests that cannot be honestly written against anything else.
//
// Row leasing is the obvious case: `SELECT ... FOR UPDATE SKIP LOCKED` is what
// stops two relays owning one entry, and SQLite has neither the clause nor the
// concurrent writers that make it necessary. A green suite that never exercised
// it would be asserting the design, not the implementation.
const PostgresTestURLEnv = "URTH_TEST_POSTGRES_URL"

// openOutboxSQLite gives the outbox its own in-memory database.
//
// The outbox table is not a manifest resource, so it escapes the wyrd
// `idx_name` collision that makes SQLite unusable for the resource schema. That
// makes the relay's bookkeeping -- backoff, retries, lease expiry -- testable
// without a container, while the concurrency cases below still demand Postgres.
func openOutboxSQLite(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(sqliteDSN(t)), &gorm.Config{
		Logger: logger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&urth.DispatchOutboxEntry{}))

	return db
}

// sqliteDSN names a private in-memory database for one test.
//
// `cache=shared` and a name are both required. A bare `file::memory:` gives
// every pooled connection its own empty database, so a write made on one
// connection and read back on another silently sees nothing -- which looks
// exactly like the store failing to persist, and cost an afternoon before it
// looked like what it was.
func sqliteDSN(t *testing.T) string {
	t.Helper()

	return fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
}

// openPostgres connects to the database named by PostgresTestURLEnv, skipping
// the test when none is configured.
func openPostgres(t *testing.T) *gorm.DB {
	t.Helper()

	url := os.Getenv(PostgresTestURLEnv)
	if url == "" {
		t.Skipf("set %s to run this test (see `make run-postgres-podman`)", PostgresTestURLEnv)
	}

	db, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger: logger.Discard,
	})
	require.NoError(t, err)

	return db
}

// migrateOutbox gives one test its own outbox table, dropped afterwards so
// repeated runs do not see each other's rows.
func migrateOutbox(t *testing.T, db *gorm.DB) {
	t.Helper()

	require.NoError(t, db.Migrator().DropTable(&urth.DispatchOutboxEntry{}))
	require.NoError(t, db.AutoMigrate(&urth.DispatchOutboxEntry{}))
	t.Cleanup(func() {
		_ = db.Migrator().DropTable(&urth.DispatchOutboxEntry{})
	})
}

func enqueue(t *testing.T, db *gorm.DB, eventUID string) urth.DispatchOutboxEntry {
	t.Helper()

	entry := urth.DispatchOutboxEntry{
		SchemaVersion: urth.DispatchOutboxEntryVersion,
		EventUID:      eventUID,
		ResultUID:     "result-1",
		ResultVersion: 1,
		ScenarioName:  "test-scenario",
		RunnerUID:     "runner-1",
		NotBefore:     time.Now().Add(-time.Second),
	}
	require.NoError(t, db.Create(&entry).Error)

	return entry
}

func TestOutboxClaimLeasesAndReleases(t *testing.T) {
	db := openOutboxSQLite(t)
	outbox := urth.NewDispatchOutbox(db)
	ctx := context.Background()

	enqueue(t, db, "result-1.1")

	claimed, err := outbox.Claim(ctx, "relay-a", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "relay-a", claimed[0].ClaimedBy)
	require.Equal(t, 1, claimed[0].Attempts, "the attempt is counted at claim time, not after it")

	// A live lease is not re-offered.
	again, err := outbox.Claim(ctx, "relay-b", 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, again)

	require.NoError(t, outbox.MarkPublished(ctx, claimed[0].ID, time.Now()))

	// A published entry is never offered again.
	after, err := outbox.Claim(ctx, "relay-a", 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, after)
}

// A relay that dies mid-publication leaves its name on the row. Nothing else
// would ever release it, so the lease expiry is the only path back -- without
// it, one crash strands that Result permanently.
func TestOutboxReclaimsExpiredLease(t *testing.T) {
	db := openOutboxSQLite(t)
	outbox := urth.NewDispatchOutbox(db)
	ctx := context.Background()

	enqueue(t, db, "result-1.1")

	claimed, err := outbox.Claim(ctx, "relay-that-died", 10, -time.Second)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	reclaimed, err := outbox.Claim(ctx, "relay-b", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, reclaimed, 1)
	require.Equal(t, claimed[0].ID, reclaimed[0].ID)
	require.Equal(t, 2, reclaimed[0].Attempts,
		"an attempt that killed its relay must still be counted, or the entry looks healthy forever")
}

// A recorded failure holds the entry back and stays readable. Both halves
// matter: the backoff keeps a broker outage from becoming a spin loop, and the
// stored error is the only explanation an operator gets.
func TestOutboxMarkFailedSchedulesRetry(t *testing.T) {
	db := openOutboxSQLite(t)
	outbox := urth.NewDispatchOutbox(db)
	ctx := context.Background()

	entry := enqueue(t, db, "result-1.1")
	_, err := outbox.Claim(ctx, "relay-a", 10, time.Minute)
	require.NoError(t, err)

	require.NoError(t, outbox.MarkFailed(ctx, entry.ID, errors.New("no responders"), time.Now().Add(time.Hour)))

	held, err := outbox.Claim(ctx, "relay-a", 10, time.Minute)
	require.NoError(t, err)
	require.Empty(t, held, "an entry inside its backoff must not be re-offered")

	stats, err := outbox.Stats(ctx, time.Now())
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Pending)
	require.EqualValues(t, 1, stats.Failing)
	require.Equal(t, 1, stats.MaxAttempts)
	require.Contains(t, stats.LastError, "no responders")
}

// The backlog has to be visible before it is a problem, not after: OldestAge is
// the number that stays near zero while the relay keeps up regardless of how
// much work is flowing through it.
func TestOutboxStatsReportsBacklogAge(t *testing.T) {
	db := openOutboxSQLite(t)
	outbox := urth.NewDispatchOutbox(db)
	ctx := context.Background()

	entry := enqueue(t, db, "result-1.1")
	require.NoError(t, db.Model(&urth.DispatchOutboxEntry{}).Where("id = ?", entry.ID).
		Update("created_at", time.Now().Add(-2*time.Hour)).Error)

	stats, err := outbox.Stats(ctx, time.Now())
	require.NoError(t, err)
	require.InDelta(t, (2 * time.Hour).Seconds(), stats.OldestAge.Seconds(), 60)

	require.NoError(t, outbox.MarkPublished(ctx, entry.ID, time.Now()))

	drained, err := outbox.Stats(ctx, time.Now())
	require.NoError(t, err)
	require.Zero(t, drained.Pending)
	require.Zero(t, drained.OldestAge)
}

// Two relays racing for one backlog must partition it, not duplicate it.
//
// Running relays in every API replica is the default, so this is the normal
// case rather than an exotic one. Postgres only: the guarantee is `FOR UPDATE
// SKIP LOCKED`, and testing it anywhere else tests nothing.
func TestOutboxCompetingRelaysDoNotDoubleClaim(t *testing.T) {
	db := openPostgres(t)
	migrateOutbox(t, db)

	outbox := urth.NewDispatchOutbox(db)
	ctx := context.Background()

	const entries = 40
	for i := range entries {
		enqueue(t, db, "result-1."+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}

	const relays = 4

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = map[uint]string{}
		dupes   []uint
	)

	for relay := range relays {
		wg.Add(1)
		go func() {
			defer wg.Done()

			relayID := "relay-" + string(rune('a'+relay))
			for {
				batch, err := outbox.Claim(ctx, relayID, 7, time.Minute)
				if err != nil || len(batch) == 0 {
					return
				}

				mu.Lock()
				for _, entry := range batch {
					if owner, seen := claimed[entry.ID]; seen {
						dupes = append(dupes, entry.ID)
						_ = owner
					}
					claimed[entry.ID] = relayID
				}
				mu.Unlock()

				for _, entry := range batch {
					require.NoError(t, outbox.MarkPublished(ctx, entry.ID, time.Now()))
				}
			}
		}()
	}

	wg.Wait()

	require.Empty(t, dupes, "an entry was leased to two relays at once")
	require.Len(t, claimed, entries, "every entry should have been claimed exactly once")
}
