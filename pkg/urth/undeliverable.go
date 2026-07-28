package urth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
)

// PermanentDispatchSink settles a dispatch the relay will never publish.
//
// The relay knows a publication can never succeed; it does not know what that
// means for the run, and must not: deciding that a Result is over is authoritative
// state, and the transport-facing loop is the wrong place to own it. So the relay
// reports, and this settles -- the same split as DispatchAdvisorySink, and for the
// same reason.
//
// Without it a permanently undeliverable dispatch was retried hourly forever
// while its run stayed `pending`. Nothing else could repair that: the reconciler
// leaves an unpublished outbox row to the relay by design, because inferring a
// lost dispatch there would expire runs the relay is about to deliver.
type PermanentDispatchSink interface {
	RecordUndeliverable(ctx context.Context, entry DispatchOutboxEntry, cause error) error
}

// undeliverableRecorder is the store-backed PermanentDispatchSink.
type undeliverableRecorder struct {
	store dbstore.TransactionalStore
}

// NewUndeliverableRecorder builds the sink that settles undeliverable dispatches.
func NewUndeliverableRecorder(store dbstore.TransactionalStore) PermanentDispatchSink {
	return &undeliverableRecorder{store: store}
}

// RecordUndeliverable strands the run, and dead-letters the dispatch when it is
// a fault rather than an ordinary placement outcome.
//
// The split is the whole point of this type. A dispatch with no runner means the
// scenario's requirements matched nothing at the moment it was written -- a
// fleet being changed, not a fault -- and once the scheduler runs a scenario
// every minute, dead-lettering it would file a record per tick and bury the
// failures that do need a human. Everything else here is a genuine exception:
// an envelope this build cannot encode, a row from a schema it does not know, a
// Result that has moved on. Those go to the dead-letter queue, which is where an
// operator looks for them.
func (r *undeliverableRecorder) RecordUndeliverable(ctx context.Context, entry DispatchOutboxEntry, cause error) error {
	if errors.Is(cause, ErrDispatchUnplaced) {
		return r.strandUnplaced(ctx, entry)
	}

	spec := DispatchFailureSpec{
		Reason:        ReasonUndeliverableDispatch,
		EventUID:      entry.EventUID,
		DispatchID:    entry.EventUID,
		ResultUID:     entry.ResultUID,
		ResultVersion: entry.ResultVersion,
		ScenarioName:  entry.ScenarioName,
		RunnerUID:     entry.RunnerUID,
		ReportedBy:    ReporterControlPlane,
		Detail:        fmt.Sprintf("the dispatch could not be published: %v", cause),
		OccurredAt:    time.Now(),
	}

	var runner Runner
	if entry.RunnerUID != "" {
		if _, err := r.store.GetByUID(ctx, &runner, entry.RunnerUID); err != nil {
			// The runner's name is only a label. Losing it is not worth losing
			// the record.
			log.Printf("could not load runner %v while recording an undeliverable dispatch: %v", entry.RunnerUID, err)
		}
	}

	failure, created, err := recordDispatchFailure(ctx, r.store, spec, runner.Name)
	if err != nil {
		return err
	}

	if created {
		log.Printf("dispatch failure %q recorded: dispatch %v for result %v can never be published",
			failure.Name, entry.EventUID, entry.ResultUID)
	}

	return nil
}

// strandUnplaced marks the run of an unroutable dispatch terminal, with no
// dead-letter record.
//
// The run itself carries the fact, as LabelResultUnschedulable, so "everything
// that could not be placed" stays a label query over runs rather than a second
// resource per attempt.
func (r *undeliverableRecorder) strandUnplaced(ctx context.Context, entry DispatchOutboxEntry) error {
	if entry.ResultUID == "" {
		return nil
	}

	tx, err := r.store.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to open transaction to strand an unplaced run: %w", err)
	}
	// Rollback after a successful Commit is a documented no-op in wyrd's store.
	defer tx.Rollback()

	if err := strandRunTx(tx, entry.ResultUID, entry.ResultVersion, ReasonNoEligibleRunner); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to strand unplaced run %v: %w", entry.ResultUID, err)
	}

	log.Printf("run %v stranded: no runner was assigned, so its dispatch can never be published", entry.ResultUID)

	return nil
}
