package pypuppeteer_prob

import (
	"context"
	"fmt"
	"log"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

const (
	Kind           urth.ScenarioKind = "pypuppeteer"
	ScriptMimeType                   = "text/x-python"
)

func init() {
	// Ignore double registration error
	_ = runner.RegisterRunnerKind(Kind, RunScript)
}

func RunScript(ctx context.Context, scriptContent []byte, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactValue, error) {
	log.Print("FIXME: PyPuppeteer scenarios are not implemented....yet")

	return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("not implemented yet")
}
