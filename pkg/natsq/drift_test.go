package natsq_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/natsq"
)

// What happens when the stream that exists is not the stream Urth wants.
//
// Two different situations hide behind "the config differs", and they want
// opposite handling. A limit that has been raised or lowered is applied in
// place, which is the whole point of provisioning on every start. A retention or
// storage policy that differs cannot be applied at all -- JetStream refuses --
// and the only way to "fix" it is to delete the stream, which throws away every
// queued job. That is a decision for an operator, not for a process starting up.

func TestEnsureJobStreamAppliesChangedLimits(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := testConfig()
	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	// An operator raises the fleet's bound and restarts.
	cfg.MaxJobs = 128
	cfg.MaxJobsPerRunner = 32

	stream, err := natsq.EnsureJobStream(ctx, js, cfg)
	if err != nil {
		t.Fatalf("a limit change must be applied in place: %v", err)
	}

	info, err := stream.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read stream info: %v", err)
	}
	if info.Config.MaxMsgs != 128 || info.Config.MaxMsgsPerSubject != 32 {
		t.Errorf("stream limits are %d/%d, want 128/32", info.Config.MaxMsgs, info.Config.MaxMsgsPerSubject)
	}
}

// A stream whose retention JetStream cannot change is reported, not recreated.
//
// Reached in practice by an older Urth, a hand-made stream, or a restore from a
// backup taken under different settings. Deleting it would take the queued jobs
// with it, so this refuses and says what to do.
func TestEnsureJobStreamRefusesIncompatibleRetention(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Somebody else's stream, on the name Urth uses.
	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:      natsq.JobsStreamName,
		Subjects:  []string{natsq.JobsSubjectWildcard},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		t.Fatalf("failed to create the pre-existing stream: %v", err)
	}

	_, err := natsq.EnsureJobStream(ctx, js, testConfig())
	if err == nil {
		t.Fatal("an incompatible stream must be reported, not silently accepted")
	}
	if !errors.Is(err, natsq.ErrIncompatibleStream) {
		t.Errorf("error should be ErrIncompatibleStream, got: %v", err)
	}

	message := err.Error()
	if !strings.Contains(message, natsq.JobsStreamName) {
		t.Errorf("error should name the stream, got: %v", message)
	}
	// The difference, in both directions, and what it costs to resolve.
	for _, want := range []string{"retention", "limits", "workqueue", "discards every queued job"} {
		if !strings.Contains(strings.ToLower(message), want) {
			t.Errorf("error should mention %q so an operator can act on it, got: %v", want, message)
		}
	}

	// And the queued work is still there: refusing must not be a euphemism for
	// deleting.
	if _, err := js.Stream(ctx, natsq.JobsStreamName); err != nil {
		t.Errorf("the existing stream must be left alone, got: %v", err)
	}
}

// The same for storage: a memory-backed stream loses every queued job on a
// broker restart, and switching it to file storage is not an in-place change.
func TestEnsureJobStreamRefusesIncompatibleStorage(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name:      natsq.JobsStreamName,
		Subjects:  []string{natsq.JobsSubjectWildcard},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.MemoryStorage,
	}); err != nil {
		t.Fatalf("failed to create the pre-existing stream: %v", err)
	}

	_, err := natsq.EnsureJobStream(ctx, js, testConfig())
	if !errors.Is(err, natsq.ErrIncompatibleStream) {
		t.Fatalf("a storage change must be refused, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "storage") {
		t.Errorf("error should name the storage difference, got: %v", err)
	}
}

// A consumer is repaired in place, never refused.
//
// Unlike the stream, nothing on a consumer of this stream is beyond JetStream's
// reach: a work-queue stream refuses any acknowledgement policy but explicit, so
// that difference cannot arise, and the subject filter -- the one a deployment
// that keyed consumers on runner *names* would have wrong -- is updated without
// disturbing the messages already queued. This pins that, because the tempting
// symmetry with the stream would turn the repair into a refusal.
func TestEnsureRunnerConsumerRepairsTheSubjectFilter(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := testConfig()
	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	const runnerUID = "runner-1"

	// A consumer under the name this runner uses, bound to the wrong subject.
	if _, err := js.CreateConsumer(ctx, natsq.JobsStreamName, jetstream.ConsumerConfig{
		Durable:       natsq.RunnerConsumerName(runnerUID),
		FilterSubject: natsq.JobSubject("some-other-runner"),
		AckPolicy:     jetstream.AckExplicitPolicy,
	}); err != nil {
		t.Fatalf("failed to create the pre-existing consumer: %v", err)
	}

	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID)
	if err != nil {
		t.Fatalf("a consumer with the wrong filter must be repaired, not refused: %v", err)
	}

	info, err := consumer.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read consumer info: %v", err)
	}
	if want := natsq.JobSubject(runnerUID); info.Config.FilterSubject != want {
		t.Errorf("consumer filter is %q, want %q", info.Config.FilterSubject, want)
	}
	if info.Config.MaxAckPending != cfg.MaxAckPending {
		t.Errorf("the repair should also apply the configured limits, MaxAckPending is %d, want %d",
			info.Config.MaxAckPending, cfg.MaxAckPending)
	}
}

// Provisioning an unchanged stream is a no-op that reports no drift, so a
// restart does not fill the log with differences that are not there.
func TestEnsureJobStreamReportsNoDriftWhenUnchanged(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := testConfig()
	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	existing, err := js.Stream(ctx, natsq.JobsStreamName)
	if err != nil {
		t.Fatalf("failed to look up stream: %v", err)
	}
	info, err := existing.Info(ctx)
	if err != nil {
		t.Fatalf("failed to read stream info: %v", err)
	}

	drift := natsq.StreamDrift(info.Config, cfg)
	if len(drift) != 0 {
		t.Errorf("a freshly provisioned stream should show no drift, got: %v", drift)
	}
}
