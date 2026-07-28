package urth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrDispatchFailureNotRetryable marks a failure whose run cannot be re-created.
var ErrDispatchFailureNotRetryable = errors.New("dispatch failure cannot be retried")

// recordDispatchFailure writes a failure and, when the affected run is still
// pending, strands it in the same transaction.
//
// The two halves have to commit together. A failure recorded without the Result
// transition leaves a run pending forever with a dead-letter record explaining
// why -- a worse state than either alone, because monitoring says the run is
// still coming and the evidence that it is not sits somewhere nobody is looking.
// A Result errored without the record loses the reason entirely.
//
// Reports whether the record was newly created. A repeat report of the same
// dispatch failing the same way returns the existing record and false, so a
// worker retrying a report it is unsure landed cannot manufacture history.
func recordDispatchFailure(ctx context.Context, store dbstore.TransactionalStore, spec DispatchFailureSpec, runnerName manifest.ResourceName) (DispatchFailure, bool, error) {
	if !spec.Reason.IsValid() {
		return DispatchFailure{}, false, fmt.Errorf("%w: unknown reason %q", ErrInvalidDispatchFailure, spec.Reason)
	}
	if spec.EventUID == "" {
		return DispatchFailure{}, false, fmt.Errorf("%w: failure requires an event UID", ErrInvalidDispatchFailure)
	}

	spec.SchemaVersion = DispatchFailureSpecVersion
	spec.Detail = truncateDetail(spec.Detail)
	if spec.OccurredAt.IsZero() {
		spec.OccurredAt = time.Now()
	}

	name := DispatchFailureName(spec.EventUID, spec.Reason)

	// The cheap path, and the common one: this failure is already on record.
	// Checked before opening a transaction because a worker whose report
	// succeeded but whose response was lost will ask again, and that is not an
	// error worth a rollback.
	var existing DispatchFailure
	if found, err := store.GetByName(ctx, &existing, name); err != nil {
		return DispatchFailure{}, false, fmt.Errorf("failed to look up dispatch failure %q: %w", name, err)
	} else if found {
		return existing, false, nil
	}

	entry := DispatchFailure{
		ObjectMeta: manifest.ObjectMeta{
			Name:   name,
			Labels: dispatchFailureLabels(spec, runnerName),
		},
		Spec: spec,
	}

	tx, err := store.Begin(ctx)
	if err != nil {
		return DispatchFailure{}, false, fmt.Errorf("failed to open transaction to record a dispatch failure: %w", err)
	}
	// Rollback after a successful Commit is a documented no-op in wyrd's store,
	// so this covers every early return without a flag to track.
	defer tx.Rollback()

	if err := tx.Create(&entry); err == nil {
		if err := strandRun(tx, spec); err != nil {
			return DispatchFailure{}, false, err
		}

		if err := tx.Commit(); err == nil {
			return entry, true, nil
		}
	}

	// The create or the commit failed. A race against a concurrent reporter
	// leaves the record present, and re-reading settles it without classifying
	// the driver's error -- what matters is whether the failure is now on
	// record, not which constraint said so.
	var raced DispatchFailure
	if found, lookupErr := store.GetByName(ctx, &raced, name); lookupErr == nil && found {
		return raced, false, nil
	}

	return DispatchFailure{}, false, fmt.Errorf("failed to record dispatch failure %q", name)
}

// strandRun moves the run a dead dispatch was carrying to a terminal error.
//
// Only a pending run is touched. A run that is already `running` was claimed
// through some other delivery and is somebody's live work; a terminal one is
// history. Both cases still record the failure -- the delivery really did fail,
// and suppressing that would hide a broker fault behind a run that happened to
// succeed on a redelivery -- but neither has its state rewritten.
//
// The outbox row is deliberately left alone. Retiring it here would take it out
// of the reconciler's stale-dispatch sweep, which is what withdraws the queued
// message now that the Result is terminal. Leaving it lets the machinery task
// 003 already built do the withdrawal, rather than this path growing a second,
// subtly different copy of it.
func strandRun(tx dbstore.StoreTransaction, spec DispatchFailureSpec) error {
	if spec.ResultUID == "" {
		// A malformed envelope may not have yielded a run to strand. The record
		// is still worth having: it says a message was undeliverable, which is a
		// fault in its own right.
		return nil
	}

	var result Result
	found, err := tx.GetByUID(&result, spec.ResultUID)
	if err != nil {
		return fmt.Errorf("failed to load result %v for a dispatch failure: %w", spec.ResultUID, err)
	}
	if !found {
		return nil
	}

	if result.Status.Status != JobPending {
		return nil
	}

	// A dispatch for a superseded version is not evidence about the run as it
	// now stands. The current version has its own dispatch, and stranding the
	// run on the strength of an old message would kill work that is still live.
	if spec.ResultVersion != 0 && spec.ResultVersion != result.Version {
		return nil
	}

	version := result.Version
	failRun(&result, spec.Reason, time.Now())

	// Version-guarded like every other transition on this path: the race here is
	// against a worker claiming the run through a redelivery that arrived while
	// this report was in flight, and the claim must win.
	updated, err := tx.Update(&result, result.UID, dbstore.WithVersion(version))
	if err != nil {
		return fmt.Errorf("failed to strand result %v: %w", spec.ResultUID, err)
	}
	if !updated {
		// Lost to a concurrent transition. The failure record still commits: the
		// dispatch did fail, whatever the run went on to do.
		log.Printf("result %v moved on before a dispatch failure could strand it", spec.ResultUID)
	}

	return nil
}

// failRun records a run stranded by a dead dispatch.
//
// `errored` rather than `timeout`: nothing waited and nothing ran out of time.
// The dispatch that was supposed to start this run is permanently undeliverable,
// which is a different fact from an execution lease elapsing, and an operator
// triaging by state should not have to read the detail to tell them apart.
func failRun(result *Result, reason DispatchFailureReason, at time.Time) {
	result.Status.Status = JobErrored
	result.Status.Result = prob.RunFinishedError
	result.Spec.TimeEnded = &at

	result.Labels = manifest.MergeLabels(
		result.Labels,
		manifest.Labels{
			LabelResultJobState:      string(result.Status.Status),
			LabelResultStatus:        string(result.Status.Result),
			LabelResultUnschedulable: reason.String(),
		},
	)
}

// retryDispatchFailure creates the new attempt.
//
// A new Result rather than a reopened one, which ADR 0004 requires and this path
// could easily get wrong: the failed run records an attempt that really was made
// and really did strand, and an operator retrying is asking for another attempt,
// not for the record of the first to be edited.
//
// Reports false when the failure has already been retried, so a double-click on
// an operator surface does not schedule two runs.
func retryDispatchFailure(ctx context.Context, store dbstore.TransactionalStore, failure DispatchFailure, resolve bool, at time.Time) (DispatchFailure, Result, bool, error) {
	if failure.Status.RetryResultUID != "" {
		return failure, Result{}, false, nil
	}

	if failure.Spec.ResultUID == "" {
		return failure, Result{}, false, fmt.Errorf("%w: failure names no run to retry", ErrDispatchFailureNotRetryable)
	}

	var original Result
	if found, err := store.GetByUID(ctx, &original, failure.Spec.ResultUID); err != nil {
		return failure, Result{}, false, fmt.Errorf("failed to load the stranded run: %w", err)
	} else if !found {
		return failure, Result{}, false, fmt.Errorf("%w: the stranded run no longer exists", ErrDispatchFailureNotRetryable)
	}

	// The execution input is copied, never re-derived from the scenario as it
	// now stands. Re-reading it here would make "retry" quietly run something the
	// failed attempt was never asked to do -- the same trap the execution
	// snapshot was introduced to close on the claim path.
	if original.Spec.Execution.IsZero() {
		return failure, Result{}, false, fmt.Errorf("%w: the stranded run carries no execution snapshot", ErrDispatchFailureNotRetryable)
	}

	// Lowercased, because a resource name must be a DNS subdomain and
	// NewRandToken's alphabet is mixed case. Creating a Result through the
	// service does this; building one here does not, and the row stores happily
	// -- the refusal surfaces only when a client reads it back.
	name := manifest.ResourceName(strings.ToLower(string(NewRandToken(16))))

	retry := Result{
		ObjectMeta: manifest.ObjectMeta{
			Name:   name,
			Labels: retryLabels(original, failure),
		},
		Spec: ResultSpec{
			// The scenario foreign key is carried over rather than left unset:
			// it is a uuid column, and an empty one is not a null one. The
			// association itself is deliberately not copied -- gorm would try to
			// write it back, and the retry has no business updating a scenario.
			ScenarioID: original.Spec.ScenarioID,
			ProbKind:   original.Spec.ProbKind,
			Execution:  original.Spec.Execution,
		},
		Status: ResultStatus{
			Status: JobPending,
			Result: prob.RunNotFinished,
			// Placement is inherited rather than recomputed. The failed run was
			// placed on this runner and the retry re-attempts that same work;
			// re-running placement would silently move it, which is a scheduling
			// decision and not this path's to make.
			Executor: ExecutorRef{
				RunnerID:   original.Status.Executor.RunnerID,
				RunnerName: original.Status.Executor.RunnerName,
			},
		},
	}

	updated := failure

	tx, err := store.Begin(ctx)
	if err != nil {
		return failure, Result{}, false, fmt.Errorf("failed to open transaction to retry a dispatch: %w", err)
	}
	defer tx.Rollback()

	if err := tx.Create(&retry); err != nil {
		return failure, Result{}, false, fmt.Errorf("failed to create the retry run: %w", err)
	}

	// Built after the insert, because the entry is keyed on the UID and version
	// gorm assigns there. Same transaction as the Result for the same reason
	// ordinary run creation is: a retry that committed without its dispatch
	// would sit pending with nothing to wake a worker.
	outboxEntry := NewDispatchOutboxEntry(retry, at)
	if err := tx.Create(&outboxEntry); err != nil {
		return failure, Result{}, false, fmt.Errorf("failed to enqueue the retry dispatch: %w", err)
	}

	updated.Status.RetryResultUID = retry.UID
	updated.Status.RetryResultName = retry.Name
	if resolve {
		markResolved(&updated, at)
	}

	// Version-guarded: two operators pressing retry race here, and exactly one
	// may win. The loser's transaction rolls back, taking its Result and outbox
	// row with it, which is why the run is created inside this transaction
	// rather than before it.
	ok, err := tx.Update(&updated, updated.UID, dbstore.WithVersion(failure.Version))
	if err != nil {
		return failure, Result{}, false, fmt.Errorf("failed to record the retry: %w", err)
	}
	if !ok {
		return failure, Result{}, false, nil
	}

	if err := tx.Commit(); err != nil {
		return failure, Result{}, false, fmt.Errorf("failed to commit the retry: %w", err)
	}

	return updated, retry, true, nil
}

// resolveDispatchFailure closes a failure without retrying it.
func resolveDispatchFailure(ctx context.Context, store dbstore.TransactionalStore, failure DispatchFailure, at time.Time) (DispatchFailure, bool, error) {
	if failure.Status.Resolved {
		return failure, false, nil
	}

	updated := failure
	markResolved(&updated, at)

	ok, err := store.Update(ctx, &updated, updated.UID, dbstore.WithVersion(failure.Version))
	if err != nil {
		return failure, false, fmt.Errorf("failed to resolve dispatch failure %q: %w", failure.Name, err)
	}

	return updated, ok, nil
}

// markResolved records that an operator is done with a failure.
func markResolved(failure *DispatchFailure, at time.Time) {
	failure.Status.Resolved = true
	failure.Status.ResolvedAt = &at
	failure.Labels = manifest.MergeLabels(failure.Labels, manifest.Labels{
		LabelDispatchFailureResolved: "true",
	})
}

// retryLabels carries the stranded run's identity onto its re-attempt.
//
// The forward link lives on the failure record; this is the backward one, so
// that an operator looking at a run can see it is a retry and of what, without
// having to search the dead-letter list for a record that mentions it.
func retryLabels(original Result, failure DispatchFailure) manifest.Labels {
	labels := manifest.Labels{}
	for key, value := range original.Labels {
		// The stranded run's terminal state is not the retry's. Copying it would
		// make a pending run advertise itself as errored, and every label query
		// an operator runs against run state would count it twice.
		switch key {
		case LabelResultJobState, LabelResultStatus, LabelResultUnschedulable:
			continue
		}
		labels[key] = value
	}

	labels[LabelResultJobState] = string(JobPending)
	putLabel(labels, LabelRetryOfResult, string(original.UID))
	putLabel(labels, LabelRetryOfFailure, string(failure.Name))

	return labels
}
