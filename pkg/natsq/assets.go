package natsq

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// EnsureJobStream creates or updates the shared work-queue stream.
//
// Only the control plane calls this. The settings are chosen so that reaching a
// limit fails loudly at publication time rather than quietly dropping work an
// operator still believes is queued.
func EnsureJobStream(ctx context.Context, js jetstream.JetStream, cfg Config) (jetstream.Stream, error) {
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     JobsStreamName,
		Subjects: []string{JobsSubjectWildcard},

		// WorkQueue: a job is delivered to exactly one worker and removed once
		// acknowledged. Note this is what makes runner subject filters need to
		// be disjoint -- a work-queue stream refuses overlapping consumers.
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
		Replicas:  cfg.Replicas,

		// DiscardNew, not the default DiscardOld. Evicting the oldest unclaimed
		// job to make room for a new one loses work silently; refusing the
		// publication surfaces the problem to the caller, which can mark the
		// Result errored and alert.
		Discard:              jetstream.DiscardNew,
		DiscardNewPerSubject: true,
		MaxMsgsPerSubject:    cfg.MaxJobsPerRunner,

		MaxAge: cfg.MaxJobAge,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to provision stream %q: %w", JobsStreamName, err)
	}

	return stream, nil
}

// EnsureRunnerConsumer creates or updates the durable pull consumer for one
// runner. Every worker registered to that runner binds to this one consumer, so
// a job goes to exactly one of them.
//
// Idempotent: called both when a runner is created and again at dispatch time,
// so that a runner created before this code shipped -- or one whose consumer an
// operator removed -- still gets a queue rather than silently dropping jobs.
func EnsureRunnerConsumer(ctx context.Context, js jetstream.JetStream, cfg Config, runnerUID manifest.ResourceID) (jetstream.Consumer, error) {
	consumer, err := js.CreateOrUpdateConsumer(ctx, JobsStreamName, jetstream.ConsumerConfig{
		Durable: RunnerConsumerName(runnerUID),

		// Exactly one subject. A wildcard here would overlap other runners'
		// subjects, which a work-queue stream rejects outright -- and if it did
		// not, would let one runner's workers drain another's queue.
		FilterSubject: JobSubject(runnerUID),

		AckPolicy: jetstream.AckExplicitPolicy,

		// Covers the claim handshake only. See Config.AckWait.
		AckWait:    cfg.AckWait,
		MaxDeliver: cfg.MaxDeliver,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to provision consumer for runner %q: %w", runnerUID, err)
	}

	return consumer, nil
}

// BindRunnerConsumer looks up a runner's existing durable consumer.
//
// This is the worker's entry point, and it deliberately cannot create anything.
// A missing consumer means the worker has been pointed at a runner the control
// plane has not provisioned, which is a configuration error worth failing on
// rather than papering over.
func BindRunnerConsumer(ctx context.Context, js jetstream.JetStream, runnerUID manifest.ResourceID) (jetstream.Consumer, error) {
	consumer, err := js.Consumer(ctx, JobsStreamName, RunnerConsumerName(runnerUID))
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		return nil, fmt.Errorf("%w: runner %q", ErrNoConsumer, runnerUID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to bind consumer for runner %q: %w", runnerUID, err)
	}

	return consumer, nil
}
