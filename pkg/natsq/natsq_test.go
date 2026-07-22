package natsq_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// startNATS runs an in-process NATS server with JetStream on a temp dir.
//
// Embedded rather than containerised so that `make audit` and CI need no
// broker: the transport's semantics -- work-queue exclusivity, redelivery,
// deduplication -- are exactly the part worth testing against a real server
// rather than a mock, and a mock would have agreed with every wrong assumption
// made while writing this package.
func startNATS(t *testing.T) *nats.Conn {
	t.Helper()

	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // ephemeral, so parallel packages do not collide
		JetStream: true,
		StoreDir:  t.TempDir(),
	}

	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to start NATS server: %v", err)
	}

	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL())
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	t.Cleanup(conn.Close)

	return conn
}

func testConfig() natsq.Config {
	return natsq.Config{
		Replicas:         1,
		MaxJobsPerRunner: 16,
		MaxJobAge:        time.Hour,
		AckWait:          500 * time.Millisecond,
		MaxDeliver:       5,
	}
}

func mustJetStream(t *testing.T, conn *nats.Conn) jetstream.JetStream {
	t.Helper()

	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("failed to init JetStream: %v", err)
	}

	return js
}

func publishJob(t *testing.T, js jetstream.JetStream, runnerUID manifest.ResourceID, resultUID manifest.ResourceID, version manifest.Version) string {
	t.Helper()

	dispatchID := natsq.DispatchIDFor(resultUID, version)
	data, err := natsq.MarshalEnvelope(natsq.DispatchEnvelope{
		SchemaVersion: natsq.DispatchEnvelopeVersion,
		ResultUID:     resultUID,
		ResultVersion: version,
		ScenarioName:  "test-scenario",
		RunnerUID:     runnerUID,
		DispatchID:    dispatchID,
	})
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err = js.Publish(ctx, natsq.JobSubject(runnerUID), data, jetstream.WithMsgID(dispatchID)); err != nil {
		t.Fatalf("failed to publish job: %v", err)
	}

	return dispatchID
}

// TestWorkerDiesBeforeClaimRedelivers covers the ADR 0004 failure-table row
// "Worker receives a message and dies before claim". The whole reason a worker
// acknowledges after the claim rather than on receipt is that this case must
// redeliver; if it did not, a worker crash would silently lose a scheduled run.
func TestWorkerDiesBeforeClaimRedelivers(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)
	cfg := testConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to ensure stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-a")
	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID)
	if err != nil {
		t.Fatalf("failed to ensure consumer: %v", err)
	}

	publishJob(t, js, runnerUID, "result-1", 1)

	// First delivery: the worker takes the message and then "dies" -- it never
	// acks, which is precisely what a crashed worker looks like to JetStream.
	first, err := consumer.Fetch(1)
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	var firstCount int
	for range first.Messages() {
		firstCount++
	}
	if firstCount != 1 {
		t.Fatalf("expected 1 message on first delivery, got %d", firstCount)
	}

	// After AckWait elapses the message must come back to another worker.
	time.Sleep(cfg.AckWait + 500*time.Millisecond)

	second, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}

	var redelivered int
	for msg := range second.Messages() {
		redelivered++
		if err := msg.Ack(); err != nil {
			t.Fatalf("failed to ack redelivered message: %v", err)
		}
	}

	if redelivered != 1 {
		t.Errorf("unacknowledged job was not redelivered: got %d messages, want 1", redelivered)
	}
}

// TestAckedJobIsNotRedelivered is the other half: once a worker has claimed and
// acked, the job is gone. A work-queue stream that kept redelivering acked
// messages would run every scenario repeatedly.
func TestAckedJobIsNotRedelivered(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)
	cfg := testConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to ensure stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-b")
	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID)
	if err != nil {
		t.Fatalf("failed to ensure consumer: %v", err)
	}

	publishJob(t, js, runnerUID, "result-2", 1)

	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	for msg := range batch.Messages() {
		if err := msg.Ack(); err != nil {
			t.Fatalf("failed to ack: %v", err)
		}
	}

	time.Sleep(cfg.AckWait + 500*time.Millisecond)

	after, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("post-ack fetch failed: %v", err)
	}
	var got int
	for range after.Messages() {
		got++
	}

	if got != 0 {
		t.Errorf("acknowledged job was redelivered %d times, want 0", got)
	}
}

// TestDuplicatePublishIsSuppressed checks that republishing the same dispatch
// -- what an outbox relay retry or a lost publish ack produces -- does not
// enqueue the run twice.
func TestDuplicatePublishIsSuppressed(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)
	cfg := testConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to ensure stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-c")
	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID)
	if err != nil {
		t.Fatalf("failed to ensure consumer: %v", err)
	}

	publishJob(t, js, runnerUID, "result-3", 1)
	publishJob(t, js, runnerUID, "result-3", 1)

	batch, err := consumer.Fetch(10, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}

	var got int
	for msg := range batch.Messages() {
		got++
		if err := msg.Ack(); err != nil {
			t.Fatalf("failed to ack: %v", err)
		}
	}

	if got != 1 {
		t.Errorf("duplicate publication delivered %d messages, want 1", got)
	}
}

// TestRunnersDoNotSeeEachOthersJobs is the isolation guarantee the whole
// subject scheme exists for. One shared stream is only acceptable if a runner's
// consumer can never observe another runner's work.
func TestRunnersDoNotSeeEachOthersJobs(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)
	cfg := testConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to ensure stream: %v", err)
	}

	const runnerOne = manifest.ResourceID("runner-one")
	const runnerTwo = manifest.ResourceID("runner-two")

	consumerOne, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerOne)
	if err != nil {
		t.Fatalf("failed to ensure consumer one: %v", err)
	}
	consumerTwo, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerTwo)
	if err != nil {
		t.Fatalf("failed to ensure consumer two: %v", err)
	}

	publishJob(t, js, runnerOne, "result-for-one", 1)

	// Runner two must see nothing at all.
	batchTwo, err := consumerTwo.Fetch(10, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("fetch for runner two failed: %v", err)
	}
	var leaked int
	for range batchTwo.Messages() {
		leaked++
	}
	if leaked != 0 {
		t.Errorf("runner two received %d of runner one's jobs, want 0", leaked)
	}

	// And runner one must still have its job -- proving the check above passed
	// because of subject filtering, not because the job went missing entirely.
	batchOne, err := consumerOne.Fetch(10, jetstream.FetchMaxWait(5*time.Second))
	if err != nil {
		t.Fatalf("fetch for runner one failed: %v", err)
	}
	var delivered int
	for msg := range batchOne.Messages() {
		delivered++
		envelope, err := natsq.UnmarshalEnvelope(msg.Data())
		if err != nil {
			t.Fatalf("failed to decode envelope: %v", err)
		}
		if envelope.RunnerUID != runnerOne {
			t.Errorf("envelope runner = %q, want %q", envelope.RunnerUID, runnerOne)
		}
		if err := msg.Ack(); err != nil {
			t.Fatalf("failed to ack: %v", err)
		}
	}
	if delivered != 1 {
		t.Errorf("runner one received %d jobs, want 1", delivered)
	}
}

// TestBindRunnerConsumerRefusesMissing covers the permission boundary: a worker
// pointed at a runner the control plane never provisioned must fail, not
// quietly create the consumer for itself.
func TestBindRunnerConsumerRefusesMissing(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)
	cfg := testConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to ensure stream: %v", err)
	}

	_, err := natsq.BindRunnerConsumer(ctx, js, "never-provisioned")
	if err == nil {
		t.Fatal("binding a consumer that does not exist succeeded, want failure")
	}
}

func TestUnmarshalEnvelopeRejectsBadInput(t *testing.T) {
	tests := map[string]string{
		"not JSON":            "{{{",
		"unknown version":     `{"schemaVersion":99,"resultUid":"r","runnerUid":"n","dispatchId":"d"}`,
		"missing result UID":  `{"schemaVersion":1,"runnerUid":"n","dispatchId":"d"}`,
		"missing runner UID":  `{"schemaVersion":1,"resultUid":"r","dispatchId":"d"}`,
		"missing dispatch ID": `{"schemaVersion":1,"resultUid":"r","runnerUid":"n"}`,
	}

	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := natsq.UnmarshalEnvelope([]byte(payload)); err == nil {
				t.Errorf("UnmarshalEnvelope(%q) succeeded, want error", payload)
			}
		})
	}
}

func TestEnvelopeRoundTrip(t *testing.T) {
	want := natsq.DispatchEnvelope{
		SchemaVersion: natsq.DispatchEnvelopeVersion,
		ResultUID:     "result-uid",
		ResultVersion: 7,
		ScenarioName:  "some-scenario",
		RunnerUID:     "runner-uid",
		DispatchID:    "result-uid.7",
	}

	data, err := natsq.MarshalEnvelope(want)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	got, err := natsq.UnmarshalEnvelope(data)
	if err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip = %+v, want %+v", got, want)
	}
}

func TestRunnerUIDFromLogSubject(t *testing.T) {
	tests := []struct {
		subject string
		want    manifest.ResourceID
		ok      bool
	}{
		{natsq.LogSubject("runner-1", "result-1"), "runner-1", true},
		{"urth.v1.logs.runner-1", "", false},
		{"urth.v1.logs.runner-1.result-1.extra", "", false},
		{"urth.v1.jobs.runner-1.result-1", "", false},
		{"urth.v1.logs..result-1", "", false},
		{"", "", false},
	}

	for _, test := range tests {
		got, ok := natsq.RunnerUIDFromLogSubject(test.subject)
		if ok != test.ok || got != test.want {
			t.Errorf("RunnerUIDFromLogSubject(%q) = (%q, %v), want (%q, %v)",
				test.subject, got, ok, test.want, test.ok)
		}
	}
}

// A run's log tail is subscribed with a wildcard across runners, so the
// subscriber must check who published each line. Without that check any worker
// able to publish could inject output into another runner's run log, and an
// operator reading it would have no way to tell.
func TestSubscribeRunLogIgnoresOtherRunners(t *testing.T) {
	conn := startNATS(t)

	const executor = manifest.ResourceID("runner-executor")
	const impostor = manifest.ResourceID("runner-impostor")
	const resultUID = manifest.ResourceID("result-42")

	subscriber, err := natsq.SubscribeRunLog(conn, executor, resultUID, 16)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	t.Cleanup(func() { subscriber.Close() })

	// Publish as the impostor first, then as the genuine executor. If the
	// impostor's line were accepted it would be the one read below.
	natsq.NewLogPublisher(conn, impostor, resultUID).PublishLine([]byte("forged line"))
	natsq.NewLogPublisher(conn, executor, resultUID).PublishLine([]byte("genuine line"))

	select {
	case line := <-subscriber.Lines():
		if string(line) != "genuine line" {
			t.Errorf("received %q, want %q", line, "genuine line")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a log line")
	}

	// And nothing further should arrive.
	select {
	case line := <-subscriber.Lines():
		t.Errorf("unexpected extra line %q", line)
	case <-time.After(500 * time.Millisecond):
	}
}

// A run's log must not leak into another run's tail.
func TestSubscribeRunLogIsScopedToOneResult(t *testing.T) {
	conn := startNATS(t)

	const runnerUID = manifest.ResourceID("runner-1")

	subscriber, err := natsq.SubscribeRunLog(conn, runnerUID, "result-a", 16)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	t.Cleanup(func() { subscriber.Close() })

	natsq.NewLogPublisher(conn, runnerUID, "result-b").PublishLine([]byte("other run"))
	natsq.NewLogPublisher(conn, runnerUID, "result-a").PublishLine([]byte("this run"))

	select {
	case line := <-subscriber.Lines():
		if string(line) != "this run" {
			t.Errorf("received %q, want %q", line, "this run")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a log line")
	}
}

// A viewer that stops reading must not stall the run producing the output.
func TestSubscribeRunLogDropsWhenReaderIsBehind(t *testing.T) {
	conn := startNATS(t)

	const runnerUID = manifest.ResourceID("runner-slow")
	const resultUID = manifest.ResourceID("result-slow")

	subscriber, err := natsq.SubscribeRunLog(conn, runnerUID, resultUID, 2)
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}
	t.Cleanup(func() { subscriber.Close() })

	publisher := natsq.NewLogPublisher(conn, runnerUID, resultUID)
	for i := 0; i < 100; i++ {
		publisher.PublishLine([]byte("line"))
	}

	// The point is that publishing returned rather than blocking; drain
	// whatever the small buffer held.
	if err := conn.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	var got int
	for {
		select {
		case <-subscriber.Lines():
			got++
			continue
		case <-time.After(500 * time.Millisecond):
		}
		break
	}

	if got == 0 {
		t.Error("expected at least one buffered line")
	}
	if got > 16 {
		t.Errorf("buffer of 2 yielded %d lines; it is not bounding anything", got)
	}
}
