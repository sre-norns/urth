package runner

import (
	"context"
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
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

type ScriptRunner func(context.Context, []byte, RunOptions) (urth.FinalRunResults, error)

var kindRunnerMap = map[urth.ScenarioKind]ScriptRunner{
	urth.TcpPortCheckKind: runTcpPortScript,
	urth.HttpGetKind:      runHttpRequestScript,
	urth.HarKind:          runHarScript,
	urth.PuppeteerKind:    runPuppeteerScript,
	urth.PyPuppeteerKind:  runPyPuppeteerScript,
}

// Execute a single scenario run
func Play(ctx context.Context, script urth.ScenarioScript, options RunOptions) (urth.FinalRunResults, error) {
	if len(script.Kind) == 0 {
		return urth.NewRunResults(urth.RunFinishedError), fmt.Errorf("no script Kind specified")
	}

	runner, ok := kindRunnerMap[script.Kind]
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), fmt.Errorf("unsupported script kind: %v", script.Kind)
	}

	return runner(ctx, script.Content, options)
}
