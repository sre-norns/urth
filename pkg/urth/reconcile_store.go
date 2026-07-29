package urth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// reconcileStore answers the reconciler's whole-table questions.
//
// It holds both handles on purpose. Every state transition goes through
// dbstore, so the version guard and the metadata bookkeeping are the same ones
// the API uses -- a reconciler writing Results by a private route would be a
// second definition of what a valid transition is. The queries that find work,
// on the other hand, are joins and predicates over columns that a store
// addressed by UID cannot express, so those go to gorm directly.
type reconcileStore struct {
	db    *gorm.DB
	store *dbstore.DBStore
}

// NewReconcileStore returns the reconciler's view of an existing store.
func NewReconcileStore(db *gorm.DB, store *dbstore.DBStore) ReconcileStore {
	return &reconcileStore{db: db, store: store}
}

// AcquireScanLease claims the right to run one scan.
//
// Two statements rather than an upsert, because the interesting case is not
// insertion: it is one reconciler taking over from another whose lease has
// elapsed, which has to be a single conditional UPDATE or two reconcilers can
// both read "expired" and both write "mine". `RowsAffected` from that UPDATE is
// the whole decision.
func (s *reconcileStore) AcquireScanLease(ctx context.Context, holder string, lease time.Duration) (bool, error) {
	now := time.Now()
	expires := now.Add(lease)

	taken := s.db.WithContext(ctx).Model(&ReconcileLease{}).
		Where("name = ?", ReconcileScanLeaseName).
		// Either nobody holds it any more, or this reconciler is renewing its own.
		Where("expires_at <= ? OR holder = ?", now, holder).
		Updates(map[string]any{
			"holder":     holder,
			"expires_at": expires,
			"updated_at": now,
		})
	if taken.Error != nil {
		return false, fmt.Errorf("failed to take the reconcile lease: %w", taken.Error)
	}
	if taken.RowsAffected == 1 {
		return true, nil
	}

	// No row matched. Either the lease is live and held elsewhere -- in which
	// case the insert below conflicts and reports nothing taken -- or this is the
	// first scan this deployment has ever run.
	row := ReconcileLease{
		Name:      ReconcileScanLeaseName,
		Holder:    holder,
		ExpiresAt: expires,
		UpdatedAt: now,
	}

	created := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row)
	if created.Error != nil {
		return false, fmt.Errorf("failed to create the reconcile lease: %w", created.Error)
	}

	return created.RowsAffected == 1, nil
}

func (s *reconcileStore) ReleaseScanLease(ctx context.Context, holder string) error {
	now := time.Now()

	tx := s.db.WithContext(ctx).Model(&ReconcileLease{}).
		Where("name = ?", ReconcileScanLeaseName).
		// Only the holder may release it. A reconciler that lost the lease to a
		// takeover must not then expire the new holder's claim on its way out.
		Where("holder = ?", holder).
		Updates(map[string]any{
			"expires_at": now,
			"updated_at": now,
		})
	if tx.Error != nil {
		return fmt.Errorf("failed to release the reconcile lease: %w", tx.Error)
	}

	return nil
}

// scan is the session used for the queries that find work.
//
// Hooks are skipped deliberately. Result.AfterFind counts each row's artifacts,
// which is a query per row -- reasonable when serving one run to a browser,
// absurd for a batch the reconciler only wants the lifecycle columns of.
func (s *reconcileStore) scan(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true})
}

func (s *reconcileStore) ExpiredRuns(ctx context.Context, cutoff time.Time, limit int) ([]Result, error) {
	var results []Result

	err := s.scan(ctx).
		Where("status_status = ?", JobRunning).
		// A zero deadline is a run claimed by the deprecated Auth path, which
		// never recorded one. Expiring those on the strength of a zero value
		// would terminate every legacy run the moment this shipped.
		Where("status_deadline IS NOT NULL").
		Where("status_deadline > ?", time.Time{}).
		Where("status_deadline < ?", cutoff).
		Order("status_deadline ASC").
		Limit(limit).
		Find(&results).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query runs with expired leases: %w", err)
	}

	return results, nil
}

// SilentWorkers lists registrations that have gone quiet on every signal.
//
// The first predicate is the load-bearing one: a worker with neither timestamp
// has never reported at all, which is `unknown` rather than offline, and
// evicting on no evidence would delete the asynq prototype's registrations and
// every record written before liveness reporting existed. Silence is only
// meaningful from something that was once heard.
//
// Both signals must be quiet. A worker still announcing itself over NATS is
// present, however long its route to the API server has been broken -- that is
// the api-unreachable case, which wants an operator, not a deletion.
func (s *reconcileStore) SilentWorkers(ctx context.Context, cutoff time.Time, limit int) ([]WorkerInstance, error) {
	var workers []WorkerInstance

	// COALESCE to the zero time rather than testing each column for NULL: a
	// signal never heard is older than any cutoff, and spelling that out in SQL
	// keeps the predicate the same shape as WorkerInstanceStatus.IsSilent.
	err := s.scan(ctx).
		Where("status_last_seen_time IS NOT NULL OR status_nats_last_seen_time IS NOT NULL").
		Where("COALESCE(status_last_seen_time, ?) < ?", time.Time{}, cutoff).
		Where("COALESCE(status_nats_last_seen_time, ?) < ?", time.Time{}, cutoff).
		Order("uid ASC").
		Limit(limit).
		Find(&workers).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query workers that have gone silent: %w", err)
	}

	return workers, nil
}

// DropWorker revokes one worker's registration.
//
// Through dbstore rather than a bulk delete, so the soft-delete and version
// bookkeeping are exactly the API's: a worker the reconciler drops and one an
// operator drops should leave the same trace.
func (s *reconcileStore) DropWorker(ctx context.Context, worker WorkerInstance) (bool, error) {
	return s.store.Delete(ctx, &WorkerInstance{}, worker.UID, worker.Version)
}

func (s *reconcileStore) StalePendingRuns(ctx context.Context, cutoff time.Time, limit int) ([]PendingRun, error) {
	var results []Result

	err := s.scan(ctx).
		Where("status_status = ?", JobPending).
		Where("created_at < ?", cutoff).
		Order("created_at ASC").
		Limit(limit).
		Find(&results).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query stale pending runs: %w", err)
	}
	if len(results) == 0 {
		return nil, nil
	}

	uids := make([]manifest.ResourceID, 0, len(results))
	for _, result := range results {
		uids = append(uids, result.UID)
	}

	var entries []DispatchOutboxEntry
	if err := s.scan(ctx).Where("result_uid IN ?", uids).Find(&entries).Error; err != nil {
		return nil, fmt.Errorf("failed to query dispatches for stale pending runs: %w", err)
	}

	// Keyed on version as well as UID: an entry written for an earlier version of
	// a Result says nothing about whether the current version has been
	// dispatched, and treating it as if it did would leave a re-dispatched run
	// waiting on a message that was published for something else.
	type dispatchKey struct {
		uid     manifest.ResourceID
		version manifest.Version
	}

	dispatches := make(map[dispatchKey]DispatchOutboxEntry, len(entries))
	for _, entry := range entries {
		dispatches[dispatchKey{uid: entry.ResultUID, version: entry.ResultVersion}] = entry
	}

	pending := make([]PendingRun, 0, len(results))
	for _, result := range results {
		run := PendingRun{Result: result}
		if entry, found := dispatches[dispatchKey{uid: result.UID, version: result.Version}]; found {
			run.Dispatch = &entry
		}
		pending = append(pending, run)
	}

	return pending, nil
}

// ExpireRun records the expiry and retires the dispatch that would have started
// the run, in one transaction.
//
// The atomicity is not incidental. Expiring a Result while leaving a publishable
// outbox entry behind would have the relay queue a job for a run that is already
// terminal, which every worker would then be handed and refuse.
func (s *reconcileStore) ExpireRun(ctx context.Context, result Result, at time.Time, reason string) (bool, error) {
	// Refused rather than guarded against by the caller's query alone. The
	// version check below settles a race, but it would happily admit an expiry
	// written over a Result that was already `completed` at the moment it was
	// read -- rewriting the outcome of a run that finished. Nothing terminal is
	// reopened or overwritten here, ever.
	if result.Status.Status.IsTerminal() {
		return false, nil
	}

	expired := result
	expireResult(&expired, at)

	// errLostRace unwinds the transaction when the version guard rejects the
	// write. gorm only rolls back on an error, and rolling back is what must
	// happen: the dispatch retirement below is conditional on the expiry having
	// landed, not on it having been attempted.
	errLostRace := fmt.Errorf("run %q moved on before it could be expired", result.Name)

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The store is rebuilt over the transaction handle rather than the
		// transition being open-coded here. Version guarding, the version bump
		// and the metadata bookkeeping are one definition, in dbstore, and a
		// reconciler that wrote Results by a private route would quietly become a
		// second one.
		txStore, err := dbstore.NewDBStore(tx, dbstore.ManifestModel)
		if err != nil {
			return err
		}

		// Version-guarded, for the same reason the claim is: this decides the
		// race between two reconcilers, and between a reconciler and a worker
		// reporting a run it finished after all. The loser's version is stale and
		// its write does not apply.
		updated, err := txStore.Update(ctx, &expired, expired.UID, dbstore.WithVersion(result.Version))
		if err != nil {
			return err
		}
		if !updated {
			return errLostRace
		}

		return tx.Model(&DispatchOutboxEntry{}).
			Where("result_uid = ?", result.UID).
			// Only what has not gone out yet. A dispatch already published needs
			// its message withdrawn before the row is retired, which is the
			// stale-dispatch sweep's job and not something to do inside the
			// transaction that owns the Result.
			Where("published_at IS NULL").
			Where("retired_at IS NULL").
			Updates(map[string]any{
				"retired_at":     at,
				"retired_reason": truncateReason(reason),
				"updated_at":     at,
			}).Error
	})

	switch {
	case errors.Is(err, errLostRace):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("failed to expire run %q: %w", result.Name, err)
	}

	return true, nil
}

func (s *reconcileStore) EnqueueDispatch(ctx context.Context, result Result, now time.Time) (bool, error) {
	entry := NewDispatchOutboxEntry(result, now)

	// DoNothing on conflict, because the event UID is derived from the Result's
	// identity and version: two reconcilers repairing the same run produce the
	// same row, and so does a scan repeating work a previous scan already did.
	tx := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_uid"}}, DoNothing: true}).
		Create(&entry)
	if tx.Error != nil {
		return false, fmt.Errorf("failed to enqueue a dispatch for run %q: %w", result.Name, tx.Error)
	}

	return tx.RowsAffected == 1, nil
}

// staleDispatchRow is one row of the stale-dispatch join.
//
// Flat and explicit rather than the entry embedded: the query computes a column
// that is not on any model, and gorm scanning a struct that mixes model fields
// with computed ones is a source of surprises this does not need.
type staleDispatchRow struct {
	ID            uint
	EventUID      string
	ResultUID     manifest.ResourceID
	ResultVersion manifest.Version
	RunnerUID     manifest.ResourceID
	PublishedAt   *time.Time
	PublishedSeq  uint64
	Delivered     bool
}

func (s *reconcileStore) StaleDispatches(ctx context.Context, limit int) ([]StaleDispatch, error) {
	var rows []staleDispatchRow

	terminal := TerminalJobStates()

	err := s.db.WithContext(ctx).
		Table("dispatch_outbox").
		Select(`dispatch_outbox.id AS id,
			dispatch_outbox.event_uid AS event_uid,
			dispatch_outbox.result_uid AS result_uid,
			dispatch_outbox.result_version AS result_version,
			dispatch_outbox.runner_uid AS runner_uid,
			dispatch_outbox.published_at AS published_at,
			dispatch_outbox.published_seq AS published_seq,
			(COALESCE(results.status_dispatch_id, '') = dispatch_outbox.event_uid) AS delivered`).
		// LEFT JOIN, so an entry whose Result was deleted outright comes back
		// too. That row is the most stale of all: nothing will ever claim it, and
		// nothing else would ever notice it.
		//
		// The cast is not cosmetic: Postgres stores `results.uid` as a uuid and
		// the outbox's copy of it as text, and comparing the two without it fails
		// the whole query with "operator does not exist".
		Joins("LEFT JOIN results ON CAST(results.uid AS TEXT) = dispatch_outbox.result_uid").
		Where("dispatch_outbox.retired_at IS NULL").
		Where("results.uid IS NULL OR results.deleted_at IS NOT NULL OR results.status_status IN ?", terminal).
		Order("dispatch_outbox.id ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query stale dispatches: %w", err)
	}

	stale := make([]StaleDispatch, 0, len(rows))
	for _, row := range rows {
		stale = append(stale, StaleDispatch{
			Entry: DispatchOutboxEntry{
				ID:            row.ID,
				SchemaVersion: DispatchOutboxEntryVersion,
				EventUID:      row.EventUID,
				ResultUID:     row.ResultUID,
				ResultVersion: row.ResultVersion,
				RunnerUID:     row.RunnerUID,
				PublishedAt:   row.PublishedAt,
				PublishedSeq:  row.PublishedSeq,
			},
			Delivered: row.Delivered,
		})
	}

	return stale, nil
}

func (s *reconcileStore) RetireDispatch(ctx context.Context, id uint, at time.Time, reason string) error {
	tx := s.db.WithContext(ctx).Model(&DispatchOutboxEntry{}).
		Where("id = ?", id).
		Where("retired_at IS NULL").
		Updates(map[string]any{
			"retired_at":     at,
			"retired_reason": truncateReason(reason),
			// The lease goes with it: an entry nobody will publish must not read
			// as one a relay is working on.
			"claimed_by":       "",
			"claim_expires_at": nil,
			"updated_at":       at,
		})
	if tx.Error != nil {
		return fmt.Errorf("failed to retire dispatch %d: %w", id, tx.Error)
	}

	return nil
}

func (s *reconcileStore) ReleaseAbandonedDispatches(ctx context.Context, now time.Time) (int, error) {
	tx := s.db.WithContext(ctx).Model(&DispatchOutboxEntry{}).
		Where("published_at IS NULL").
		Where("retired_at IS NULL").
		Where("claimed_by <> ''").
		Where("claim_expires_at IS NOT NULL AND claim_expires_at <= ?", now).
		Updates(map[string]any{
			"claimed_by":       "",
			"claim_expires_at": nil,
			"updated_at":       now,
		})
	if tx.Error != nil {
		return 0, fmt.Errorf("failed to release abandoned dispatch leases: %w", tx.Error)
	}

	return int(tx.RowsAffected), nil
}

func (s *reconcileStore) ActiveRunnerUIDs(ctx context.Context) ([]manifest.ResourceID, error) {
	var uids []manifest.ResourceID

	err := s.scan(ctx).Model(&Runner{}).
		Where("is_active = ?", true).
		Pluck("uid", &uids).Error
	if err != nil {
		return nil, fmt.Errorf("failed to query active runners: %w", err)
	}

	return uids, nil
}

// truncateReason bounds the stored explanation, which is written for operators
// and can inherit the length of whatever error produced it.
func truncateReason(reason string) string {
	if len(reason) > LastErrorLimit {
		return reason[:LastErrorLimit]
	}

	return reason
}
