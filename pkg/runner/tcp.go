package runner

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/sre-norns/urth/pkg/urth"
)

func runTcpPortScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	var runLog RunLog
	runLog.Log("fondling TCP port")
	host, port, err := net.SplitHostPort(strings.TrimSpace(string(scriptContent)))

	if err != nil {
		runLog.Log("failed to parse address: ", err)
		return NewRunResultsWithLog(urth.RunFinishedError, &runLog), nil
	}
	runLog.Logf("...host=%q port=%q", host, port)

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%s", host, port))
	if err != nil {
		runLog.Log("failed to resolve address: ", err)
		return NewRunResultsWithLog(urth.RunFinishedError, &runLog), nil
	}

	con, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		runLog.Log("failed to connect: ", err)
		return NewRunResultsWithLog(urth.RunFinishedFailed, &runLog), nil
	}
	defer con.Close()

	return NewRunResultsWithLog(urth.RunFinishedSuccess, &runLog), nil
}
