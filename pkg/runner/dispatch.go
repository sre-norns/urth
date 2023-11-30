package runner

import (
	"context"
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
)

var (
	ErrNilRunner = fmt.Errorf("prob run function is nil")
)

type PuppeteerOptions struct {
	Headless        bool
	PageWaitSeconds int

	WorkingDirectory string
	KeepTempDir      bool
	TempDirPrefix    string
}

type HttpOptions struct {
	CaptureResponseBody bool
	CaptureRequestBody  bool
	IgnoreRedirects     bool
}

type HarOptions struct {
	CompareWithOriginal bool
}

type RunOptions struct {
	Puppeteer PuppeteerOptions
	Http      HttpOptions
	Har       HarOptions
}

type ScriptRunner func(context.Context, []byte, RunOptions) (urth.FinalRunResults, []urth.ArtifactValue, error)

type ProbRegistration struct {
	// Sem-version of the prober module loaded
	Version string

	// Mime type of the script.
	ContentType string

	// Function to execute a script
	RunFunc ScriptRunner
}

// Registrar of Probing modules
var kindRunnerMap = map[urth.ScenarioKind]ProbRegistration{}

// Register new kind of prob
func RegisterProbKind(kind urth.ScenarioKind, probInfo ProbRegistration) error {
	if probInfo.RunFunc == nil {
		return ErrNilRunner
	}

	// TODO: Should be return an error?
	kindRunnerMap[kind] = probInfo
	return nil
}

// Unregister given prober kind
func UnregisterProbKind(kind urth.ScenarioKind) error {
	delete(kindRunnerMap, kind)

	return nil
}

// List all registered probers
// Note: function makes a copy of the module list to avoid accidental modification of registration info
func ListProbs() map[urth.ScenarioKind]ProbRegistration {
	result := make(map[urth.ScenarioKind]ProbRegistration, len(kindRunnerMap))
	for kind, info := range kindRunnerMap {
		result[kind] = info
	}
	return result
}

// Execute a single scenario
func Play(ctx context.Context, script *urth.ScenarioScript, options RunOptions) (urth.FinalRunResults, []urth.ArtifactValue, error) {
	if script == nil {
		return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("no script to run")
	}

	if len(script.Kind) == 0 {
		return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("no script Kind specified")
	}

	probInfo, ok := kindRunnerMap[script.Kind]
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("unsupported script kind: %v", script.Kind)
	}

	return probInfo.RunFunc(ctx, script.Content, options)
}
