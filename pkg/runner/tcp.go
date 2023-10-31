package runner

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"

	"github.com/sre-norns/urth/pkg/urth"
)

func runTcpPortScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	log.Println("fondling TCP port: ")
	host, port, err := net.SplitHostPort(strings.TrimSpace(string(scriptContent)))
	if err != nil {
		log.Println("...failed to parse address: ", err)
		return urth.NewRunResults(urth.RunFinishedError), nil
	}
	log.Printf("...host=%q port%q", host, port)

	addr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%s", host, port))
	if err != nil {
		log.Println("\tfailed to resolve address: ", err)
		return urth.NewRunResults(urth.RunFinishedError), nil
	}

	// con, err := net.Dial("tcp4", string(script.Content))
	con, err := net.DialTCP("tcp", nil, addr)
	if err != nil {
		log.Println("...failed: ", err)
		return urth.NewRunResults(urth.RunFinishedFailed), nil
	}
	defer con.Close()

	return urth.NewRunResults(urth.RunFinishedSuccess), nil
}
