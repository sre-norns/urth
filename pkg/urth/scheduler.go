package urth

import (
	"context"
	"io"
)

type RunID string

const InvalidRunID = RunID("")

const RunScenarioTopicName = "scenario:run"

type Scheduler interface {
	io.Closer

	Schedule(ctx context.Context, scenarioRun Result, scenario Scenario) (RunID, error)
}
