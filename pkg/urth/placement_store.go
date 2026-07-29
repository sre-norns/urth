package urth

import (
	"context"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

// RunnerLoadStore answers how much work each runner already has.
//
// A whole-table question -- "group every unfinished run by the runner it was
// placed on" -- that a store addressed by UID cannot express, which is why it is
// its own interface over gorm rather than another method on the resource API.
// The same division of labour ReconcileStore follows.
//
// The counts come from Postgres rather than from the broker, and that is the
// deliberate simplification this change rests on. Review-backlog task 014
// assumed placement would have to inspect JetStream consumers, because when it
// was written nothing else knew what a runner was carrying. Two things have
// changed since: placement records the runner on a Result *before* it is
// persisted, so `results` knows the queue depth of every runner; and worker
// presence says which workers are actually reachable. Between them the database
// answers the whole question, so run creation gains no broker round trip and no
// new failure mode.
type RunnerLoadStore interface {
	// InFlightRuns counts unfinished runs per runner.
	//
	// Every runner at once rather than one at a time: placement needs the whole
	// eligible set to compare, and a query per candidate would put the fleet
	// size on the run-creation path.
	InFlightRuns(ctx context.Context) (map[manifest.ResourceID]RunnerLoad, error)
}

// RunnerLoad is the work committed to one runner, split by what it is doing.
//
// Split rather than summed because the two mean different things to an operator
// looking at a slow fleet: runs piling up in Queued is a fleet that is not
// keeping up, while a large Running with an empty queue is one that is simply
// busy.
type RunnerLoad struct {
	// Queued is placed but unclaimed -- the run is waiting in this runner's
	// queue for a worker to take it.
	Queued int

	// Running is claimed and executing.
	Running int
}

// runnerLoadStore reads run counts straight from the results table.
type runnerLoadStore struct {
	db *gorm.DB
}

// NewRunnerLoadStore returns a load store over an existing database handle, in
// the shape NewWorkerPresenceStore and NewReconcileStore established.
func NewRunnerLoadStore(db *gorm.DB) RunnerLoadStore {
	return &runnerLoadStore{db: db}
}

// runnerLoadRow is one (runner, status) tally from the grouped query.
type runnerLoadRow struct {
	RunnerID string
	Status   string
	Total    int
}

func (s *runnerLoadStore) InFlightRuns(ctx context.Context) (map[manifest.ResourceID]RunnerLoad, error) {
	var rows []runnerLoadRow

	// Hooks skipped for the reason reconcileStore skips them: Result.AfterFind
	// counts each row's artifacts, which is a query per row. This reads
	// aggregates and never materialises a Result at all.
	//
	// The two statuses are named rather than expressed as "not terminal", so a
	// state added later is counted only once someone has decided it should be.
	err := s.db.WithContext(ctx).
		Session(&gorm.Session{SkipHooks: true}).
		Model(&Result{}).
		Select("status_executor_runner_id AS runner_id, status_status AS status, COUNT(*) AS total").
		Where("deleted_at IS NULL").
		Where("status_status IN ?", []JobStatus{JobPending, JobRunning}).
		// An unplaced run -- one nothing could take -- carries no runner and is
		// nobody's load.
		Where("status_executor_runner_id IS NOT NULL AND status_executor_runner_id <> ''").
		Group("status_executor_runner_id, status_status").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count in-flight runs per runner: %w", err)
	}

	load := make(map[manifest.ResourceID]RunnerLoad, len(rows))
	for _, row := range rows {
		entry := load[manifest.ResourceID(row.RunnerID)]

		switch JobStatus(row.Status) {
		case JobPending:
			entry.Queued += row.Total
		case JobRunning:
			entry.Running += row.Total
		}

		load[manifest.ResourceID(row.RunnerID)] = entry
	}

	return load, nil
}
