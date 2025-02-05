package pypuppeteer

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sre-norns/urth/pkg/prob"
)

const (
	Kind           = prob.Kind("pypuppeteer")
	ScriptMimeType = "text/x-python"
)

func init() {
	moduleVersion := "devel"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = prob.RegisterProbKind(
		Kind,
		"",
		prob.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		})
}

func RunScript(ctx context.Context, probSpec any, config prob.RunOptions, registry *prometheus.Registry, logger log.Logger) (prob.RunStatus, []prob.Artifact, error) {
	logger.Log("FIXME: PyPuppeteer scenarios runner is not implemented....yet")

	return prob.RunFinishedError, nil, fmt.Errorf("not implemented yet")
}
