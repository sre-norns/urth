package urth_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestCustomMarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testCases := map[string]struct {
		given       urth.ProbManifest
		expect      string
		expectError bool
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
				Kind: manifest.Kind("testSpec"),
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

	testKind := manifest.Kind("testSpec")
	require.NoError(t, urth.RegisterProbKind(testKind, &TestSpec{}))
	defer urth.UnregisterProbKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      urth.ProbManifest
		expectError bool
	}{
		"nothing-object": {
			given:  `{}`,
			expect: urth.ProbManifest{},
		},
		"unknown-kind": {
			given: `{"kind":"unknownSpec","spec":{"field":"xyz","desc":"unknown"}}`,
			expect: urth.ProbManifest{
				Kind: manifest.Kind("unknownSpec"),
				Spec: map[string]any{"field": "xyz", "desc": "unknown"},
			},
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
		"invalid-spec": {
			expectError: true,
			given:       `{"kind":"testSpec","spec":{"script":"meaning"}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got urth.ProbManifest
			err := json.Unmarshal([]byte(test.given), &got)
			if test.expectError {
				require.Error(t, err, "expected error")
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}
