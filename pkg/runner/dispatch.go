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

type ScriptRunner func(context.Context, any, *RunLog, RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error)

type ProbRegistration struct {
	// Function to execute a script
	RunFunc ScriptRunner

	// Sem-version of the prober module loaded
	Version string

	// Mime type of the script.
	ContentType string

	// Types of artifacts this prob is expected to produce
	Produce []string
}

// Registrar of Probing modules
var (
	kindRunnerMap = map[urth.ProbKind]ProbRegistration{}
)

// Register new kind of prob
func RegisterProbKind(kind urth.ProbKind, proto any, probInfo ProbRegistration) error {
	if probInfo.RunFunc == nil {
		return ErrNilRunner
	}

	if err := urth.RegisterProbKind(kind, proto); err != nil {
		return err
	}

	// TODO: Should be return an error?
	kindRunnerMap[kind] = probInfo
	return nil
}

// Unregister given prober kind
func UnregisterProbKind(kind urth.ProbKind) error {
	urth.UnregisterProbKind(kind)
	delete(kindRunnerMap, kind)

	return nil
}

// List all registered probers
// Note: function makes a copy of the module list to avoid accidental modification of registration info
func ListProbs() map[urth.ProbKind]ProbRegistration {
	result := make(map[urth.ProbKind]ProbRegistration, len(kindRunnerMap))
	for kind, info := range kindRunnerMap {
		result[kind] = info
	}

	return result
}

// Execute a single scenario
func Play(ctx context.Context, prob urth.ProbManifest, options RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	if prob.Spec == nil {
		return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("no prob spec")
	}

	probInfo, ok := kindRunnerMap[prob.Kind]
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("unsupported script kind: %q", prob.Kind)
	}

	var logger RunLog
	return probInfo.RunFunc(ctx, prob.Spec, &logger, options)
}
