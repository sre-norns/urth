package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SSE frames are newline-delimited, so a log chunk containing newlines has to
// become one `data:` line each. Getting this wrong does not error -- it
// produces a stream the browser silently mis-parses, which is the kind of bug
// that only shows up as "the log panel looks weird".
func TestSplitLines(t *testing.T) {
	tests := map[string]struct {
		payload string
		want    []string
	}{
		"single line without terminator": {
			payload: "hello",
			want:    []string{"hello"},
		},
		"single line with terminator": {
			payload: "hello\n",
			want:    []string{"hello"},
		},
		"several lines": {
			payload: "one\ntwo\nthree\n",
			want:    []string{"one", "two", "three"},
		},
		"trailing partial line is kept": {
			payload: "one\ntwo",
			want:    []string{"one", "two"},
		},
		"blank lines within are preserved": {
			payload: "one\n\ntwo\n",
			want:    []string{"one", "", "two"},
		},
		"CRLF is normalised": {
			payload: "one\r\ntwo\r\n",
			want:    []string{"one", "two"},
		},
		"empty payload": {
			payload: "",
			want:    []string{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got := splitLines([]byte(test.payload))

			asStrings := make([]string, 0, len(got))
			for _, line := range got {
				asStrings = append(asStrings, string(line))
			}

			require.Equal(t, test.want, asStrings)
		})
	}
}
