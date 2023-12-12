package tcp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"runtime/debug"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
)

const (
	Kind           = urth.ProbKind("tcp")
	ScriptMimeType = "text/plain"
)

type Spec struct {
	Net  string // tcp, tcp4, tcp6, udp, udp4, udp6...
	Port string
	Host string
}

type TcpProbSpec1 struct {
	Aggregation string // All / Any / First
	Address     []Spec
	Payload     []byte
	Expects     []byte
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

func RunScript(ctx context.Context, probSpec any, logger *runner.RunLog, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	prob, ok := probSpec.(*Spec)
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), fmt.Errorf("%w: got %q, expected %q", wyrd.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}
	if prob.Net == "" {
		prob.Net = "tcp"
	}

	logger.Log("fondling TCP port")
	logger.Logf("prob: net=%q host=%q port=%v", prob.Net, prob.Host, prob.Port)
	resolver := net.DefaultResolver

	// TODO: Trace
	addrs, err := resolver.LookupHost(ctx, prob.Host)
	if err != nil {
		logger.Log("failed to resolve host name: ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}
	logger.Logf("hostname resolved into %d address(es)", len(addrs))

	// TODO: Trace
	port, err := resolver.LookupPort(ctx, prob.Net, prob.Port)
	if err != nil {
		logger.Log("failed to resolve host name: ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}
	logger.Logf("service port resolved as: %d", port)

	ips := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		ip, err := netip.ParseAddr(addr)
		if err == nil {
			ips = append(ips, ip)
		}
	}
	logger.Logf("... %d non null IPs", len(ips))

	// TODO: Is it ok to have no valid addresses?
	if len(ips) == 0 {
		logger.Log("no non-null IP addresses for host: ", prob.Host)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	ip := ips[0] // Implement strategy selector. For now copy what GO-libs do
	addr := &net.TCPAddr{
		IP:   ip.AsSlice(),
		Port: port,
		Zone: ip.Zone(),
	}

	logger.Logf("address resolved as: %q", addr)

	// TODO: Trace
	con, err := net.DialTCP(prob.Net, nil, addr)
	if err != nil {
		logger.Log("failed to connect: ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}
	logger.Log("connected successfully")
	defer con.Close()

	return urth.NewRunResults(urth.RunFinishedSuccess), logger.Package(), nil
}
