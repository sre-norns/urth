package bark

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterFlags(t *testing.T) {
	testCases := map[string]struct {
		given  string
		expect string
	}{
		"empty": {
			given:  "",
			expect: "",
		},

		"identity": {
			given:  "hello",
			expect: "hello",
		},

		"case": {
			given:  "type; utf-3",
			expect: "type",
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got := filterFlags(test.given)

			require.Equal(t, test.expect, got)
		})
	}
}
