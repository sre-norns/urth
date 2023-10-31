package runner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"time"

	httpparser "github.com/sre-norns/urth/pkg/http-parser"
	"github.com/sre-norns/urth/pkg/urth"

	"github.com/google/martian/har"
)

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

func NewHttpRequestTracer() *httpRequestTracer {
	result := &httpRequestTracer{}

	tracer := &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			log.Printf("DNS resolving: %+v", info.Host)
			result.dnsResolutionStarted = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			log.Printf("DNS resolved: %+v", info.Addrs)
			result.dnsResolutionFinished = time.Now()
		},

		TLSHandshakeStart: func() {
			log.Printf("TLS handshake started")
			result.tlsStarted = time.Now()
		},

		TLSHandshakeDone: func(tlsState tls.ConnectionState, err error) {
			log.Printf("TLS handshake done: err=%v: %v", err, tlsState)
			result.tlsFinished = time.Now()
		},

		ConnectStart: func(network, addr string) {
			log.Printf("net=%q connecting to: addr=%q", network, addr)
			result.connectionStarted = time.Now()
		},
		ConnectDone: func(network, addr string, err error) {
			log.Printf("net=%q connected to: addr=%q, err=%v", network, addr, err)
			result.connectionFinished = time.Now()
		},

		WroteHeaders: func() {
			log.Printf("done writing request headers")
			result.timeHeaderWritten = time.Now()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			log.Printf("done writing request: err=%v", info.Err)
			result.timeRequestWritten = time.Now()
		},
		GotConn: func(connInfo httptrace.GotConnInfo) {
			log.Printf("established connection: %+v", connInfo)
		},

		Got1xxResponse: func(code int, header textproto.MIMEHeader) error {
			log.Printf("got response %d: %+v", code, header)
			return nil
		},

		GotFirstResponseByte: func() {
			log.Println("response data received")
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

func runHttpRequests(ctx context.Context, requests []httpparser.TestRequest, options RunOptions) (urth.FinalRunResults, error) {
	harLogger := har.NewLogger()
	harLogger.SetOption(har.BodyLogging(options.Http.CaptureResponseBody))
	harLogger.SetOption(har.PostDataLogging(options.Http.CaptureRequestBody))

	outcome := urth.RunFinishedSuccess
	client := http.Client{}
	tracer := NewHttpRequestTracer()

	for i, req := range requests {
		id := fmt.Sprintf("%d", i)
		log.Printf("HTTP Request %d / %d\n%v\n", i+1, len(requests), formatRequest(req.Request))

		if err := harLogger.RecordRequest(id, req.Request); err != nil {
			log.Println("...failed to record request: ", err)
			return urth.NewRunResults(urth.RunFinishedError), err
		}

		res, err := client.Do(tracer.TraceRequest(req.Request))
		if err != nil {
			log.Println("...failed: ", err)
			return urth.NewRunResults(urth.RunFinishedError), err
		}

		if err := harLogger.RecordResponse(id, res); err != nil {
			log.Println("...failed to record response: ", err)
			return urth.NewRunResults(urth.RunFinishedError), err
		}

		log.Printf("Response:\n%v\n", formatResponse(res))

		if _, err := io.Copy(ioutil.Discard, res.Body); err != nil {
			log.Println("...failed while reading response body: ", err)
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
		log.Println("...error: failed to serialize HAR file", err)
		return urth.NewRunResults(urth.RunFinishedError), err
	}

	return urth.NewRunResults(outcome, urth.WithArtifacts(urth.ArtifactValue{
		Rel:      "har",
		MimeType: "application/json",
		Content:  harData,
	})), nil
}

func runHttpRequestScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	log.Println("fondling HTTP")
	requests, err := httpparser.Parse(bytes.NewReader(scriptContent))
	if err != nil {
		log.Println("...failed: ", err)
		return urth.NewRunResults(urth.RunFinishedError), err
	}

	return runHttpRequests(ctx, requests, options)
}
