package httpparser

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRequestOption func(req *http.Request)

func WithHeaders(headers map[string]string) mockRequestOption {
	return func(req *http.Request) {
		if req.Header == nil {
			req.Header = make(http.Header, len(headers))
		}
		for key, value := range headers {
			req.Header.Add(key, value)
		}
	}
}

func mockRequest(t *testing.T, verb, url string, options ...mockRequestOption) TestRequest {
	t.Helper()
	req, err := http.NewRequest(verb, url, nil)
	if !assert.NoError(t, err) {
		t.Fatalf("failed to create mock request: %v", err)
	}

	for _, option := range options {
		option(req)
	}

	return TestRequest{
		Request: req,
	}
}

func TestParser(t *testing.T) {
	testCases := map[string]struct {
		input       string
		expect      []TestRequest
		expectError bool
	}{
		"empty-input": {
			input:  "",
			expect: []TestRequest{},
		},
		"comments-only-input": {
			input:  "###",
			expect: []TestRequest{},
		},
		"short-url-input": {
			input: "go.dev/test",
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
			},
		},
		"short-http-url-input": {
			input: "go.dev:80/test",
			expect: []TestRequest{
				mockRequest(t, "GET", "http://go.dev:80/test"),
			},
		},
		"ip-input": {
			input: "192.10.0.1:80/test",
			expect: []TestRequest{
				mockRequest(t, "GET", "http://192.10.0.1:80/8321/test"),
			},
		},
		"full-url-input": {
			input: "http://localhost:8321/test",
			expect: []TestRequest{
				mockRequest(t, "GET", "http://localhost:8321/test"),
			},
		},
		"url-input-with-leading-comment": {
			input: `# This is our test endpoint
					http://localhost:8321/test
					`,
			expect: []TestRequest{mockRequest(t, "GET", "http://localhost:8321/test")},
		},
		"url-trailing-comment": {
			input: `http://localhost:8321/test # This is our test endpoint
					`,
			expect: []TestRequest{mockRequest(t, "GET", "http://localhost:8321/test")},
		},
		"multiple-short-urls": {
			input: `go.dev/test
					golang.org/something/else`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
				mockRequest(t, "GET", "https://golang.org/something/else"),
			},
		},
		"multiple-short-urls-with-comments": {
			input: `# this it url1
					go.dev/test

					#this is url 2
					http://golang.org/something/else # Other trailing comment
					
					#and that is just a trailing comment for fun
					`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
				mockRequest(t, "GET", "http://golang.org/something/else"),
			},
		},

		// Proper request format:
		"short_http_request-headers": {
			input: `GET go.dev/test`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
			},
		},
		"full_http_request-headers": {
			input: `GET go.dev/test HTTP/1.1`,
			expect: []TestRequest{
				mockRequest(t, "GET", "http://go.dev/test"),
			},
		},
		"full_http_request+host-header": {
			input: `POST /static/shared/logo/go-white.svg HTTP/1.1
					Host: pkg.go.dev
			`,
			expect: []TestRequest{
				mockRequest(t, "POST", "http://go.dev/static/shared/logo/go-white.svg"),
			},
		},
		"short_http_request+host-header": {
			input: `GET /static/shared/logo/go-white.svg
					Host: pkg.go.dev
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "http://go.dev/static/shared/logo/go-white.svg"),
			},
		},

		"short_http+spaceed-header": {
			input: `GET pkg.go.dev/static/shared/logo/go-white.svg
					Content-type : shrooms
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/static/shared/logo/go-white.svg", WithHeaders(map[string]string{"Content-type": "shrooms"})),
			},
		},

		// Not so well formed request
		"ambiguous-requests": {
			input: `GET /test`,
			expect: []TestRequest{
				mockRequest(t, "GET", "//test"),
			},
		},
		"ambiguous-requests-2": {
			input: `GET test`,
			expect: []TestRequest{
				mockRequest(t, "GET", "//test"),
			},
		},

		"ill-formed-request": {
			input: `GET:path
					Content-type:shrooms
			`,
			expectError: true,
		},

		"ill-formed-request_unordered": {
			input: `
			Content-type: shrooms
			GET /path
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "//path", WithHeaders(map[string]string{"Content-type": "shrooms"})),
			},
			expectError: true,
		},

		"multiline_script": {
			input: `GET pkg.go.dev/static/shared/logo/go-white.svg

					### 
					POST /blogs/
					Host: dev.to
					Content-type : shrooms
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/static/shared/logo/go-white.svg"),
				mockRequest(t, "POST", "https://dev.to/blogs/", WithHeaders(map[string]string{"Content-type": "shrooms", "Host": "dev.to"})),
			},
		},

		"multiline_script+custom-verb": {
			input: `
			GET pkg.go.dev/static/shared/logo/go-white.svg
			###
			FONDLE /widgets 
			Host: either.io 

			### 
			POST /blogs/
			Host: dev.to
			Content-type : shrooms
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/static/shared/logo/go-white.svg"),
				mockRequest(t, "FONDLE", "https://either.io/widgets"),
				mockRequest(t, "POST", "https://dev.to/blogs/", WithHeaders(map[string]string{"Content-type": "shrooms", "Host": "dev.to"})),
			},
		},

		"multiline_script_with-tailier": {
			input: `
			# Comment

			### Other request
			GET pkg.go.dev/static/shared/logo/go-white.svg

			### 
			POST /blogs/
			Host: dev.to
			Content-type : shrooms

			####
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/static/shared/logo/go-white.svg"),
				mockRequest(t, "POST", "https://dev.to/blogs/", WithHeaders(map[string]string{"Content-type": "shrooms", "Host": "dev.to"})),
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("parser:%s", name), func(t *testing.T) {
			got, err := Parse(strings.NewReader(test.input))
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.Nil(t, err, "expected error: %v", test.expectError)
			}

			if assert.Len(t, got, len(test.expect)) {
				for i, req := range got {
					expected := test.expect[i]
					assert.Equalf(t, expected.Method, req.Method, "request METHOD %d/%d", i+1, len(got))
					assert.Equalf(t, expected.Host, req.Host, "request HOST %d/%d", i+1, len(got))
					assert.Equalf(t, expected.Header, req.Header, "request Header %d/%d", i+1, len(got))
					assert.Equalf(t, expected.URL.Scheme, req.URL.Scheme, "request Scheme %d/%d", i+1, len(got))

					if assert.Len(t, req.Header, len(expected.Header)) {
						for key, header := range req.Header {
							if assert.Contains(t, expected.Header, key) {
								assert.Equal(t, expected.Header[key], header)
							}
						}
					}
				}
			}
		})
	}
}
