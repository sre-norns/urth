package natsq_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// What the stream does when it runs out of room.
//
// ADR 0004 §11 requires that reaching a limit fails visibly rather than losing
// work: `DiscardNew` refuses the publication and leaves the outbox row pending,
// where `DiscardOld` -- JetStream's default -- would evict the oldest unclaimed
// job to admit a new one and report success to both publishers.

// publishRaw publishes one job and returns the error, rather than failing the
// test: these tests are about the errors.
func publishRaw(ctx context.Context, js jetstream.JetStream, runnerUID manifest.ResourceID, dispatchID string, payload []byte) error {
	_, err := js.Publish(ctx, natsq.JobSubject(runnerUID), payload, jetstream.WithMsgID(dispatchID))

	return err
}

// envelopeOfSize builds a valid envelope padded to roughly the requested size,
// so a test can exercise the message-size bound without inventing a payload the
// decoder would reject for another reason.
func envelopeOfSize(t *testing.T, runnerUID manifest.ResourceID, dispatchID string, size int) []byte {
	t.Helper()

	data, err := natsq.MarshalEnvelope(natsq.DispatchEnvelope{
		SchemaVersion: natsq.DispatchEnvelopeVersion,
		ResultUID:     "result-1",
		ResultVersion: 1,
		// The scenario name is the only free-length field in the envelope, which
		// makes it the honest way to produce an oversized one.
		ScenarioName: manifest.ResourceName(strings.Repeat("x", size)),
		RunnerUID:    runnerUID,
		DispatchID:   dispatchID,
	})
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}

	return data
}

// One runner filling its share must not cost another runner its queued work.
//
// This is what DiscardNewPerSubject buys. Without it the stream's global
// discard policy applies to the whole stream, and a runner whose workers are
// all offline evicts -- or blocks -- everybody else.
func TestPerRunnerLimitDoesNotEvictAnotherRunner(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	cfg := testConfig()
	cfg.MaxJobsPerRunner = 2
	cfg.MaxJobs = 16

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	const busy = manifest.ResourceID("runner-busy")
	const quiet = manifest.ResourceID("runner-quiet")

	// The quiet runner's one job, queued first and never claimed.
	quietID := publishJob(t, js, quiet, "result-quiet", 1)

	// The busy runner fills its share, and then some.
	for i := range 2 {
		publishJob(t, js, busy, manifest.ResourceID("result-busy-"+itoa(i)), 1)
	}

	err := publishRaw(ctx, js, busy, "result-busy-overflow",
		envelopeOfSize(t, busy, "result-busy-overflow", 8))
	if err == nil {
		t.Fatal("publishing past the per-runner limit must be refused, not accepted")
	}

	// The refusal is the point, but so is what it did not do.
	stream, err := js.Stream(ctx, natsq.JobsStreamName)
	if err != nil {
		t.Fatalf("failed to look up stream: %v", err)
	}

	msg, err := stream.GetLastMsgForSubject(ctx, natsq.JobSubject(quiet))
	if err != nil {
		t.Fatalf("the quiet runner's job should still be queued: %v", err)
	}
	if got := msg.Header.Get(jetstream.MsgIDHeader); got != quietID {
		t.Errorf("queued job is %q, want the one published first (%q)", got, quietID)
	}
}

// The global limit is the one that bounds the disk. Reaching it must refuse the
// publication -- leaving the outbox row unpublished and retryable -- rather than
// making room by dropping work somebody is waiting for.
//
// Two runners, each inside its own per-runner share, together exceed the stream:
// that is the case the per-subject limit cannot catch and the global one exists
// for, and it is what a growing fleet does.
func TestGlobalLimitRefusesPublicationAndKeepsQueuedWork(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	cfg := testConfig()
	cfg.MaxJobs = 3
	cfg.MaxJobsPerRunner = 2

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-1")
	const otherUID = manifest.ResourceID("runner-2")

	for i := range 2 {
		publishJob(t, js, runnerUID, manifest.ResourceID("result-"+itoa(i)), 1)
	}
	publishJob(t, js, otherUID, "result-other", 1)

	// Within runner-2's own share, and one past the stream's.
	err := publishRaw(ctx, js, otherUID, "result-overflow",
		envelopeOfSize(t, otherUID, "result-overflow", 8))
	if err == nil {
		t.Fatal("publishing past the global limit must be refused")
	}

	stream, err := js.Stream(ctx, natsq.JobsStreamName)
	if err != nil {
		t.Fatalf("failed to look up stream: %v", err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read stream info: %v", err)
	}
	if info.State.Msgs != 3 {
		t.Errorf("stream holds %d messages, want the 3 already accepted -- nothing evicted", info.State.Msgs)
	}
}

// An envelope larger than the configured bound is refused, visibly.
//
// The dispatch envelope carries identity and nothing else -- the probe
// definition is disclosed only in the claim response, by ADR 0004 -- so a
// message anywhere near this size means something is riding on the queue that
// should not be. Storing it quietly would hide that.
func TestOversizedEnvelopeIsRefused(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	cfg := testConfig()
	cfg.MaxMsgSize = 512

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-1")

	err := publishRaw(ctx, js, runnerUID, "result-huge",
		envelopeOfSize(t, runnerUID, "result-huge", 4096))
	if err == nil {
		t.Fatal("an oversized message must be refused")
	}

	// A normal envelope still goes through, so the bound is not simply too small
	// for the traffic it is meant to admit.
	if err := publishRaw(ctx, js, runnerUID, "result-normal",
		envelopeOfSize(t, runnerUID, "result-normal", 8)); err != nil {
		t.Fatalf("an ordinary dispatch must still be accepted: %v", err)
	}
}

// A runner's workers cannot reserve more work than MaxAckPending, however
// eagerly they pull.
//
// The reservation is a claim handshake, not a probe run: a worker acks as soon
// as the claim commits. Unbounded, a pool of workers pulls in far more than it
// can claim inside AckWait, every reservation times out, and the redelivery
// storm that follows reads as a broker fault rather than a configuration one.
func TestConsumerCannotExceedMaxAckPending(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	cfg := testConfig()
	cfg.MaxAckPending = 2
	// Long enough that nothing is redelivered while the test holds messages.
	cfg.AckWait = 30 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-1")

	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID)
	if err != nil {
		t.Fatalf("failed to provision consumer: %v", err)
	}

	for i := range 6 {
		publishJob(t, js, runnerUID, manifest.ResourceID("result-"+itoa(i)), 1)
	}

	// Several workers pulling at once, none of them acknowledging: exactly the
	// shape of a pool that is slow to claim.
	var (
		mu       sync.Mutex
		received int
		wg       sync.WaitGroup
	)

	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			batch, err := consumer.Fetch(6, jetstream.FetchMaxWait(2*time.Second))
			if err != nil {
				return
			}
			for range batch.Messages() {
				mu.Lock()
				received++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	got := received
	mu.Unlock()

	if got > cfg.MaxAckPending {
		t.Errorf("workers held %d unacknowledged jobs, want at most %d", got, cfg.MaxAckPending)
	}
	if got == 0 {
		t.Error("workers received nothing; the bound should limit delivery, not stop it")
	}

	info, err := consumer.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read consumer info: %v", err)
	}
	if info.Config.MaxAckPending != cfg.MaxAckPending {
		t.Errorf("consumer MaxAckPending is %d, want %d", info.Config.MaxAckPending, cfg.MaxAckPending)
	}
}

// Every bound this package sets is set explicitly, so that a JetStream default
// changing underneath does not quietly change Urth's behaviour.
func TestStreamAndConsumerCarryEveryConfiguredLimit(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	cfg := testConfig()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	stream, err := natsq.EnsureJobStream(ctx, js, cfg)
	if err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read stream info: %v", err)
	}

	got := info.Config
	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"MaxMsgs", got.MaxMsgs, cfg.MaxJobs},
		{"MaxBytes", got.MaxBytes, cfg.MaxBytes},
		{"MaxMsgsPerSubject", got.MaxMsgsPerSubject, cfg.MaxJobsPerRunner},
		{"MaxMsgSize", got.MaxMsgSize, cfg.MaxMsgSize},
		{"MaxAge", got.MaxAge, cfg.MaxJobAge},
		{"Duplicates", got.Duplicates, cfg.DuplicateWindow},
		{"Replicas", got.Replicas, cfg.Replicas},
		{"Discard", got.Discard, jetstream.DiscardNew},
		{"DiscardNewPerSubject", got.DiscardNewPerSubject, true},
		{"Retention", got.Retention, jetstream.WorkQueuePolicy},
		{"Storage", got.Storage, jetstream.FileStorage},
	}
	for _, check := range checks {
		if check.got != check.want {
			t.Errorf("stream %s is %v, want %v", check.field, check.got, check.want)
		}
	}

	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, "runner-1")
	if err != nil {
		t.Fatalf("failed to provision consumer: %v", err)
	}

	consumerInfo, err := consumer.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read consumer info: %v", err)
	}

	consumerChecks := []struct {
		field string
		got   any
		want  any
	}{
		{"AckWait", consumerInfo.Config.AckWait, cfg.AckWait},
		{"MaxDeliver", consumerInfo.Config.MaxDeliver, cfg.MaxDeliver},
		{"MaxAckPending", consumerInfo.Config.MaxAckPending, cfg.MaxAckPending},
		{"AckPolicy", consumerInfo.Config.AckPolicy, jetstream.AckExplicitPolicy},
		// A consumer that expires takes a runner's queued work with it.
		{"InactiveThreshold", consumerInfo.Config.InactiveThreshold, time.Duration(0)},
	}
	for _, check := range consumerChecks {
		if check.got != check.want {
			t.Errorf("consumer %s is %v, want %v", check.field, check.got, check.want)
		}
	}
}
