package natsq_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const testRunnerUID = manifest.ResourceID("runner-outbox")

// restartableNATS is a JetStream server that can be stopped and brought back on
// the same port and store directory.
//
// Being able to take the broker away mid-test is the point: the outbox exists
// for the window in which Postgres has committed and NATS cannot be reached, and
// a test that never takes NATS away never enters that window.
type restartableNATS struct {
	t    *testing.T
	dir  string
	port int
	srv  *natsserver.Server
}

func newRestartableNATS(t *testing.T) *restartableNATS {
	t.Helper()

	// A store directory that outlives individual server instances, so a restart
	// finds the stream and its message IDs -- including the deduplication state
	// the relay's retry depends on.
	s := &restartableNATS{t: t, dir: t.TempDir(), port: -1}
	s.start()
	t.Cleanup(s.stop)

	return s
}

func (s *restartableNATS) start() {
	s.t.Helper()

	srv, err := natsserver.NewServer(&natsserver.Options{
		Host:      "127.0.0.1",
		Port:      s.port,
		JetStream: true,
		StoreDir:  s.dir,
	})
	if err != nil {
		s.t.Fatalf("failed to create NATS server: %v", err)
	}

	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		s.t.Fatal("NATS server did not become ready")
	}

	s.srv = srv
	s.port = srv.Addr().(*net.TCPAddr).Port
}

func (s *restartableNATS) stop() {
	if s.srv == nil {
		return
	}
	s.srv.Shutdown()
	s.srv.WaitForShutdown()
	s.srv = nil
}

func (s *restartableNATS) url() string {
	return "nats://127.0.0.1:" + itoa(s.port)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}

	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}

	return string(digits)
}

func outboxTestConfig(url string) natsq.Config {
	cfg := testConfig()
	cfg.URL = url

	return cfg
}

// sqliteOutbox backs the relay with a real store, so claim, lease and publish
// bookkeeping are exercised rather than stubbed.
func sqliteOutbox(t *testing.T) *gorm.DB {
	t.Helper()

	// A named shared-cache database, not a bare `file::memory:`. The latter gives
	// every pooled connection its own empty database, so a row written on one and
	// read on another silently vanishes -- which reads as the store being broken.
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&urth.DispatchOutboxEntry{}); err != nil {
		t.Fatalf("failed to migrate outbox: %v", err)
	}

	return db
}

func enqueueDispatch(t *testing.T, db *gorm.DB, eventUID string) urth.DispatchOutboxEntry {
	t.Helper()

	entry := urth.DispatchOutboxEntry{
		SchemaVersion: urth.DispatchOutboxEntryVersion,
		EventUID:      eventUID,
		ResultUID:     "result-1",
		ResultVersion: 1,
		ScenarioName:  "test-scenario",
		RunnerUID:     testRunnerUID,
		NotBefore:     time.Now().Add(-time.Second),
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("failed to enqueue dispatch: %v", err)
	}

	return entry
}

// queuedMessages reports how many jobs the stream is holding for the runner.
func queuedMessages(t *testing.T, url string) uint64 {
	t.Helper()

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	defer conn.Close()

	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("failed to init JetStream: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := js.Stream(ctx, natsq.JobsStreamName)
	if err != nil {
		t.Fatalf("failed to look up stream: %v", err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read stream info: %v", err)
	}

	return info.State.Msgs
}

// A dispatch committed while the broker is unreachable is published once the
// broker returns, with nobody re-requesting the run.
//
// This is the failure the outbox was added for. Before it, the publication
// happened inline with the request: NATS being down at that moment meant the
// dispatch was simply never made, and the Result sat pending forever.
func TestRelayPublishesAfterBrokerRecovers(t *testing.T) {
	server := newRestartableNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := outboxTestConfig(server.url())
	transport, err := natsq.NewScheduler(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer transport.Close()

	db := sqliteOutbox(t)
	outbox := urth.NewDispatchOutbox(db)
	// A short publish timeout so the unreachable broker is given up on quickly;
	// no backoff so the recovery poll below is not waiting on a retry schedule.
	relay := urth.NewDispatchRelay(outbox, transport,
		urth.WithRelayBackoff(0, 0),
		urth.WithRelayPublishTimeout(2*time.Second),
	)

	// The Result has committed; the broker is now unavailable.
	server.stop()
	entry := enqueueDispatch(t, db, "result-1.1")

	published, err := relay.RunOnce(ctx)
	if err == nil {
		t.Fatal("publishing to a stopped broker should fail")
	}
	if published != 0 {
		t.Fatalf("published %d dispatches while the broker was down", published)
	}

	var stored urth.DispatchOutboxEntry
	if err := db.First(&stored, entry.ID).Error; err != nil {
		t.Fatalf("failed to reload entry: %v", err)
	}
	if stored.PublishedAt != nil {
		t.Fatal("entry must not be marked published when the broker refused it")
	}
	if stored.LastError == "" {
		t.Fatal("the failure must be recorded on the entry for an operator to see")
	}

	// The broker comes back. Nothing re-creates the Result: the committed row is
	// the only thing that remembers a dispatch is owed.
	server.start()

	deadline := time.Now().Add(30 * time.Second)
	for {
		published, err = relay.RunOnce(ctx)
		if published == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dispatch was never published after the broker recovered: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := db.First(&stored, entry.ID).Error; err != nil {
		t.Fatalf("failed to reload entry: %v", err)
	}
	if stored.PublishedAt == nil {
		t.Fatal("a published entry must be marked, or it will be published again forever")
	}

	if msgs := queuedMessages(t, server.url()); msgs != 1 {
		t.Fatalf("stream holds %d messages, want exactly 1", msgs)
	}
}

// failOnceOutbox drops the first MarkPublished, standing in for a relay that
// died in the window between the broker accepting a message and the row being
// updated.
type failOnceOutbox struct {
	urth.DispatchOutbox

	failed bool
}

func (o *failOnceOutbox) MarkPublished(ctx context.Context, id uint, at time.Time, receipt urth.DispatchReceipt) error {
	if !o.failed {
		o.failed = true
		return errors.New("relay died before recording the publication")
	}

	return o.DispatchOutbox.MarkPublished(ctx, id, at, receipt)
}

// A relay that publishes and dies before marking the row republishes on
// recovery, and JetStream collapses the two into one job.
//
// There is no way to make a broker publication and a database update atomic, so
// the design accepts at-least-once publication and leans on the stable event
// UID as `Nats-Msg-Id`. This test is what says that lean is load-bearing: if the
// ID were regenerated per attempt, the stream below would hold two jobs and the
// scenario would run twice.
func TestRelayCrashBeforeMarkingDeliversOneJob(t *testing.T) {
	server := newRestartableNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	transport, err := natsq.NewScheduler(ctx, outboxTestConfig(server.url()))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer transport.Close()

	db := sqliteOutbox(t)
	outbox := &failOnceOutbox{DispatchOutbox: urth.NewDispatchOutbox(db)}
	relay := urth.NewDispatchRelay(outbox, transport, urth.WithRelayBackoff(0, 0))

	entry := enqueueDispatch(t, db, "result-1.1")

	// Published, then lost before it could be recorded.
	if _, err := relay.RunOnce(ctx); err == nil {
		t.Fatal("expected the simulated relay crash to surface as an error")
	}
	if msgs := queuedMessages(t, server.url()); msgs != 1 {
		t.Fatalf("stream holds %d messages after the first publish, want 1", msgs)
	}

	// The relay restarts and finds the entry still unpublished.
	published, err := relay.RunOnce(ctx)
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if published != 1 {
		t.Fatalf("retry published %d entries, want 1", published)
	}

	var stored urth.DispatchOutboxEntry
	if err := db.First(&stored, entry.ID).Error; err != nil {
		t.Fatalf("failed to reload entry: %v", err)
	}
	if stored.PublishedAt == nil {
		t.Fatal("the retry must mark the entry published")
	}
	if stored.Attempts != 2 {
		t.Fatalf("entry records %d attempts, want 2", stored.Attempts)
	}

	// Two publications of one event UID, one job.
	if msgs := queuedMessages(t, server.url()); msgs != 1 {
		t.Fatalf("stream holds %d messages after the retry, want 1: the duplicate was not suppressed", msgs)
	}
}

// A dispatch that was never placed on a runner has no subject to go to. It is
// reported as permanent so the relay parks it instead of retrying forever.
func TestPublishUnplacedDispatchIsPermanent(t *testing.T) {
	server := newRestartableNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	transport, err := natsq.NewScheduler(ctx, outboxTestConfig(server.url()))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	defer transport.Close()

	_, err = transport.PublishDispatch(ctx, urth.DispatchOutboxEntry{
		SchemaVersion: urth.DispatchOutboxEntryVersion,
		EventUID:      "result-1.1",
		ResultUID:     "result-1",
		ResultVersion: 1,
	})

	if !errors.Is(err, urth.ErrPermanentDispatch) {
		t.Fatalf("unplaced dispatch reported %v, want a permanent failure", err)
	}
	if !errors.Is(err, natsq.ErrNoRunner) {
		t.Fatalf("unplaced dispatch reported %v, want ErrNoRunner", err)
	}
}
