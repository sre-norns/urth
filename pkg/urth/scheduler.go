package urth

import (
	"context"
	"io"
)

type RunId string

const InvalidRunId = RunId("")

const RunScenarioTopicName = "scenario:run"

type Scheduler interface {
	io.Closer

	Schedule(ctx context.Context, scenario RunScenarioJob) (RunId, error)
}
