package tcp_prob

import (
	"context"
	"fmt"
	"net"
	"runtime/debug"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

const (
	Kind           = urth.ProbKind("tcp")
	ScriptMimeType = "text/plain"
)

type Spec struct {
	Port int
	Host string
}

type TcpProbSpec1 struct {
	Aggregation string // All / Any
	Address     []Spec
	Payload     []byte
}

func init() {
	moduleVersion := "(unknown)"
	bi, ok := debug.ReadBuildInfo()
	if ok {
		moduleVersion = bi.Main.Version
	}

	// Ignore double registration error
	_ = runner.RegisterProbKind(
		Kind,
		&Spec{},
		runner.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		},
	)
}

func RunScript(ctx context.Context, probSpec any, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	var runLog runner.RunLog
	prob, ok := probSpec.(*Spec)
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), runLog.Package(), fmt.Errorf("invalid spec")
	}

	runLog.Log("fondling TCP port")
	// host, port, err := net.SplitHostPort(strings.TrimSpace(string(scriptContent)))
	// if err != nil {
	// 	runLog.Log("failed to parse address: ", err)
	// 	return urth.NewRunResults(urth.RunFinishedError), runLog.Package(), nil
	// }
	runLog.Logf("script parsed: host=%q port=%v", prob.Host, prob.Port)

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%v", prob.Host, prob.Port))
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
