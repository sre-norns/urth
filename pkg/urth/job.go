package urth

import (
	"encoding/json"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type Job struct {
	// Name of the resource representing run results
	ResultName manifest.ResourceName `json:"runName" yaml:"runName"`

	// Name of the scenario that this results were produced for
	ScenarioName manifest.ResourceName `form:"scenarioName" json:"scenarioName" yaml:"scenarioName" xml:"scenarioName"  binding:"required"`

	// Labels of the scenario
	Labels manifest.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" `

	// Script of a job to be performed by a runner
	Prob ProbManifest `form:"prob" json:"prob" yaml:"prob" xml:"prob"`

	// True if you want the worker to keep temp working directory with run artifacts
	IsKeepDirectory bool `form:"keepDir" json:"keepDir" yaml:"keepDir" xml:"keepDir"`
}

// RunScenarioJob represents a job to be picked by a qualifying worker
type RunScenarioJob struct {
	// Name of the scenario that this results were produced for
	ScenarioName manifest.ResourceName `form:"scenarioName" json:"scenarioName" yaml:"scenarioName" xml:"scenarioName"  binding:"required" `
	// Version and ID of the run to update results to
	ScenarioID manifest.VersionedResourceID `json:"scenarioID" yaml:"scenarioID"`

	// Name of the resource representing run results
	RunName string `json:"runName" yaml:"runName"`
	// RunID is the Version and UID of the run result
	RunID manifest.VersionedResourceID `json:"runId" yaml:"runId"`

	// Name of the scenario that produced this job
	Name string `form:"name,omitempty" json:"name,omitempty" yaml:"name,omitempty" xml:"name,omitempty" `

	// Labels of the scenario
	Labels manifest.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" `

	// Requirements that a worker must satisfy in order to pick-up this job
	Requirements manifest.LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" `

	// A schedule to run the script
	RunSchedule CronSchedule `form:"schedule" json:"schedule,omitempty" yaml:"schedule,omitempty" xml:"schedule"`

	// Script of a job to be performed by a runner
	Prob ProbManifest `form:"prob" json:"prob" yaml:"prob" xml:"prob" `

	// True if you want the worker to keep temp working directory with run artifacts
	IsKeepDirectory bool `form:"keepDir" json:"keepDir" yaml:"keepDir" xml:"keepDir" `
}

// func scenarioToRunnable(run Result, scenario Scenario) RunScenarioJob {
// 	return RunScenarioJob{
// 		Name:       scenarioMeta.Name,
// 		ScenarioID: scenarioMeta.Name,

// 		Requirements: scenario.Requirements,
// 		RunSchedule:  scenario.RunSchedule,
// 		Prob:         scenario.Prob,

// 		Labels: manifest.MergeLabels(
// 			scenarioMeta.Labels,
// 			run.Labels,
// 		),

// 		RunID:   run.GetVersionedID(),
// 		RunName: run.Name,
// 	}
// }

func UnmarshalJob(data []byte) (result Job, err error) {
	// err = yaml.Unmarshal(data, &result)
	err = json.Unmarshal(data, &result)
	return
}

func MarshalJob(job Job) ([]byte, error) {
	// return yaml.Marshal(&runScenario)
	return json.Marshal(&job)
}
