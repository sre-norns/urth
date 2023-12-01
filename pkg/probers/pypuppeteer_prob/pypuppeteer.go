package pypuppeteer_prob

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

const (
	Kind           = urth.ScenarioKind("pypuppeteer")
	ScriptMimeType = "text/x-python"
)

func init() {
	moduleVersion := "(unknown)"
	bi, ok := debug.ReadBuildInfo()
	if ok {
		moduleVersion = bi.Main.Version
	}

	// Ignore double registration error
	_ = runner.RegisterProbKind(Kind, runner.ProbRegistration{
		RunFunc:     RunScript,
		ContentType: ScriptMimeType,
		Version:     moduleVersion,
	})
}

func RunScript(ctx context.Context, scriptContent []byte, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	log.Print("FIXME: PyPuppeteer scenarios are not implemented....yet")

	return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("not implemented yet")
}
