package urth

import (
	"context"
	"fmt"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

// WorkerPresenceStore records that a worker was heard from.
//
// A separate interface from the resource APIs, and a separate write path from
// every other change to a WorkerInstance, for a reason worth stating plainly:
// recording liveness is not editing a resource. It happens on a timer, forever,
// for every worker in the fleet, and it changes nothing an operator declared.
//
// The distinction is not academic. wyrd's ObjectMeta.BeforeSave increments
// Version on every gorm Save, so routing a report through dbstore's
// CreateOrUpdate -- the way resource edits correctly go -- would bump a worker's
// resource version every interval for as long as it lives. The version would
// stop meaning "this record was edited", and the version-guarded delete behind
// the UI's Drop button would start failing against a version that was current a
// minute ago. The implementation below writes the columns directly instead; see
// the note on touch for exactly which gorm call and why.
type WorkerPresenceStore interface {
	// RecordContact stamps the API-contact signal: the worker reached the API
	// server, by heartbeat or by claiming a run.
	//
	// leaving marks a worker that announced its shutdown. It reports false when
	// no such registration exists, so a worker whose registration was dropped
	// learns to register again rather than reporting into nothing.
	RecordContact(ctx context.Context, workerUID manifest.ResourceID, at time.Time, via WorkerContact, leaving bool) (bool, error)

	// RecordNATSPresence stamps the queue signal: the worker was heard on its
	// runner's subject.
	//
	// A separate method writing a separate column, because the whole value of
	// these two signals is that one can move while the other does not.
	//
	// runnerUID is the runner whose subject the announcement arrived on, and the
	// write only lands if the worker really belongs to it. Broker permissions
	// scope a worker to its own runner's prefix, which stops one runner's fleet
	// speaking for another's, but says nothing about a worker naming a sibling
	// under the same prefix. Matching the membership here closes that without a
	// second round trip.
	RecordNATSPresence(ctx context.Context, workerUID, runnerUID manifest.ResourceID, at time.Time) (bool, error)
}

// workerPresenceStore writes liveness columns straight to the database.
type workerPresenceStore struct {
	db *gorm.DB
}

// NewWorkerPresenceStore returns a presence store over an existing database
// handle, in the shape NewReconcileStore established: the domain owns the
// interface and gorm is an implementation detail behind it.
func NewWorkerPresenceStore(db *gorm.DB) WorkerPresenceStore {
	return &workerPresenceStore{db: db}
}

func (s *workerPresenceStore) RecordContact(ctx context.Context, workerUID manifest.ResourceID, at time.Time, via WorkerContact, leaving bool) (bool, error) {
	columns := map[string]any{
		"status_last_seen_time": at,
		"status_last_seen_via":  via,
		// Cleared on every ordinary contact: a worker that shut down and has come
		// back must not be held offline by its own farewell.
		"status_left_at": nil,
	}
	if leaving {
		columns["status_left_at"] = at
	}

	return s.touch(ctx, workerUID, nil, columns)
}

func (s *workerPresenceStore) RecordNATSPresence(ctx context.Context, workerUID, runnerUID manifest.ResourceID, at time.Time) (bool, error) {
	if runnerUID == "" {
		return false, nil
	}

	return s.touch(ctx, workerUID, &runnerUID, map[string]any{
		"status_nats_last_seen_time": at,
		"status_left_at":             nil,
	})
}

// touch writes liveness columns for one worker without disturbing anything else
// about the record.
//
// Two deliberate choices, measured against a live Postgres rather than assumed:
//
//   - Not a resource save. dbstore's CreateOrUpdate goes through gorm Save,
//     which fires ObjectMeta.BeforeSave and increments Version -- so a worker
//     reporting every interval would climb a version per report, and the
//     version-guarded delete behind the UI's Drop button would fail against a
//     version that was current a minute ago.
//   - UpdateColumns rather than Updates. Both leave the version alone with a map
//     of columns, but Updates refreshes updated_at, and a heartbeat is not a
//     modification of the record. Letting it move updated_at would destroy the
//     answer to "when was this worker last actually changed" and, with it, any
//     later use of that column. UpdateColumns skips hooks too, which keeps this
//     correct if wyrd ever gives BeforeSave more to do.
//
// The columns are named as gorm generates them for an embedded status:
// WorkerInstanceStatus.LastSeenTime becomes status_last_seen_time. The
// reconciler's queries already depend on the same convention.
//
// runnerUID, when given, additionally requires the worker to be a member of that
// runner. `uid` and `runner_id` are uuid columns and these are bound parameters,
// which Postgres compares to text happily -- it is column-to-column comparison
// that would need a cast.
func (s *workerPresenceStore) touch(ctx context.Context, workerUID manifest.ResourceID, runnerUID *manifest.ResourceID, columns map[string]any) (bool, error) {
	if workerUID == "" {
		return false, nil
	}

	tx := s.db.WithContext(ctx).
		Model(&WorkerInstance{}).
		Where("uid = ?", workerUID).
		Where("deleted_at IS NULL")

	if runnerUID != nil {
		tx = tx.Where("runner_id = ?", *runnerUID)
	}

	tx = tx.UpdateColumns(columns)

	if tx.Error != nil {
		return false, fmt.Errorf("failed to record presence for worker %v: %w", workerUID, tx.Error)
	}

	return tx.RowsAffected == 1, nil
}
