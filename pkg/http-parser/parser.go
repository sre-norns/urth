package httpparser

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type TestRequest struct {
	*http.Request

	// TODO: Add "expectation"
	// TODO: Add variables
	// TODO: add body
}

func parseURL(uri string) (*url.URL, error) {
	if !strings.Contains(uri, "://") && !strings.HasPrefix(uri, "//") {
		uri = "//" + uri
	}

	url, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	if url.Scheme == "" {
		url.Scheme = "http"
		if !strings.HasSuffix(url.Host, ":80") && !strings.HasSuffix(url.Host, ":http") {
			url.Scheme = "https"
		}
	}

	return url, nil
}

type requestParser struct {
	_requests []TestRequest

	method       string
	path         string
	protoVersion string
	headers      map[string]string

	// TODO: Body
}

func (p *requestParser) Requests() ([]TestRequest, error) {
	if err := p.onFinishRequest(); err != nil {
		return nil, err
	}

	return p._requests, nil
}

func (p *requestParser) reset() error {
	p.method = ""
	p.path = ""
	p.protoVersion = ""
	p.headers = make(map[string]string)

	return nil
}

func (p *requestParser) onFinishRequest() error {
	if p.path == "" {
		return p.reset()
	}

	targetUrl, err := parseURL(p.path)
	if err != nil {
		return fmt.Errorf("failed to parse target URL: %w", err)
	}

	if p.method == "" {
		p.method = "GET"
	}

	result, err := http.NewRequest(p.method, targetUrl.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if p.protoVersion != "" {
		result.Proto = p.protoVersion
	}

	if result.Header == nil {
		result.Header = make(http.Header)
	}

	for k, v := range p.headers {
		result.Header.Add(k, v)
	}

	p._requests = append(p._requests, TestRequest{
		Request: result,
	})

	return p.reset()
}

func (p *requestParser) onLine(line string) error {
	parts := strings.Split(string(line), "#") // Split out content from a trailing line comments: <content> # comment
	action := strings.TrimSpace(parts[0])
	if action == "" {
		// Line with no leading part, need to check if there is a comment block:
		if len(parts) > 1 && strings.HasPrefix(parts[1], "###") {
			return p.onFinishRequest()
		}
		return nil
	}

	actionComponents := strings.Split(string(parts[0]), " ")
	for i, component := range actionComponents {
		actionComponents[i] = strings.TrimSpace(component)
	}

	// If there is a single word - that's url for short request form
	if len(actionComponents) == 1 {
		p.path = parts[0]
		return nil
	}

	// if strings.HasSuffix(actionComponents[0], ":") || strings.Contains(p.path, " ") { // not Header but verb?
	// }

	// if strings.HasPrefix(action, "#") {
	// 	return nil
	// }

	return nil
}

func Parse(script io.Reader) ([]TestRequest, error) {
	parser := requestParser{}
	scanner := bufio.NewScanner(script)

	for scanner.Scan() {
		if err := parser.onLine(scanner.Text()); err != nil {
			return nil, fmt.Errorf("failed to parse request script: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading request script: %w", err)
	}

	return parser.Requests()
}
