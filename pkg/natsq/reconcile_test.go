package natsq_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const reconcileRunnerUID = manifest.ResourceID("runner-reconcile")

// newReconcilableTransport gives a test a transport over a live JetStream, plus
// the raw handle used to break things behind its back.
func newReconcilableTransport(t *testing.T) (natsq.Transport, jetstream.JetStream, string) {
	t.Helper()

	server := newRestartableNATS(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	transport, err := natsq.NewScheduler(ctx, outboxTestConfig(server.url()))
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	t.Cleanup(func() { _ = transport.Close() })

	conn, err := outboxTestConfig(server.url()).Connect("test-admin")
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	t.Cleanup(conn.Close)

	return transport, mustJetStream(t, conn), server.url()
}

// A runner whose durable consumer has been deleted still has jobs published to
// its subject and nothing bound to collect them, so its probes stop with no
// error anywhere. Restoring it is the control plane's job by design: ADR 0004
// gives workers no JetStream administration rights precisely so a worker cannot
// paper over this by creating an overlapping consumer of its own.
func TestEnsureRunnerChannelRestoresADeletedConsumer(t *testing.T) {
	transport, js, _ := newReconcilableTransport(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Registration provisions the runner's queue.
	if _, err := transport.ConnectionInfoFor(ctx, reconcileRunnerUID); err != nil {
		t.Fatalf("failed to provision the runner channel: %v", err)
	}

	// A healthy queue is not reported as repaired, or the one number worth
	// paging on becomes noise.
	restored, err := transport.EnsureRunnerChannel(ctx, reconcileRunnerUID)
	if err != nil {
		t.Fatalf("failed to check the runner channel: %v", err)
	}
	if restored {
		t.Fatal("an existing consumer was reported as restored")
	}

	// An operator clears up, or a restore predates the runner.
	if err := js.DeleteConsumer(ctx, natsq.JobsStreamName, natsq.RunnerConsumerName(reconcileRunnerUID)); err != nil {
		t.Fatalf("failed to delete the consumer: %v", err)
	}
	if _, err := natsq.BindRunnerConsumer(ctx, js, reconcileRunnerUID); !errors.Is(err, natsq.ErrNoConsumer) {
		t.Fatalf("consumer lookup after deletion reported %v, want ErrNoConsumer", err)
	}

	restored, err = transport.EnsureRunnerChannel(ctx, reconcileRunnerUID)
	if err != nil {
		t.Fatalf("failed to restore the runner channel: %v", err)
	}
	if !restored {
		t.Fatal("a deleted consumer was not reported as restored")
	}

	if _, err := natsq.BindRunnerConsumer(ctx, js, reconcileRunnerUID); err != nil {
		t.Fatalf("the restored consumer could not be bound: %v", err)
	}
}

// A dispatch for a Result that finished without it leaves a message in the
// runner's bounded share of the stream, where it blocks live work if the runner
// is busy and sits until MaxJobAge if it is not. Withdrawing it must take that
// message and no other -- purging the runner's subject would discard every job
// queued behind it.
func TestDropDispatchRemovesOnlyTheStaleMessage(t *testing.T) {
	transport, _, url := newReconcilableTransport(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stale, err := transport.PublishDispatch(ctx, dispatchEntry("result-stale.1", "result-stale"))
	if err != nil {
		t.Fatalf("failed to publish the stale dispatch: %v", err)
	}
	if stale.Sequence == 0 {
		t.Fatal("a published dispatch must come back addressable, or it can never be withdrawn")
	}

	live, err := transport.PublishDispatch(ctx, dispatchEntry("result-live.1", "result-live"))
	if err != nil {
		t.Fatalf("failed to publish the live dispatch: %v", err)
	}

	if msgs := queuedMessages(t, url); msgs != 2 {
		t.Fatalf("stream holds %d messages, want 2", msgs)
	}

	entry := dispatchEntry("result-stale.1", "result-stale")
	published := time.Now()
	entry.PublishedAt = &published
	entry.PublishedSeq = stale.Sequence

	if err := transport.DropDispatch(ctx, entry); err != nil {
		t.Fatalf("failed to withdraw the stale dispatch: %v", err)
	}

	if msgs := queuedMessages(t, url); msgs != 1 {
		t.Fatalf("stream holds %d messages after the withdrawal, want 1", msgs)
	}

	// And the one left is the live job, not whichever survived.
	consumer, err := natsq.EnsureRunnerConsumer(ctx, mustJetStreamFor(t, url), outboxTestConfig(url), testRunnerUID)
	if err != nil {
		t.Fatalf("failed to bind the runner consumer: %v", err)
	}

	msg, err := consumer.Next(jetstream.FetchMaxWait(5 * time.Second))
	if err != nil {
		t.Fatalf("failed to fetch the surviving job: %v", err)
	}

	envelope, err := natsq.UnmarshalEnvelope(msg.Data())
	if err != nil {
		t.Fatalf("failed to decode the surviving job: %v", err)
	}
	if envelope.DispatchID != "result-live.1" {
		t.Fatalf("the surviving job is %q, want the live one", envelope.DispatchID)
	}
	if live.Sequence == stale.Sequence {
		t.Fatal("two publications shared a sequence; the withdrawal was not addressing one message")
	}
}

// Reaching the desired state by another route is not a failure. A worker that
// claimed and acknowledged the message got there first, and treating that as an
// error would have the reconciler retry the same entry on every scan forever.
func TestDropDispatchIsQuietWhenTheMessageIsAlreadyGone(t *testing.T) {
	transport, _, _ := newReconcilableTransport(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	published := time.Now()
	entry := dispatchEntry("result-gone.1", "result-gone")
	entry.PublishedAt = &published
	entry.PublishedSeq = 9999

	if err := transport.DropDispatch(ctx, entry); err != nil {
		t.Fatalf("withdrawing an already-gone message reported %v, want no error", err)
	}

	// An entry that was never published has no address and nothing to withdraw.
	if err := transport.DropDispatch(ctx, dispatchEntry("result-unpublished.1", "result-unpublished")); err != nil {
		t.Fatalf("withdrawing an unpublished dispatch reported %v, want no error", err)
	}
}

func dispatchEntry(eventUID string, resultUID manifest.ResourceID) urth.DispatchOutboxEntry {
	return urth.DispatchOutboxEntry{
		SchemaVersion: urth.DispatchOutboxEntryVersion,
		EventUID:      eventUID,
		ResultUID:     resultUID,
		ResultVersion: 1,
		ScenarioName:  "test-scenario",
		RunnerUID:     testRunnerUID,
	}
}

func mustJetStreamFor(t *testing.T, url string) jetstream.JetStream {
	t.Helper()

	conn, err := natsq.ClientConfig{URL: url}.Connect("test-consumer")
	if err != nil {
		t.Fatalf("failed to connect to NATS: %v", err)
	}
	t.Cleanup(conn.Close)

	return mustJetStream(t, conn)
}
