package natsq

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/urth"
)

// PublishDispatch implements urth.DispatchPublisher.
//
// This is the only place a job message is published. The API server no longer
// publishes at all: it commits an outbox entry alongside the Result, and the
// relay brings that entry here. What used to be one uncoordinated write pair is
// now a commit followed by an at-least-once delivery keyed on the entry's event
// UID.
func (s *scheduler) PublishDispatch(ctx context.Context, entry urth.DispatchOutboxEntry) (urth.DispatchReceipt, error) {
	if entry.RunnerUID == "" {
		// No subject to publish on. Retrying will not conjure a runner, and the
		// entry is not this transport's to fix -- the run was never placed.
		s.totalErrors.Add(1)
		return urth.DispatchReceipt{}, fmt.Errorf("%w: %w for result %v", urth.ErrPermanentDispatch, ErrNoRunner, entry.ResultUID)
	}

	envelope := DispatchEnvelope{
		SchemaVersion: DispatchEnvelopeVersion,
		ResultUID:     entry.ResultUID,
		ResultVersion: entry.ResultVersion,
		ScenarioName:  entry.ScenarioName,
		RunnerUID:     entry.RunnerUID,
		// The outbox's event UID *is* the dispatch ID. Both name the same thing
		// -- one attempt to run one version of one Result -- and keeping them
		// equal is what lets a worker's claim, JetStream's duplicate window, and
		// the outbox row all be matched up when diagnosing a stuck run.
		DispatchID: entry.EventUID,
	}

	data, err := MarshalEnvelope(envelope)
	if err != nil {
		s.totalErrors.Add(1)
		// An envelope this build cannot encode will not encode on the next
		// attempt either.
		return urth.DispatchReceipt{}, fmt.Errorf("%w: failed to encode dispatch %v: %w", urth.ErrPermanentDispatch, entry.EventUID, err)
	}

	// Publish synchronously and wait for the storage acknowledgement. Returning
	// before JetStream has persisted the message would let the relay mark the
	// entry published when it may never be delivered -- reintroducing, one layer
	// further down, exactly the lost-dispatch window the outbox closes.
	ack, err := s.js.Publish(ctx, JobSubject(entry.RunnerUID), data, jetstream.WithMsgID(entry.EventUID))
	if err != nil {
		s.totalErrors.Add(1)
		return urth.DispatchReceipt{}, fmt.Errorf("failed to publish dispatch %v: %w", entry.EventUID, err)
	}

	s.totalScheduled.Add(1)

	// The stream sequence is what makes this message addressable later. A
	// duplicate suppressed by the message-ID window comes back with the sequence
	// of the original, which is the answer the reconciler wants: the message that
	// is actually queued.
	return urth.DispatchReceipt{Sequence: ack.Sequence}, nil
}
