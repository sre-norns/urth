package urth

// Common prefix for Urth managed resources

const (
	LabelsPrefix = "urth/"

	LabelWorkerCapPrefix     = LabelsPrefix + "capability."
	LabelWorkerCapProbPrefix = LabelWorkerCapPrefix + "prob."

	// Well-known worker labels:
	LabelWorkerOS           = LabelsPrefix + "worker.os"
	LabelWorkerArch         = LabelsPrefix + "worker.arch"
	LabelWorkerBuildVersion = LabelsPrefix + "worker.build.version"
	LabelWorkerName         = LabelsPrefix + "worker.name"
	LabelWorkerUID          = LabelsPrefix + "worker.uid"
	LabelWorkerVersion      = LabelsPrefix + "worker.version"

	// Well-known runner labels:
	LabelRunnerName    = LabelsPrefix + "runner.name"
	LabelRunnerUID     = LabelsPrefix + "runner.uid"
	LabelRunnerVersion = LabelsPrefix + "runner.version"

	// Well-known scenario labels:
	LabelScenarioName    = LabelsPrefix + "scenario.name"
	LabelScenarioUID     = LabelsPrefix + "scenario.uid"
	LabelScenarioVersion = LabelsPrefix + "scenario.version"
	LabelScenarioKind    = LabelsPrefix + "scenario.kind"

	// Well-known result labels:
	LabelResultName    = LabelsPrefix + "result.name"
	LabelResultUID     = LabelsPrefix + "result.uid"
	LabelResultVersion = LabelsPrefix + "result.version"

	LabelResultJobState = LabelsPrefix + "result.state"
	LabelResultStatus   = LabelsPrefix + "result.result"

	LabelResultMessageId = "run.messageId"

	// Well-known artifact labels:
	LabelArtifactKind = LabelsPrefix + "artifact.kind"
	LabelArtifactMime = LabelsPrefix + "artifact.mime"
)
