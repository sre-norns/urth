package natsq

import (
	"context"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// EnsureRunnerChannel implements urth.RunnerChannelReconciler.
//
// Binding first, then creating, so the return value means something. Calling
// CreateOrUpdateConsumer unconditionally would work just as well and report
// every runner as "restored" on every scan, which turns the one number an
// operator would page on -- a queue that had to be rebuilt -- into noise.
func (s *scheduler) EnsureRunnerChannel(ctx context.Context, runnerUID manifest.ResourceID) (bool, error) {
	if _, err := BindRunnerConsumer(ctx, s.js, runnerUID); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNoConsumer) {
		return false, err
	}

	// The consumer is genuinely absent: deleted by an operator clearing up, or
	// missing after a restore from a backup taken before the runner existed. Its
	// workers cannot fix this -- ADR 0004 gives them no administration rights, on
	// purpose -- so if the control plane does not rebuild it, the runner accepts
	// dispatches and delivers none of them.
	if _, err := EnsureRunnerConsumer(ctx, s.js, s.cfg, runnerUID); err != nil {
		return false, err
	}

	return true, nil
}

// DropDispatch implements urth.RunnerChannelReconciler.
//
// The message is deleted by its exact stream sequence. Purging by subject was
// never an option: a runner's subject carries every one of its queued jobs, and
// withdrawing one stale dispatch is not a reason to discard the live work behind
// it.
func (s *scheduler) DropDispatch(ctx context.Context, entry urth.DispatchOutboxEntry) error {
	if entry.PublishedSeq == 0 {
		// Never published, or published by a transport that does not address its
		// messages. Either way there is nothing here to withdraw.
		return nil
	}

	stream, err := s.js.Stream(ctx, JobsStreamName)
	if err != nil {
		return fmt.Errorf("failed to look up stream %q: %w", JobsStreamName, err)
	}

	err = stream.DeleteMsg(ctx, entry.PublishedSeq)
	if err == nil || errors.Is(err, jetstream.ErrMsgNotFound) {
		// Already gone: a worker claimed and acknowledged it, or it aged out.
		// Reaching the desired state by another route is not a failure, and
		// treating it as one would have the reconciler retry this entry forever.
		return nil
	}

	// JetStream does not report "there is nothing at this sequence" the same way
	// in every case -- a sequence past the end of the stream comes back as a
	// generic 500 rather than a not-found -- so the absence is confirmed by
	// asking rather than by matching on the shape of the error.
	if _, getErr := stream.GetMsg(ctx, entry.PublishedSeq); errors.Is(getErr, jetstream.ErrMsgNotFound) {
		return nil
	}

	return fmt.Errorf("failed to withdraw dispatch %v at sequence %d: %w", entry.EventUID, entry.PublishedSeq, err)
}
