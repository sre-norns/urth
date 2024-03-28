package urth

import (
	"encoding/json"

	"github.com/sre-norns/urth/pkg/wyrd"
)

// RunScenarioJob represents a to be picked by a qualified worker
type RunScenarioJob struct {
	// Name of the scenario that produced this job
	Name string `form:"name,omitempty" json:"name,omitempty" yaml:"name,omitempty" xml:"name,omitempty" `

	// Labels of the scenario
	Labels wyrd.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" `

	// Requirements for a set of labels that a worker must satisfy
	Requirements wyrd.LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" `

	// A schedule to run the script
	RunSchedule CronSchedule `form:"schedule" json:"schedule,omitempty" yaml:"schedule,omitempty" xml:"schedule"`

	// ID and version of the scenario that this results were produced for
	ScenarioID wyrd.VersionedResourceId `form:"play_id" json:"play_id" yaml:"play_id" xml:"play_id"  binding:"required" `

	// Script of a job to be performed by a runner
	Prob ProbManifest `form:"prob" json:"prob" yaml:"prob" xml:"prob" `

	// True if you want the worker to keep temp working directory with run artifacts
	IsKeepDirectory bool `form:"keepDir" json:"keepDir" yaml:"keepDir" xml:"keepDir" `

	// Version and ID of the run to update results to
	RunID   wyrd.VersionedResourceId `json:"runId" yaml:"runId"`
	RunName string                   `json:"runName" yaml:"runName"`
}

func scenarioToRunnable(run Result, scenarioMeta ResourceMeta, scenario *ScenarioSpec) RunScenarioJob {
	return RunScenarioJob{
		Name:       scenarioMeta.Name,
		ScenarioID: scenarioMeta.GetVersionedID(),

		Requirements: scenario.Requirements,
		RunSchedule:  scenario.RunSchedule,
		Prob:         scenario.Prob,

		Labels: wyrd.MergeLabels(
			scenarioMeta.Labels,
			run.Labels,
		),

		RunID:   run.GetVersionedID(),
		RunName: run.Name,
	}
}

func UnmarshalJob(data []byte) (result RunScenarioJob, err error) {
	// err = yaml.Unmarshal(data, &result)
	err = json.Unmarshal(data, &result)
	return
}

func MarshalJob(runScenario RunScenarioJob) ([]byte, error) {
	// return yaml.Marshal(&runScenario)
	return json.Marshal(&runScenario)
}
