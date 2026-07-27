package urth

import (
	"context"
	"fmt"
	"time"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// dispatchOutboxStore is the Postgres-backed outbox.
//
// It talks to gorm directly rather than through wyrd's dbstore. The store
// interface is built for manifest resources addressed by UID and cannot express
// the one thing the relay depends on -- `SELECT ... FOR UPDATE SKIP LOCKED` --
// and a claim that cannot skip locked rows is a claim two relays can both win.
type dispatchOutboxStore struct {
	db *gorm.DB
}

// NewDispatchOutbox returns the outbox backed by an existing gorm connection.
func NewDispatchOutbox(db *gorm.DB) DispatchOutbox {
	return &dispatchOutboxStore{db: db}
}

// supportsSkipLocked reports whether the dialect can lease rows concurrently.
//
// Postgres can. SQLite cannot, and has no concurrent writers to protect against
// anyway -- it serialises the whole database -- so the claim degrades to a plain
// read there. This is what lets the relay's logic be unit-tested without a
// Postgres instance; it is not an endorsement of running SQLite in production,
// which does not work for the resource tables in any case.
func (s *dispatchOutboxStore) supportsSkipLocked() bool {
	return s.db.Dialector.Name() == "postgres"
}

// Claim leases due entries to one relay.
//
// The lease is a column, not a database lock held across the publication: a
// publication involves a network round trip to the broker, and holding a
// transaction open across it would pin a connection for as long as the broker
// takes to answer. The row lock is held only for the moment it takes to stamp
// the lease.
func (s *dispatchOutboxStore) Claim(ctx context.Context, relayID string, limit int, lease time.Duration) ([]DispatchOutboxEntry, error) {
	if limit <= 0 {
		return nil, nil
	}

	var claimed []DispatchOutboxEntry

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()

		query := tx.Model(&DispatchOutboxEntry{}).
			Where("published_at IS NULL").
			Where("not_before <= ?", now).
			// Either unclaimed, or claimed by a relay whose lease has lapsed.
			// The second case is the crash recovery path: a relay that died
			// mid-publication left its name on the row, and nothing else would
			// ever release it.
			Where("claim_expires_at IS NULL OR claim_expires_at <= ?", now).
			Order("not_before ASC, id ASC").
			Limit(limit)

		if s.supportsSkipLocked() {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}

		var due []DispatchOutboxEntry
		if err := query.Find(&due).Error; err != nil {
			return err
		}
		if len(due) == 0 {
			return nil
		}

		ids := make([]uint, 0, len(due))
		for _, entry := range due {
			ids = append(ids, entry.ID)
		}

		expiry := now.Add(lease)
		update := tx.Model(&DispatchOutboxEntry{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"claimed_by":       relayID,
				"claim_expires_at": expiry,
				// Counted at claim time rather than after the attempt, so that a
				// relay that crashes mid-publication still burns an attempt. A
				// counter only incremented on a recorded failure would sit at
				// zero forever for exactly the entry that keeps killing relays.
				"attempts":   gorm.Expr("attempts + 1"),
				"updated_at": now,
			})
		if update.Error != nil {
			return update.Error
		}

		for i := range due {
			due[i].ClaimedBy = relayID
			due[i].ClaimExpiresAt = &expiry
			due[i].Attempts++
		}
		claimed = due

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to claim dispatch outbox entries: %w", err)
	}

	return claimed, nil
}

func (s *dispatchOutboxStore) MarkPublished(ctx context.Context, id uint, at time.Time) error {
	tx := s.db.WithContext(ctx).Model(&DispatchOutboxEntry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"published_at":     at,
			"claimed_by":       "",
			"claim_expires_at": nil,
			"last_error":       "",
			"updated_at":       at,
		})
	if tx.Error != nil {
		return fmt.Errorf("failed to mark dispatch %d published: %w", id, tx.Error)
	}
	// An update that matched nothing means the row this relay is holding is not
	// there any more. Silently succeeding would report a dispatch as published
	// against a row that cannot say so, and the next relay would publish it
	// again -- forever, since nothing can ever mark it.
	if tx.RowsAffected == 0 {
		return fmt.Errorf("%w: dispatch %d disappeared before it could be marked published", ErrPermanentDispatch, id)
	}

	return nil
}

func (s *dispatchOutboxStore) MarkFailed(ctx context.Context, id uint, cause error, notBefore time.Time) error {
	tx := s.db.WithContext(ctx).Model(&DispatchOutboxEntry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_error": truncateError(cause),
			"not_before": notBefore,
			// The lease is released rather than left to expire: the failure is
			// already recorded, and holding the row until the lease lapses would
			// delay the retry by the lease duration for no benefit.
			"claimed_by":       "",
			"claim_expires_at": nil,
			"updated_at":       time.Now(),
		})
	if tx.Error != nil {
		return fmt.Errorf("failed to mark dispatch %d failed: %w", id, tx.Error)
	}
	if tx.RowsAffected == 0 {
		return fmt.Errorf("dispatch %d disappeared before its failure could be recorded", id)
	}

	return nil
}

// storeResultLoader rehydrates the resources a legacy dispatch needs.
type storeResultLoader struct {
	store *dbstore.DBStore
}

// NewStoreResultLoader reads Results and Scenarios for SchedulerDispatchPublisher.
func NewStoreResultLoader(store *dbstore.DBStore) ResultLoader {
	return &storeResultLoader{store: store}
}

func (l *storeResultLoader) LoadForDispatch(ctx context.Context, entry DispatchOutboxEntry) (Result, Scenario, error) {
	var result Result
	if ok, err := l.store.GetByUID(ctx, &result, entry.ResultUID); err != nil {
		return result, Scenario{}, fmt.Errorf("failed to load result %v for dispatch: %w", entry.ResultUID, err)
	} else if !ok {
		// The Result is gone but its dispatch survived. Retrying cannot bring it
		// back, and publishing a job for a Result nothing can report against
		// would strand a worker.
		return result, Scenario{}, fmt.Errorf("%w: result %v no longer exists", ErrPermanentDispatch, entry.ResultUID)
	}

	var scenario Scenario
	if ok, err := l.store.GetByUID(ctx, &scenario, result.Spec.ScenarioID); err != nil {
		return result, scenario, fmt.Errorf("failed to load scenario %v for dispatch: %w", result.Spec.ScenarioID, err)
	} else if !ok {
		return result, scenario, fmt.Errorf("%w: scenario %v no longer exists", ErrPermanentDispatch, result.Spec.ScenarioID)
	}

	result.Spec.Scenario = scenario

	return result, scenario, nil
}

func (s *dispatchOutboxStore) Stats(ctx context.Context, now time.Time) (DispatchOutboxStats, error) {
	var stats DispatchOutboxStats

	db := s.db.WithContext(ctx)

	unpublished := func() *gorm.DB {
		return db.Model(&DispatchOutboxEntry{}).Where("published_at IS NULL")
	}

	if err := unpublished().Count(&stats.Pending).Error; err != nil {
		return stats, fmt.Errorf("failed to count pending dispatches: %w", err)
	}
	if stats.Pending == 0 {
		return stats, nil
	}

	if err := unpublished().Where("attempts > 0 AND last_error <> ''").Count(&stats.Failing).Error; err != nil {
		return stats, fmt.Errorf("failed to count failing dispatches: %w", err)
	}

	var oldest DispatchOutboxEntry
	if err := unpublished().Order("created_at ASC").First(&oldest).Error; err != nil {
		return stats, fmt.Errorf("failed to read oldest pending dispatch: %w", err)
	}
	stats.OldestAge = now.Sub(oldest.CreatedAt)

	var worst DispatchOutboxEntry
	if err := unpublished().Order("attempts DESC, id ASC").First(&worst).Error; err != nil {
		return stats, fmt.Errorf("failed to read most-attempted dispatch: %w", err)
	}
	stats.MaxAttempts = worst.Attempts
	stats.LastError = worst.LastError

	return stats, nil
}
