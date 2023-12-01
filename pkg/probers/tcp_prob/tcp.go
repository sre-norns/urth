package tcp_prob

import (
	"context"
	"fmt"
	"net"
	"runtime/debug"
	"strings"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

const (
	Kind           = urth.ScenarioKind("tcp")
	ScriptMimeType = "text/plain"
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
	var runLog runner.RunLog

	runLog.Log("fondling TCP port")
	host, port, err := net.SplitHostPort(strings.TrimSpace(string(scriptContent)))

	if err != nil {
		runLog.Log("failed to parse address: ", err)
		return urth.NewRunResults(urth.RunFinishedError), runLog.Package(), nil
	}
	runLog.Logf("script parsed: host=%q port=%q", host, port)

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%s", host, port))
	if err != nil {
		runLog.Log("failed to resolve address: ", err)
		return urth.NewRunResults(urth.RunFinishedError), runLog.Package(), nil
	}
	runLog.Logf("address resolved as: %q", addr)

	con, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		runLog.Log("failed to connect: ", err)
		return urth.NewRunResults(urth.RunFinishedError), runLog.Package(), nil
	}
	runLog.Log("connected successfully")
	defer con.Close()

	return urth.NewRunResults(urth.RunFinishedSuccess), runLog.Package(), nil
}
