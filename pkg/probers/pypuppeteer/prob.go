package pypuppeteer

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

const (
	Kind           = urth.ProbKind("pypuppeteer")
	ScriptMimeType = "text/x-python"
)

func init() {
	moduleVersion := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = runner.RegisterProbKind(
		Kind,
		"",
		runner.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		})
}

func RunScript(ctx context.Context, prob any, logger *runner.RunLog, options runner.RunOptions) (urth.ResultStatus, []urth.ArtifactSpec, error) {
	logger.Log("FIXME: PyPuppeteer scenarios are not implemented....yet")

	return urth.NewRunResults(urth.RunFinishedError), nil, fmt.Errorf("not implemented yet")
}
