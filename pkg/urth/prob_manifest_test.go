package urth_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
	"github.com/stretchr/testify/require"
)

func TestCustomMarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testCases := map[string]struct {
		given  urth.ProbManifest
		expect string
	}{
		"nothing": {
			given:  urth.ProbManifest{},
			expect: `{}`,
		},
		"min-spec": {
			given: urth.ProbManifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			expect: `{"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			given: urth.ProbManifest{
				Kind: wyrd.Kind("testSpec"),
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			expect: `{"kind":"testSpec","spec":{"value":42,"name":"meaning"}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := json.Marshal(test.given)
			require.NoError(t, err)
			require.Equal(t, test.expect, string(got))
		})
	}
}

func TestCustomUnmarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testKind := wyrd.Kind("testSpec")
	require.NoError(t, urth.RegisterProbKind(testKind, &TestSpec{}))
	defer urth.UnregisterProbKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      urth.ProbManifest
		expectError bool
	}{
		"nothing": {
			given:  `{}`,
			expect: urth.ProbManifest{},
		},
		"unknown-kind": {
			given: `{"kind":"unknownSpec", "metadata":{"name":""},"spec":{"field":"xyz","desc":"unknown"}}`,
			expect: urth.ProbManifest{
				Kind: wyrd.Kind("unknownSpec"),
				Spec: json.RawMessage(`{"field":"xyz","desc":"unknown"}`),
			},
			expectError: false,
		},
		"basic": {
			expect: urth.ProbManifest{
				Kind: testKind,
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			given: `{"kind":"testSpec","spec":{"value":42,"name":"meaning"}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got urth.ProbManifest
			err := json.Unmarshal([]byte(test.given), &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}
