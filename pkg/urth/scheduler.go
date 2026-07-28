package urth

import (
	"context"
	"io"
)

type RunID string

const InvalidRunID = RunID("")

const RunScenarioTopicName = "scenario:run"

// Scheduler publishes a run to a transport.
//
// It takes the Result alone. It used to take the Scenario beside it, and the
// asynq transport marshalled that scenario's prob into the queue message -- so a
// scenario edited between scheduling and publication changed what a queued run
// executed. The Result now carries its own execution snapshot, and dropping the
// parameter is what makes that structural rather than a convention a transport
// has to remember.
type Scheduler interface {
	io.Closer

	Schedule(ctx context.Context, scenarioRun Result) (RunID, error)
}
