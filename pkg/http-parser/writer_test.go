package httpparser

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// What Marshal writes has to parse back into the same requests: `urthctl
// convert-har` exists to turn a recorded session into a script that a rest
// prob then runs, and a lossy conversion would silently probe something other
// than what was recorded.
func TestMarshalRoundTrip(t *testing.T) {
	for name, script := range map[string]string{
		"bare url":        "GET https://go.dev/test HTTP/1.1\n",
		"with query":      "GET https://go.dev/search?limit=10&q=urth HTTP/1.1\n",
		"cleartext":       "GET http://localhost:8321/test HTTP/1.1\n",
		"with headers":    "POST https://httpbin.org/post HTTP/1.1\nAccept: application/json\n",
		"with body":       "POST https://httpbin.org/post HTTP/1.1\nContent-Type: application/json\n\n{\"name\": \"John Doe\"}\n",
		"named":           "# @name GetVersion\nGET https://go.dev/version HTTP/1.1\n",
		"virtual host":    "GET http://10.0.0.7:8080/healthz HTTP/1.1\nHost: api.example.com\n",
		"many requests":   "GET https://go.dev/one HTTP/1.1\n###\nGET https://go.dev/two HTTP/1.1\n",
		"proto version":   "GET https://go.dev/test HTTP/1.0\n",
		"body then query": "PUT https://go.dev/items?id=7 HTTP/1.1\n\nhello\n",
	} {
		t.Run(name, func(t *testing.T) {
			requests, err := Parse(strings.NewReader(script))
			require.NoError(t, err)

			var written strings.Builder
			require.NoError(t, Marshal(&written, requests))

			reparsed, err := Parse(strings.NewReader(written.String()))
			require.NoError(t, err, "Marshal wrote a script that does not parse:\n%v", written.String())

			require.Len(t, reparsed, len(requests))
			for i, before := range requests {
				after := reparsed[i]

				require.Equal(t, before.Method, after.Method, "request %d method", i+1)
				require.Equal(t, before.URL.String(), after.URL.String(), "request %d URL", i+1)
				require.Equal(t, before.Host, after.Host, "request %d Host", i+1)
				require.Equal(t, before.Proto, after.Proto, "request %d proto", i+1)
				require.Equal(t, before.Name, after.Name, "request %d name", i+1)
				require.Equal(t, before.Header, after.Header, "request %d headers", i+1)

				beforeBody, err := before.bodyContent()
				require.NoError(t, err)
				afterBody, err := after.bodyContent()
				require.NoError(t, err)
				require.Equal(t, string(beforeBody), string(afterBody), "request %d body", i+1)
			}
		})
	}
}

// Marshal reads the body through GetBody where it can, so a request that is
// written out is still sendable afterwards.
func TestMarshalLeavesBodyReadable(t *testing.T) {
	requests, err := Parse(strings.NewReader("POST https://httpbin.org/post\n\n{\"a\": 1}\n"))
	require.NoError(t, err)
	require.Len(t, requests, 1)

	var written strings.Builder
	require.NoError(t, Marshal(&written, requests))

	body, err := requests[0].bodyContent()
	require.NoError(t, err)
	require.EqualValues(t, `{"a": 1}`, string(body))
}
