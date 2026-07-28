package urth

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"gorm.io/gorm"
)

// DispatchAdvisory is a transport's report that it has given up delivering a
// message.
//
// Transport-neutral by design: the domain's interest is that some dispatch
// exhausted its delivery budget, addressed by the same sequence the outbox
// already records when the message was published. Nothing here names JetStream,
// which is what keeps pkg/urth free of a broker dependency.
type DispatchAdvisory struct {
	// StreamSequence addresses the message within the transport. It is matched
	// against DispatchOutboxEntry.PublishedSeq, which is the only durable link
	// between a broker message and the Result it was published for.
	StreamSequence uint64

	// Deliveries is how many attempts the transport made before giving up.
	Deliveries int

	// Channel names the transport-side queue, for the failure's detail text.
	Channel string

	// ObservedAt is when the advisory was seen.
	ObservedAt time.Time
}

// DispatchAdvisorySink records what an advisory means for authoritative state.
//
// Owned here and called by transport packages, the same direction as
// DispatchPublisher and RunnerChannelReconciler: the transport knows a message
// was abandoned and nothing about Results, while the domain knows what
// abandoning one means and nothing about streams.
type DispatchAdvisorySink interface {
	RecordMaxDelivery(ctx context.Context, advisory DispatchAdvisory) error
}

// advisoryRecorder turns advisories into dead-letter records.
type advisoryRecorder struct {
	db    *gorm.DB
	store *dbstore.DBStore
}

// NewAdvisoryRecorder builds the sink that records abandoned dispatches.
func NewAdvisoryRecorder(db *gorm.DB, store *dbstore.DBStore) DispatchAdvisorySink {
	return &advisoryRecorder{db: db, store: store}
}

// RecordMaxDelivery records a dispatch the transport has stopped redelivering.
//
// This is the one dead-letter category no worker can report. The other three are
// decisions a worker made while holding the message; this one is the broker
// deciding to stop handing it out, and the worker that failed to claim it has
// long since moved on. Without an advisory the message simply stops being
// delivered -- a work-queue stream does not remove it for reaching its delivery
// limit -- and the Result waits for a worker that will never be offered it.
func (r *advisoryRecorder) RecordMaxDelivery(ctx context.Context, advisory DispatchAdvisory) error {
	if advisory.StreamSequence == 0 {
		// Nothing to resolve. Reported rather than ignored: an advisory without
		// a sequence means this build and the broker disagree about the payload.
		return fmt.Errorf("%w: advisory carries no stream sequence", ErrInvalidDispatchFailure)
	}

	var entry DispatchOutboxEntry
	found := r.db.WithContext(ctx).
		Where("published_seq = ?", advisory.StreamSequence).
		// Ordered and limited rather than assumed unique: published_seq is not a
		// key, and a stream that has been recreated can reuse a sequence. The
		// most recent row is the one this advisory is about.
		Order("id DESC").
		Limit(1).
		Find(&entry)
	if found.Error != nil {
		return fmt.Errorf("failed to resolve dispatch at sequence %d: %w", advisory.StreamSequence, found.Error)
	}
	if found.RowsAffected == 0 {
		// A message Urth has no outbox row for. Most likely it belongs to a
		// deployment that shared this stream, or the row was pruned. Logged and
		// dropped: inventing a dead-letter record for a dispatch this control
		// plane never made would be worse than saying nothing.
		log.Printf("dispatch advisory for unknown message at sequence %d on %q; ignoring",
			advisory.StreamSequence, advisory.Channel)

		return nil
	}

	observedAt := advisory.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}

	spec := DispatchFailureSpec{
		Reason:        ReasonMaxDeliveryExhausted,
		EventUID:      entry.EventUID,
		DispatchID:    entry.EventUID,
		ResultUID:     entry.ResultUID,
		ResultVersion: entry.ResultVersion,
		ScenarioName:  entry.ScenarioName,
		RunnerUID:     entry.RunnerUID,
		ReportedBy:    ReporterControlPlane,
		Deliveries:    advisory.Deliveries,
		Detail: fmt.Sprintf("the broker stopped redelivering after %d attempts on %q",
			advisory.Deliveries, advisory.Channel),
		OccurredAt: observedAt,
	}

	var runner Runner
	if entry.RunnerUID != "" {
		if _, err := r.store.GetByUID(ctx, &runner, entry.RunnerUID); err != nil {
			// The runner's name is only a label. Losing it is not worth losing
			// the record.
			log.Printf("could not load runner %v while recording an advisory: %v", entry.RunnerUID, err)
		}
	}

	failure, created, err := recordDispatchFailure(ctx, r.store, spec, runner.Name)
	if err != nil {
		return err
	}

	if created {
		log.Printf("dispatch failure %q recorded from a broker advisory: result %v abandoned after %d deliveries",
			failure.Name, spec.ResultUID, advisory.Deliveries)
	}

	return nil
}
