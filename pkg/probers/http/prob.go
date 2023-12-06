package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"runtime/debug"
	"strings"
	"time"

	httpparser "github.com/sre-norns/urth/pkg/http-parser"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"

	"github.com/google/martian/har"
)

const (
	Kind           = urth.ProbKind("http")
	ScriptMimeType = "application/http"
)

type Spec struct {
	FollowRedirects bool
	Script          string
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
		})
}

type httpRequestTracer struct {
	tracer *httptrace.ClientTrace

	dnsResolutionStarted  time.Time
	dnsResolutionFinished time.Time

	tlsStarted  time.Time
	tlsFinished time.Time

	connectionStarted  time.Time
	connectionFinished time.Time

	timeHeaderWritten    time.Time
	timeRequestWritten   time.Time
	timeResponseReceived time.Time
}

func newHttpRequestTracer(logger *runner.RunLog) *httpRequestTracer {
	result := &httpRequestTracer{}

	tracer := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			logger.Log("DNS resolving: ", info.Host)
			result.dnsResolutionStarted = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			logger.Log("DNS resolved: ", info.Addrs)
			result.dnsResolutionFinished = time.Now()
		},

		TLSHandshakeStart: func() {
			logger.Log("TLS handshake started")
			result.tlsStarted = time.Now()
		},

		TLSHandshakeDone: func(tlsState tls.ConnectionState, err error) {
			logger.Logf("TLS handshake done: err=%v: %v", err, tlsState)
			result.tlsFinished = time.Now()
		},

		ConnectStart: func(network, addr string) {
			logger.Logf("net=%q connecting to: addr=%q", network, addr)
			result.connectionStarted = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			logger.Logf("net=%q connected to: addr=%q, err=%v", network, addr, err)
			result.connectionFinished = time.Now()
		},

		WroteHeaders: func() {
			logger.Log("done writing request headers")
			result.timeHeaderWritten = time.Now()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			logger.Log("done writing request: err=", info.Err)
			result.timeRequestWritten = time.Now()
		},
		GotConn: func(connInfo httptrace.GotConnInfo) {
			logger.Log("established connection: ", connInfo)
		},

		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			logger.Logf("got response %d: %+v", code, header)
			return nil
		},

		GotFirstResponseByte: func() {
			logger.Log("response data received")
			result.timeResponseReceived = time.Now()
		},
	}

	result.tracer = tracer

	return result
}

func (t *httpRequestTracer) TraceRequest(req *http.Request) *http.Request {
	return req.WithContext(httptrace.WithClientTrace(req.Context(), t.tracer))
}

func formatRequest(req *http.Request) string {
	result := strings.Builder{}

	fmt.Fprintf(&result, "%v %v %v\n", req.Method, req.URL.Path, req.Proto)
	for header, value := range req.Header {
		fmt.Fprintf(&result, "%v: %v\n", header, strings.Join(value, "; "))
	}

	return result.String()
}

func formatResponse(resp *http.Response) string {
	result := strings.Builder{}

	fmt.Fprintf(&result, "%v %v\n", resp.Proto, resp.Status)
	for header, value := range resp.Header {
		fmt.Fprintf(&result, "%v: %v\n", header, strings.Join(value, "; "))
	}

	return result.String()
}

func RunHttpRequests(ctx context.Context, logger *runner.RunLog, requests []httpparser.TestRequest, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	harLogger := har.NewLogger()
	harLogger.SetOption(har.BodyLogging(options.Http.CaptureResponseBody))
	harLogger.SetOption(har.PostDataLogging(options.Http.CaptureRequestBody))

	outcome := urth.RunFinishedSuccess
	client := http.Client{}
	tracer := newHttpRequestTracer(logger)

	for i, req := range requests {
		id := fmt.Sprintf("%d", i)
		logger.Logf("HTTP Request %d / %d\n%v\n", i+1, len(requests), formatRequest(req.Request))

		if err := harLogger.RecordRequest(id, req.Request); err != nil {
			logger.Log("...failed to record request: ", err)
			return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
		}

		res, err := client.Do(tracer.TraceRequest(req.Request))
		if err != nil {
			logger.Log("...failed: ", err)
			return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
		}

		if err := harLogger.RecordResponse(id, res); err != nil {
			logger.Log("...failed to record response: ", err)
			return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
		}

		logger.Logf("Response:\n%v\n", formatResponse(res))

		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			logger.Log("...failed while reading response body: ", err)
		}
		res.Body.Close()

		// TODO: Inspect headers for well known TraceID
		// TODO: Capture HTTP log

		if res.StatusCode >= 400 {
			outcome = urth.RunFinishedFailed
			break
		}
	}

	har := harLogger.ExportAndReset()
	harData, err := json.Marshal(har)
	if err != nil {
		logger.Log("...error: failed to serialize HAR file ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	return urth.NewRunResults(outcome),
		[]urth.ArtifactSpec{
			logger.ToArtifact(),
			{
				Rel:      "har",
				MimeType: "application/json",
				Content:  harData,
			},
		}, nil
}

func RunScript(ctx context.Context, probSpec any, logger *runner.RunLog, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	prob, ok := probSpec.(*Spec)
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), fmt.Errorf("invalid spec")
	}

	logger.Log("fondling HTTP")
	requests, err := httpparser.Parse(strings.NewReader(prob.Script))
	if err != nil {
		logger.Log("failed: ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	return RunHttpRequests(ctx, logger, requests, options)
}
