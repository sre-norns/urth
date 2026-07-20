package httpparser

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/require"
)

func TestTextParser(t *testing.T) {
	testCases := map[string]struct {
		input  string
		expect string
	}{

		"empty-input": {
			input:  "",
			expect: "",
		},
		"comments-only-input": {
			input:  "###",
			expect: "",
		},
		"text-only": {
			input:  "some text",
			expect: "some text",
		},
		"comments-and-text": {
			input:  "text #some comment",
			expect: "text",
		},
		"comments-and-text-2": {
			input:  "text #some comment\nmore text",
			expect: "text\nmore text",
		},
		"comments-and-text-3": {
			input:  "text #some comment\nmore text #another comment",
			expect: "text\nmore text",
		},
		"comments-and-text-4": {
			input:  "text #some comment\nmore text #another comment\n",
			expect: "text\nmore text",
		},
		"comments-and-text-5": {
			input:  "text #some comment\nmore text\n\n#another comment\nmore text",
			expect: "text\nmore text\n\nmore text",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			// Read the reader to exhaustion, the way Parse consumes it. A single
			// Read cannot be asserted on directly: when the input filters down to
			// nothing the reader correctly reports io.EOF on the first call.
			got, err := io.ReadAll(&commentFilteringReader{reader: strings.NewReader(test.input)})

			require.NoError(t, err)
			require.EqualValues(t, test.expect, string(got))
		})
	}
}

// A reader whose content filters away entirely must still terminate cleanly
// rather than reporting an error to its consumer.
func TestTextParserEmptyResultTerminates(t *testing.T) {
	for name, input := range map[string]string{
		"empty":         "",
		"comments only": "### just a comment\n# and another\n",
	} {
		t.Run(name, func(t *testing.T) {
			n, err := (&commentFilteringReader{reader: strings.NewReader(input)}).Read(make([]byte, 8))

			require.Zero(t, n)
			require.ErrorIs(t, err, io.EOF)
		})
	}
}

// Errors from the underlying reader must reach the caller rather than being
// reported as a clean end of input.
func TestTextParserPropagatesReadError(t *testing.T) {
	expected := errors.New("boom")

	_, err := io.ReadAll(&commentFilteringReader{reader: iotest.ErrReader(expected)})

	require.ErrorIs(t, err, expected)
}
