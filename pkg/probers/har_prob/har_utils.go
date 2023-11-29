package har_prob

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/chromedp/cdproto/har"

	httpparser "github.com/sre-norns/urth/pkg/http-parser"
)

func entryToRequest(request *har.Request) (httpparser.TestRequest, error) {
	if request == nil {
		return httpparser.TestRequest{}, nil
	}

	url, err := url.Parse(request.URL)
	if err != nil {
		return httpparser.TestRequest{}, err
	}

	query := url.Query()
	for _, q := range request.QueryString {
		query.Add(q.Name, q.Value)
	}
	url.RawQuery = query.Encode()

	r := &http.Request{
		Proto:  request.HTTPVersion,
		Method: request.Method,
		URL:    url,
		Header: make(http.Header),
	}

	for _, h := range request.Headers {
		r.Header.Add(h.Name, h.Value)
	}

	if request.PostData != nil {
		r.Body = io.NopCloser(strings.NewReader(request.PostData.Text))
		// Set content-type?
		// update query params?
	}

	return httpparser.TestRequest{
		Request: r,
	}, nil
}

func ConvertHarToHttpTester(entries []*har.Entry) ([]httpparser.TestRequest, error) {
	var requests []httpparser.TestRequest

	for _, entry := range entries {
		r, err := entryToRequest(entry.Request)
		if err != nil {
			return nil, err
		}
		if r.Request != nil {
			requests = append(requests, r)
		}
	}

	return requests, nil
}

func UnmarshalHAR(reader io.Reader) (har.HAR, error) {
	var harLog har.HAR

	dec := json.NewDecoder(reader)
	if err := dec.Decode(&harLog); err != nil {
		return harLog, fmt.Errorf("failed to unmarshal HAR log: %w", err)
	}

	return harLog, nil
}
