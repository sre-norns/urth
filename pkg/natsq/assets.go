package natsq

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrIncompatibleStream reports an existing stream whose configuration JetStream
// cannot bring in line with Urth's, so that a caller can tell it apart from a
// broker that is merely unreachable.
var ErrIncompatibleStream = errors.New("existing stream cannot be reconciled in place")

// Drift is one difference between an asset as it exists and as Urth wants it.
//
// Reported rather than silently corrected, because the two kinds of difference
// need opposite handling and an operator has to be able to tell them apart: a
// limit that has changed is applied on the next start, while a policy JetStream
// cannot change in place can only be "fixed" by deleting the asset and the
// queued work inside it.
type Drift struct {
	// Field is named as an operator would look for it, not as the JetStream API
	// spells it.
	Field string
	Have  string
	Want  string
}

func (d Drift) String() string {
	return fmt.Sprintf("%s is %s, want %s", d.Field, d.Have, d.Want)
}

// describeDrift renders a set of differences for a log line or an error.
func describeDrift(drift []Drift) string {
	parts := make([]string, 0, len(drift))
	for _, d := range drift {
		parts = append(parts, d.String())
	}

	return strings.Join(parts, "; ")
}

// StreamDrift lists how the existing stream differs from the configured one.
//
// Exported so an operator surface can ask the question without provisioning
// anything; EnsureJobStream uses it to decide between applying and refusing.
func StreamDrift(have jetstream.StreamConfig, cfg Config) []Drift {
	want := jobStreamConfig(cfg)

	var drift []Drift
	add := func(field string, haveValue, wantValue any) {
		if haveValue != wantValue {
			drift = append(drift, Drift{Field: field, Have: fmt.Sprint(haveValue), Want: fmt.Sprint(wantValue)})
		}
	}

	// The two JetStream will not change in place. Kept first so they read first
	// in a message that lists several.
	add("retention policy", have.Retention, want.Retention)
	add("storage", have.Storage, want.Storage)

	add("replicas", have.Replicas, want.Replicas)
	add("max messages", have.MaxMsgs, want.MaxMsgs)
	add("max bytes", have.MaxBytes, want.MaxBytes)
	add("max messages per runner", have.MaxMsgsPerSubject, want.MaxMsgsPerSubject)
	add("max message size", have.MaxMsgSize, want.MaxMsgSize)
	add("max age", have.MaxAge, want.MaxAge)
	add("duplicate window", have.Duplicates, want.Duplicates)
	add("discard policy", have.Discard, want.Discard)
	add("per-subject discard", have.DiscardNewPerSubject, want.DiscardNewPerSubject)

	return drift
}

// immutableStreamDrift returns the differences JetStream cannot apply to an
// existing stream.
//
// Deliberately short. Guessing at the broker's whole rulebook would go stale;
// these two are the ones it refuses outright, and the ones whose "fix" destroys
// data, which is what makes them worth naming here rather than letting the
// update fail with the server's own wording.
func immutableStreamDrift(drift []Drift) []Drift {
	var blocking []Drift
	for _, d := range drift {
		switch d.Field {
		case "retention policy", "storage":
			blocking = append(blocking, d)
		}
	}

	return blocking
}

// jobStreamConfig is the stream Urth wants, in one place, so that provisioning
// and drift detection cannot disagree about it.
func jobStreamConfig(cfg Config) jetstream.StreamConfig {
	return jetstream.StreamConfig{
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

		// Global bounds, alongside the per-subject one. The per-subject limit
		// stops a single offline runner from consuming the stream; it does not
		// stop a fleet of runners, each inside its own share, from adding up to
		// whatever the volume holds. Both are needed, and neither is sufficient.
		//
		// Every one of these would default to "unlimited" if left unset, which is
		// why Config.Validate refuses a zero rather than passing it through: an
		// unbounded stream is not a limit an operator chose.
		MaxMsgs:    cfg.MaxJobs,
		MaxBytes:   cfg.MaxBytes,
		MaxMsgSize: cfg.MaxMsgSize,

		MaxAge: cfg.MaxJobAge,

		// The outbox relay's duplicate suppression lives here. See
		// Config.DuplicateWindow.
		Duplicates: cfg.DuplicateWindow,
	}
}

// EnsureJobStream creates or updates the shared work-queue stream.
//
// Only the control plane calls this. The settings are chosen so that reaching a
// limit fails loudly at publication time rather than quietly dropping work an
// operator still believes is queued.
//
// An existing stream is inspected before it is updated. Provisioning on every
// start is how a limit change reaches the broker, but it is also how a stream
// that is not Urth's -- an older deployment's, a hand-made one, a restore from a
// backup taken under different settings -- would be quietly adopted. Differences
// JetStream can apply are applied and logged; the two it cannot are refused,
// because the only way to resolve them is to delete the stream and the queued
// jobs with it, and that is an operator's decision.
func EnsureJobStream(ctx context.Context, js jetstream.JetStream, cfg Config) (jetstream.Stream, error) {
	desired := jobStreamConfig(cfg)

	switch existing, err := js.Stream(ctx, JobsStreamName); {
	case errors.Is(err, jetstream.ErrStreamNotFound):
		// Nothing to reconcile: the first start of a deployment.
	case err != nil:
		return nil, fmt.Errorf("failed to inspect stream %q: %w", JobsStreamName, err)
	default:
		info, err := existing.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to read the configuration of stream %q: %w", JobsStreamName, err)
		}

		drift := StreamDrift(info.Config, cfg)
		if blocking := immutableStreamDrift(drift); len(blocking) > 0 {
			return nil, fmt.Errorf(
				"%w: stream %q has %s. JetStream cannot change these in place; resolving it means deleting the stream, which discards every queued job. Do that deliberately, or point this deployment at a different stream",
				ErrIncompatibleStream, JobsStreamName, describeDrift(blocking))
		}

		if len(drift) > 0 {
			// Logged rather than silent: an operator restarting a server should
			// be able to see that this start changed the broker's state, and what.
			log.Printf("stream %q: applying configuration drift: %s", JobsStreamName, describeDrift(drift))
		}
	}

	stream, err := js.CreateOrUpdateStream(ctx, desired)
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
	// No pre-flight drift check here, unlike the stream, and not by oversight.
	// There is nothing on a consumer of *this* stream that JetStream refuses to
	// change in place: a work-queue stream will not accept a consumer with any
	// acknowledgement policy but explicit, so that difference cannot exist, and
	// every other field -- including the subject filter, which is what a consumer
	// left behind by a name-keyed deployment would have wrong -- is updated by
	// CreateOrUpdateConsumer without touching the messages queued under it.
	// Guarding against drift that cannot happen would only block the repair.
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

		// How many jobs this runner's workers may hold unacknowledged at once.
		// Unset, JetStream defaults it to a large number, and a pool of workers
		// pulling eagerly can reserve far more work than it can claim within
		// AckWait -- every one of those reservations then times out and is
		// redelivered, which looks like a broker fault and is a configuration
		// one. See Config.MaxAckPending.
		MaxAckPending: cfg.MaxAckPending,

		// InactiveThreshold is deliberately left unset. It deletes a consumer
		// that nobody has pulled from for a while, and this consumer *is* the
		// runner's queue: expiring it because a runner's workers are offline
		// would discard exactly the work they are meant to come back for. ADR
		// 0004 makes consumers control-plane assets, created and removed with the
		// runner they belong to.
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
