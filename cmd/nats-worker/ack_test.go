package main

import (
	"context"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The acknowledgement half of the claim handshake, against a real broker.
//
// Embedded rather than mocked, for the reason pkg/natsq gives: the behaviour
// under test is JetStream's -- whether a confirmed acknowledgement actually
// removes a message from a work queue, and what a confirmation does when the
// server cannot answer -- and a mock would have agreed with every assumption
// made while writing confirmAck, including the wrong ones.

// TestHandshakeBudgetFitsAckWait locks the invariant the split exists for: claim
// plus acknowledgement must fit inside the window before redelivery. The
// prototype had no such invariant -- its claim timeout was a constant 30s and the
// default AckWait is also 30s -- so a slow claim could spend the entire window and
// leave the acknowledgement no time that still counted for anything.
func TestHandshakeBudgetFitsAckWait(t *testing.T) {
	cases := []struct {
		name    string
		ackWait time.Duration
	}{
		{"the shipped default", 30 * time.Second},
		{"a generous window", 5 * time.Minute},
		{"a window barely larger than the reserve", 6 * time.Second},
		{"a short window is split evenly", 4 * time.Second},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			claim, ack := handshakeBudget(tc.ackWait)

			if claim <= 0 || ack <= 0 {
				t.Fatalf("budget = (%v claim, %v ack); both halves must be usable", claim, ack)
			}
			if claim+ack > tc.ackWait {
				t.Fatalf("budget = (%v claim, %v ack) = %v, which overruns ack-wait %v",
					claim, ack, claim+ack, tc.ackWait)
			}
		})
	}
}

// TestHandshakeBudgetWithoutAckWait covers the consumer that will not say what its
// AckWait is. Guessing the shipped default is better than an unbounded claim: a
// claim with no deadline holds a run slot until the API answers, and the failure
// this whole path is written around is an API that does not.
func TestHandshakeBudgetWithoutAckWait(t *testing.T) {
	claim, ack := handshakeBudget(0)

	if claim+ack != defaultHandshakeBudget {
		t.Fatalf("budget = (%v, %v), want the halves to add up to %v", claim, ack, defaultHandshakeBudget)
	}
}

// TestConfirmAckRetriesWithinItsBudget proves a confirmation that keeps failing is
// retried and then gives up, rather than either trying once or spinning forever.
// A single attempt would make the confirmation useless against the momentary
// failure it exists to survive -- a reconnect in progress answers the second try.
func TestConfirmAckRetriesWithinItsBudget(t *testing.T) {
	msg := &fakeMsg{doubleAckErr: errors.New("no responders")}

	const budget = 800 * time.Millisecond

	started := time.Now()
	err := confirmAck(context.Background(), msg, budget, nil)
	took := time.Since(started)

	if err == nil {
		t.Fatal("confirmAck reported success although every attempt failed")
	}

	_, attempts := msg.acks()
	if attempts < 2 {
		t.Errorf("confirmation attempts = %d, want more than one within the budget", attempts)
	}
	if took > budget+time.Second {
		t.Errorf("confirmAck took %v, well past its %v budget", took, budget)
	}
}

// TestConfirmAckAcceptsAnAlreadyAcknowledgedMessage keeps a duplicate confirmation
// from burning the whole reserve re-learning something settled. The client refuses
// to acknowledge a message twice, and retrying that refusal would report an
// unconfirmed acknowledgement for a message that is demonstrably not coming back.
func TestConfirmAckAcceptsAnAlreadyAcknowledgedMessage(t *testing.T) {
	msg := &fakeMsg{doubleAckErr: jetstream.ErrMsgAlreadyAckd}

	if err := confirmAck(context.Background(), msg, time.Second, nil); err != nil {
		t.Fatalf("an already-acknowledged message should count as confirmed, got %v", err)
	}

	if _, attempts := msg.acks(); attempts != 1 {
		t.Errorf("confirmation attempts = %d, want 1", attempts)
	}
}

// ackTestServer is an in-process NATS server with JetStream, returned alongside
// a connection to it so a test can stop the server while the client lives on.
func ackTestServer(t *testing.T) (*natsserver.Server, *nats.Conn) {
	t.Helper()

	srv, err := natsserver.NewServer(&natsserver.Options{
		Host:      "127.0.0.1",
		Port:      -1, // ephemeral, so parallel packages do not collide
		JetStream: true,
		StoreDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("failed to start NATS server: %v", err)
	}

	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("NATS server did not become ready")
	}
	t.Cleanup(srv.Shutdown)

	conn, err := nats.Connect(srv.ClientURL(), nats.NoReconnect())
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	t.Cleanup(conn.Close)

	return srv, conn
}

// ackTestConfig mirrors pkg/natsq's test configuration: real limits, pulled down
// to sizes a test can reach, and valid by Config.Validate -- a test running
// against a combination the api-server would refuse to start with proves nothing.
func ackTestConfig() natsq.Config {
	return natsq.Config{
		Replicas:         1,
		MaxJobs:          64,
		MaxBytes:         1 << 20,
		MaxJobsPerRunner: 16,
		MaxMsgSize:       8 << 10,
		DuplicateWindow:  time.Minute,
		MaxJobAge:        time.Hour,
		AckWait:          2 * time.Second,
		MaxDeliver:       5,
		MaxAckPending:    8,
	}
}

// queuedJob provisions the runner's queue, publishes one dispatch, and returns
// the consumer along with the delivered message.
func queuedJob(t *testing.T, conn *nats.Conn, runnerUID manifest.ResourceID) (jetstream.Consumer, jetstream.Msg) {
	t.Helper()

	ctx := t.Context()
	cfg := ackTestConfig()

	js, err := jetstream.New(conn)
	if err != nil {
		t.Fatalf("failed to init JetStream: %v", err)
	}

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision the jobs stream: %v", err)
	}
	if _, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID); err != nil {
		t.Fatalf("failed to provision the runner consumer: %v", err)
	}

	envelope, err := natsq.MarshalEnvelope(natsq.DispatchEnvelope{
		SchemaVersion: natsq.DispatchEnvelopeVersion,
		ResultUID:     "run-1",
		ResultVersion: 1,
		ScenarioName:  "a-scenario",
		RunnerUID:     runnerUID,
		DispatchID:    "dispatch-1",
	})
	if err != nil {
		t.Fatalf("failed to encode the dispatch: %v", err)
	}

	if _, err := js.Publish(ctx, natsq.JobSubject(runnerUID), envelope); err != nil {
		t.Fatalf("failed to publish the dispatch: %v", err)
	}

	consumer, err := natsq.BindRunnerConsumer(ctx, js, runnerUID)
	if err != nil {
		t.Fatalf("failed to bind the runner consumer: %v", err)
	}

	return consumer, fetchOne(t, consumer)
}

// fetchOne pulls a single message, or nil if the queue had nothing to give.
func fetchOne(t *testing.T, consumer jetstream.Consumer) jetstream.Msg {
	t.Helper()

	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("failed to fetch: %v", err)
	}

	for msg := range batch.Messages() {
		return msg
	}

	if err := batch.Error(); err != nil {
		t.Fatalf("fetch ended with an error: %v", err)
	}

	return nil
}

// TestConfirmAckRemovesTheMessage is the property the whole change rests on: a
// confirmed acknowledgement means the broker has recorded it, so the dispatch is
// gone from the work queue and cannot be redelivered while the probe runs.
func TestConfirmAckRemovesTheMessage(t *testing.T) {
	_, conn := ackTestServer(t)
	consumer, msg := queuedJob(t, conn, "runner-1")

	if msg == nil {
		t.Fatal("the published dispatch was not delivered")
	}

	if err := confirmAck(t.Context(), msg, time.Second, nil); err != nil {
		t.Fatalf("confirmAck against a healthy broker: %v", err)
	}

	// Past AckWait, so a message that was merely delivered -- rather than
	// acknowledged -- would be back on offer by now.
	time.Sleep(ackTestConfig().AckWait + 500*time.Millisecond)

	if again := fetchOne(t, consumer); again != nil {
		t.Fatal("a confirmed acknowledgement left the dispatch redeliverable")
	}
}

// TestConfirmAckIsBoundedWhenTheBrokerIsGone proves the confirmation cannot hang
// a run slot. DoubleAck is a request/reply, and a broker that never answers would
// otherwise hold the handshake open indefinitely -- the very thing AckWait exists
// to prevent, reintroduced by the fix for it.
func TestConfirmAckIsBoundedWhenTheBrokerIsGone(t *testing.T) {
	srv, conn := ackTestServer(t)
	_, msg := queuedJob(t, conn, "runner-1")

	if msg == nil {
		t.Fatal("the published dispatch was not delivered")
	}

	srv.Shutdown()
	srv.WaitForShutdown()

	const budget = 750 * time.Millisecond

	started := time.Now()
	err := confirmAck(context.Background(), msg, budget, nil)
	took := time.Since(started)

	if err == nil {
		t.Fatal("confirmAck reported success against a broker that was not there")
	}

	// Generous, because this asserts boundedness rather than a duration: the
	// failure it guards against is unbounded, and a tight bound here would only
	// make the test flaky on a loaded machine.
	if took > budget+2*time.Second {
		t.Fatalf("confirmAck took %v, well past its %v budget", took, budget)
	}
}
