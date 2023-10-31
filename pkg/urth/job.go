package urth

// // RunScenarioJob represents a job enqueued for to be picked by a qualified worker
// type RunScenarioJob struct {
// 	// Labels of the scenario
// 	Labels Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" `

// 	// Requirements for a set of labels that a worker must satisfy
// 	Requirements LabelSelector `form:"requirements" json:"requirements,omitempty" yaml:"requirements,omitempty" xml:"requirements" `

// 	// A schedule to run the script
// 	RunSchedule CronSchedule `form:"schedule" json:"schedule,omitempty" yaml:"schedule,omitempty" xml:"schedule"`

// 	// ID and version of the scenario that this results were produced for
// 	ScenarioID VersionedResourceId `form:"play_id" json:"play_id" yaml:"play_id" xml:"play_id"  binding:"required" `

// 	// Script of a job to be performed by a runner
// 	Script ScenarioScript `form:"script" json:"script" yaml:"script" xml:"script" `

// 	// True if you want the worker to keep temp working directory with run artifacts
// 	IsKeepDirectory bool `form:"keepDir" json:"keepDir" yaml:"keepDir" xml:"keepDir" `
// }

// func ScenarioToRunnable(scenario Scenario) RunScenarioJob {
// 	return RunScenarioJob{
// 		Labels:       scenario.Labels,
// 		Requirements: scenario.Requirements,
// 		RunSchedule:  scenario.RunSchedule,
// 		ScenarioID:   scenario.GetVersionedID(),
// 		Script:       scenario.Script,
// 	}
// }

// func UnmarshalJobYAML(data []byte) (RunScenarioJob, error) {
// 	var value RunScenarioJob
// 	err := yaml.Unmarshal(data, &value)

// 	return value, err
// }

// func MarshalJobYAML(runScenario RunScenarioJob) ([]byte, error) {
// 	return yaml.Marshal(&runScenario)
// }
