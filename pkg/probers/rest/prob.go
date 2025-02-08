package rest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"reflect"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	httpparser "github.com/sre-norns/urth/pkg/http-parser"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/google/martian/har"
)

const (
	Kind           = prob.Kind("rest")
	ScriptMimeType = "application/http"
)

type Spec struct {
	FollowRedirects bool   `json:"followRedirects,omitempty" yaml:"followRedirects,omitempty"`
	Script          string `json:"script,omitempty" yaml:"script,omitempty"`
}

func init() {
	moduleVersion := "devel"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = prob.RegisterProbKind(
		Kind,
		&Spec{},
		prob.ProbRegistration{
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

func newHttpRequestTracer(logger log.Logger) *httpRequestTracer {
	result := &httpRequestTracer{}

	tracer := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			logger.Log("DNS resolving", "host", info.Host)
			result.dnsResolutionStarted = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			logger.Log("DNS resolved", "address", info.Addrs)
			result.dnsResolutionFinished = time.Now()
		},

		TLSHandshakeStart: func() {
			logger.Log("TLS handshake started")
			result.tlsStarted = time.Now()
		},

		TLSHandshakeDone: func(tlsState tls.ConnectionState, err error) {
			logger.Log("TLS handshake done", "tlsState", tlsState, "err", err)
			result.tlsFinished = time.Now()
		},

		ConnectStart: func(network, addr string) {
			logger.Log("connecting", "addr", addr, "net", network)
			result.connectionStarted = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			logger.Log("connected", "addr", addr, "net", network, "err", err)
			result.connectionFinished = time.Now()
		},

		WroteHeaders: func() {
			logger.Log("done writing request headers")
			result.timeHeaderWritten = time.Now()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			logger.Log("done writing request", "err", info.Err)
			result.timeRequestWritten = time.Now()
		},
		GotConn: func(connInfo httptrace.GotConnInfo) {
			logger.Log("established connection", "info", connInfo)
		},

		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			logger.Log("got response", "code", code, "header", header)
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

func RunHttpRequests(ctx context.Context, requests []httpparser.TestRequest, options prob.RunOptions, logger log.Logger) (prob.RunStatus, []prob.Artifact, error) {
	harLogger := har.NewLogger()
	harLogger.SetOption(har.BodyLogging(options.Http.CaptureResponseBody))
	harLogger.SetOption(har.PostDataLogging(options.Http.CaptureRequestBody))

	outcome := prob.RunFinishedSuccess
	client := http.Client{}
	tracer := newHttpRequestTracer(logger)

	for i, req := range requests {
		id := fmt.Sprintf("%d", i)
		logger.Log(fmt.Sprintf("HTTP Request %d/%d", i+1, len(requests)), "req", formatRequest(req.Request))

		if err := harLogger.RecordRequest(id, req.Request); err != nil {
			logger.Log("...failed to record request: ", err)
			return prob.RunFinishedError, nil, nil
		}

		res, err := client.Do(tracer.TraceRequest(req.Request))
		if err != nil {
			logger.Log("...failed: ", err)
			return prob.RunFinishedError, nil, nil
		}

		if err := harLogger.RecordResponse(id, res); err != nil {
			logger.Log("...failed to record response: ", err)
			return prob.RunFinishedError, nil, nil
		}

		logger.Log("Response:", "resp", formatResponse(res))

		if _, err := io.Copy(io.Discard, res.Body); err != nil {
			logger.Log("...failed while reading response body: ", err)
		}
		res.Body.Close()

		// TODO: Inspect headers for well known TraceID
		// TODO: Capture HTTP log

		if res.StatusCode >= 400 {
			outcome = prob.RunFinishedFailed
			break
		}
	}

	har := harLogger.ExportAndReset()
	harData, err := json.Marshal(har)
	if err != nil {
		logger.Log("...error: failed to serialize HAR file ", err)
		return prob.RunFinishedError, nil, nil
	}

	return outcome,
		[]prob.Artifact{
			{
				Rel:      "har",
				MimeType: "application/json",
				Content:  harData,
			},
		}, nil
}

func RunScript(ctx context.Context, probSpec any, config prob.RunOptions, registry *prometheus.Registry, logger log.Logger) (prob.RunStatus, []prob.Artifact, error) {
	spec, ok := probSpec.(*Spec)
	if !ok {
		return prob.RunFinishedError, nil, fmt.Errorf("%w: got %q, expected %q", manifest.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}

	logger.Log("Parsing scenario", "kind", Kind)
	requests, err := httpparser.Parse(strings.NewReader(spec.Script))
	if err != nil {
		logger.Log("Failed to parse prob script", "kind", Kind, "err", err)
		return prob.RunFinishedError, nil, nil
	}

	logger.Log("running script", "kind", Kind, "count(requests)", len(requests))
	return RunHttpRequests(ctx, requests, config, logger)
}
