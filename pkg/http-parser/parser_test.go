package httpparser

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRequestOption func(req *TestRequest)

func WithHeaders(headers map[string]string) mockRequestOption {
	return func(req *TestRequest) {
		if req.Header == nil {
			req.Header = make(http.Header, len(headers))
		}
		for key, value := range headers {
			req.Header.Add(key, value)
		}
	}
}

// WithHost expects the request to ask for a host other than the one it dials.
func WithHost(host string) mockRequestOption {
	return func(req *TestRequest) {
		req.Host = host
	}
}

func WithName(name string) mockRequestOption {
	return func(req *TestRequest) {
		req.Name = name
	}
}

func WithBody(body string) mockRequestOption {
	return func(req *TestRequest) {
		req.Body = io.NopCloser(strings.NewReader(body))
	}
}

func WithProto(proto string) mockRequestOption {
	return func(req *TestRequest) {
		req.Proto = proto
	}
}

func mockRequest(t *testing.T, verb, url string, options ...mockRequestOption) TestRequest {
	t.Helper()
	req, err := http.NewRequest(verb, url, nil)
	if !assert.NoError(t, err) {
		t.Fatalf("failed to create mock request: %v", err)
	}

	result := TestRequest{Request: req}
	for _, option := range options {
		option(&result)
	}

	return result
}

// bodyOf reads a request's body without consuming it, so a failed assertion can
// still be reported against a request that was already read.
func bodyOf(t *testing.T, req TestRequest) string {
	t.Helper()

	if req.Request == nil || req.Body == nil {
		return ""
	}

	body, err := req.bodyContent()
	require.NoError(t, err)

	return string(body)
}

func TestParser(t *testing.T) {
	testCases := map[string]struct {
		input       string
		expect      []TestRequest
		expectError error
	}{
		"empty-input": {
			input:  "",
			expect: []TestRequest{},
		},
		"blank-lines-only": {
			input:  "\n\n   \n",
			expect: []TestRequest{},
		},
		"comments-only-input": {
			input:  "###",
			expect: []TestRequest{},
		},
		"comment-markers-only": {
			input:  "# a comment\n// another one\n### and a separator\n",
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
				mockRequest(t, "GET", "http://192.10.0.1:80/test"),
			},
		},
		"full-url-input": {
			input: "http://localhost:8321/test",
			expect: []TestRequest{
				mockRequest(t, "GET", "http://localhost:8321/test"),
			},
		},
		"url-with-query": {
			input: "https://go.dev/search?q=urth&limit=10",
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/search?q=urth&limit=10"),
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
		// A `#` that is not preceded by whitespace is part of the URL, not the
		// start of a comment.
		"url-with-fragment": {
			input:  `https://go.dev/app#/route`,
			expect: []TestRequest{mockRequest(t, "GET", "https://go.dev/app#/route")},
		},
		"crlf-line-endings": {
			input: "GET go.dev/test HTTP/1.1\r\nAccept: application/json\r\n",
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test", WithHeaders(map[string]string{"Accept": "application/json"})),
			},
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

		// A bare URL is a whole request, so a script can list endpoints one per
		// line. Anything more than a URL — a method, a header — means the block
		// may go on to have a body.
		"short-urls-listed-with-blank-lines": {
			input: "go.dev/test\n\ngolang.org/other\n\nhttps://go.dev/third\n",
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
				mockRequest(t, "GET", "https://golang.org/other"),
				mockRequest(t, "GET", "https://go.dev/third"),
			},
		},
		"short-urls-with-schemes": {
			input: "http://go.dev/test\nhttps://golang.org/other\n",
			expect: []TestRequest{
				mockRequest(t, "GET", "http://go.dev/test"),
				mockRequest(t, "GET", "https://golang.org/other"),
			},
		},
		// A header that lost its value is a mistake worth reporting, not a URL.
		"misspelled-header": {
			input:       "GET go.dev/test\nContent-Type application/json\n",
			expectError: ErrMalformedHeader,
		},

		// Proper request format:
		"short_http_request-headers": {
			input: `GET go.dev/test`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
			},
		},
		// The protocol version names the wire format, and says nothing about
		// whether the connection is encrypted: scheme selection is unaffected.
		"full_http_request-headers": {
			input: `GET go.dev/test HTTP/1.1`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test"),
			},
		},
		"explicit_proto_version": {
			input: `GET https://go.dev/test HTTP/1.0`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test", WithProto("HTTP/1.0")),
			},
		},
		"unrecognised_proto_version": {
			input:       `GET https://go.dev/test HTTP/9`,
			expectError: ErrMalformedRequestLine,
		},
		"custom-verb": {
			input: `FONDLE https://either.io/widgets`,
			expect: []TestRequest{
				mockRequest(t, "FONDLE", "https://either.io/widgets"),
			},
		},

		// A path-only request line takes its host from the `Host` header, which
		// is the request exactly as it goes on the wire. The header is folded
		// into the URL rather than kept in Header, because Go writes
		// Request.Host and ignores Header["Host"].
		"full_http_request+host-header": {
			input: `POST /static/shared/logo/go-white.svg HTTP/1.1
					Host: pkg.go.dev
			`,
			expect: []TestRequest{
				mockRequest(t, "POST", "https://pkg.go.dev/static/shared/logo/go-white.svg"),
			},
		},
		"short_http_request+host-header": {
			input: `GET /static/shared/logo/go-white.svg
					Host: pkg.go.dev
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://pkg.go.dev/static/shared/logo/go-white.svg"),
			},
		},
		// When the request line names a host too, the header is not redundant:
		// it says dial one host and ask it for another, which is how a single
		// instance behind a load balancer gets probed.
		"host-header-overriding-target": {
			input: `GET http://10.0.0.7:8080/healthz
					Host: api.example.com
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "http://10.0.0.7:8080/healthz", WithHost("api.example.com")),
			},
		},
		"short_http+spaceed-header": {
			input: `GET pkg.go.dev/static/shared/logo/go-white.svg
					Content-type : shrooms
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://pkg.go.dev/static/shared/logo/go-white.svg", WithHeaders(map[string]string{"Content-type": "shrooms"})),
			},
		},
		"repeated-header": {
			input: `GET go.dev/test
					Accept: application/json
					Accept: text/plain
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/test", func(req *TestRequest) {
					req.Header = http.Header{"Accept": []string{"application/json", "text/plain"}}
				}),
			},
		},

		// Not so well formed request
		"ambiguous-request_bare-word-target": {
			input: `GET test`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://test"),
			},
		},
		// A path with no host is not a request anyone can send. Reporting it
		// here names the actual fault, where letting it through would surface
		// later as an unexplained connection failure.
		"path-only-request_without-host-header": {
			input:       `GET /test`,
			expectError: ErrNoTargetHost,
		},

		"ill-formed-request": {
			input: `GET:path
					Content-type:shrooms
			`,
			expectError: ErrMalformedRequestLine,
		},

		// A header cannot precede the request line it belongs to. The whole
		// script fails: the alternative is guessing which request the stray
		// header was meant for.
		"ill-formed-request_unordered": {
			input: `
			Content-type: shrooms
			GET /path
			`,
			expectError: ErrMalformedRequestLine,
		},

		"multiline_script": {
			input: `GET pkg.go.dev/static/shared/logo/go-white.svg

					###
					POST /blogs/
					Host: dev.to
					Content-type : shrooms
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://pkg.go.dev/static/shared/logo/go-white.svg"),
				mockRequest(t, "POST", "https://dev.to/blogs/", WithHeaders(map[string]string{"Content-type": "shrooms"})),
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
				mockRequest(t, "GET", "https://pkg.go.dev/static/shared/logo/go-white.svg"),
				mockRequest(t, "FONDLE", "https://either.io/widgets"),
				mockRequest(t, "POST", "https://dev.to/blogs/", WithHeaders(map[string]string{"Content-type": "shrooms"})),
			},
		},

		"multiline_script_with-trailer": {
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
				mockRequest(t, "GET", "https://pkg.go.dev/static/shared/logo/go-white.svg", WithName("Other request")),
				mockRequest(t, "POST", "https://dev.to/blogs/", WithHeaders(map[string]string{"Content-type": "shrooms"})),
			},
		},

		// Request names
		"named-request": {
			input: `### Request Separation & Title
					# @name GetUserProfile
					GET https://go.dev/api/users/1
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/api/users/1", WithName("GetUserProfile")),
			},
		},
		"named-request_assignment-form": {
			input: `# @name = GetUserProfile
					GET https://go.dev/api/users/1
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/api/users/1", WithName("GetUserProfile")),
			},
		},
		"unknown-directives-are-ignored": {
			input: `# @no-redirect
					# @timeout 30
					// @no-cookie-jar
					GET https://go.dev/api/users/1
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/api/users/1"),
			},
		},

		// Request bodies
		"request-with-body": {
			input: "POST https://httpbin.org/post\nContent-Type: application/json\n\n{\n  \"name\": \"John Doe\",\n  \"role\": \"Developer\"\n}\n",
			expect: []TestRequest{
				mockRequest(t, "POST", "https://httpbin.org/post",
					WithHeaders(map[string]string{"Content-Type": "application/json"}),
					WithBody("{\n  \"name\": \"John Doe\",\n  \"role\": \"Developer\"\n}"),
				),
			},
		},
		// A `#` inside a body is payload: bodies are kept byte for byte.
		"body-is-kept-verbatim": {
			input: "POST https://httpbin.org/post\n\n# not a comment\n  indented # not a comment either\n",
			expect: []TestRequest{
				mockRequest(t, "POST", "https://httpbin.org/post",
					WithBody("# not a comment\n  indented # not a comment either"),
				),
			},
		},
		"body-blank-lines-are-trimmed": {
			input: "POST https://httpbin.org/post\n\n\nfirst\n\nlast\n\n\n",
			expect: []TestRequest{
				mockRequest(t, "POST", "https://httpbin.org/post", WithBody("first\n\nlast")),
			},
		},
		"body-ends-at-the-separator": {
			input: "POST https://httpbin.org/post\n\nthe body\n###\nGET https://go.dev/\n",
			expect: []TestRequest{
				mockRequest(t, "POST", "https://httpbin.org/post", WithBody("the body")),
				mockRequest(t, "GET", "https://go.dev/"),
			},
		},

		// Response handlers verify a response, which this parser does not model.
		// They still have to be recognised, or they end up in the body.
		"inline-response-handler-is-skipped": {
			input: "POST https://httpbin.org/post\n" +
				"\n" +
				"{\"a\": 1}\n" +
				"\n" +
				"> {%\n" +
				"client.test(\"status\", function() {\n" +
				"  client.assert(response.status === 200, \"expected 200\");\n" +
				"});\n" +
				"%}\n",
			expect: []TestRequest{
				mockRequest(t, "POST", "https://httpbin.org/post", WithBody(`{"a": 1}`)),
			},
		},
		"response-handler-file-is-skipped": {
			input: "GET https://go.dev/\n\n> ./handler.js\n",
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/"),
			},
		},
		"response-redirect-is-skipped": {
			input: "GET https://go.dev/\n\n>>! ./response.json\n",
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/"),
			},
		},
		"external-body-file-is-rejected": {
			input:       "POST https://httpbin.org/post\n\n< ./body.json\n",
			expectError: ErrUnsupported,
		},

		// Variables
		"variables": {
			input: `@base_url = https://go.dev
					@auth_token = s3cret

					###
					# @name GetUserProfile
					GET {{base_url}}/api/users/1
					Authorization: Bearer {{auth_token}}
					Accept: application/json
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/api/users/1",
					WithName("GetUserProfile"),
					WithHeaders(map[string]string{
						"Authorization": "Bearer s3cret",
						"Accept":        "application/json",
					}),
				),
			},
		},
		"variables-compose": {
			input: `@host = go.dev
					@base_url = https://{{host}}/api
					GET {{base_url}}/users/1
			`,
			expect: []TestRequest{
				mockRequest(t, "GET", "https://go.dev/api/users/1"),
			},
		},
		"variables-in-body": {
			input: "@who = John Doe\nPOST https://httpbin.org/post\n\n{\"name\": \"{{who}}\"}\n",
			expect: []TestRequest{
				mockRequest(t, "POST", "https://httpbin.org/post", WithBody(`{"name": "John Doe"}`)),
			},
		},
		"undefined-variable": {
			input:       `GET {{base_url}}/api/users/1`,
			expectError: ErrUndefinedVariable,
		},
		"dynamic-variable-is-rejected": {
			input:       "GET https://go.dev/api/users/{{$uuid}}",
			expectError: ErrUnsupported,
		},
		"variable-declaration-inside-a-request": {
			input: `GET https://go.dev/
					@base_url = https://go.dev
			`,
			expectError: ErrMalformedHeader,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("parser:%s", name), func(t *testing.T) {
			got, err := Parse(strings.NewReader(test.input))
			if test.expectError != nil {
				require.ErrorIs(t, err, test.expectError)
				require.Nil(t, got, "no requests are returned when a script fails to parse")
				return
			}

			require.NoError(t, err)

			if assert.Len(t, got, len(test.expect)) {
				for i, req := range got {
					expected := test.expect[i]
					assert.Equalf(t, expected.Method, req.Method, "request METHOD %d/%d", i+1, len(got))
					assert.Equalf(t, expected.URL.String(), req.URL.String(), "request URL %d/%d", i+1, len(got))
					assert.Equalf(t, expected.Host, req.Host, "request HOST %d/%d", i+1, len(got))
					assert.Equalf(t, expected.Name, req.Name, "request Name %d/%d", i+1, len(got))
					assert.Equalf(t, expected.Proto, req.Proto, "request Proto %d/%d", i+1, len(got))
					assert.Equalf(t, bodyOf(t, expected), bodyOf(t, req), "request Body %d/%d", i+1, len(got))

					assert.Equalf(t, expected.Header, req.Header, "request Header %d/%d", i+1, len(got))
				}
			}
		})
	}
}

// A parse failure has to name the line it was found on: a probe script is a
// string inside a scenario manifest, where nothing else can point at the fault.
func TestParseErrorsNameTheLine(t *testing.T) {
	testCases := map[string]struct {
		input      string
		expectLine string
	}{
		"bad request line": {
			input:      "GET https://go.dev/\n###\nContent-type: shrooms\nGET https://go.dev/\n",
			expectLine: "line 3",
		},
		"undefined variable in a header": {
			input:      "GET https://go.dev/\nAuthorization: Bearer {{token}}\n",
			expectLine: "line 2",
		},
		"missing host is reported against the request line": {
			input:      "### a request\n# @name broken\nGET /path\nAccept: */*\n",
			expectLine: "line 3",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(test.input))

			require.Error(t, err)
			require.Contains(t, err.Error(), test.expectLine)
		})
	}
}

// A body has to survive being sent more than once: the HTTP client re-sends it
// on a redirect, and the HAR recorder reads it before the request goes out.
func TestRequestBodyIsReplayable(t *testing.T) {
	got, err := Parse(strings.NewReader("POST https://httpbin.org/post\n\n{\"a\": 1}\n"))
	require.NoError(t, err)
	require.Len(t, got, 1)

	request := got[0]
	require.NotNil(t, request.GetBody, "a parsed body must be re-readable")
	require.EqualValues(t, len(`{"a": 1}`), request.ContentLength)

	first, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.EqualValues(t, `{"a": 1}`, string(first))

	replay, err := request.GetBody()
	require.NoError(t, err)

	second, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.EqualValues(t, string(first), string(second))
}

// A request with no body must not carry an empty one: an http.Request with a
// non-nil Body advertises `Content-Length: 0`, which turns a GET into something
// some servers reject.
func TestRequestWithoutBodyHasNoBody(t *testing.T) {
	got, err := Parse(strings.NewReader("GET https://go.dev/\nAccept: */*\n\n"))
	require.NoError(t, err)
	require.Len(t, got, 1)

	require.Nil(t, got[0].Body)
	require.Zero(t, got[0].ContentLength)
}
