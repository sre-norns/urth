package prob_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestCustomMarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testCases := map[string]struct {
		given       prob.Manifest
		expect      string
		expectError bool
	}{
		"nothing": {
			given:  prob.Manifest{},
			expect: `{}`,
		},
		"min-spec": {
			given: prob.Manifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			expect: `{"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			given: prob.Manifest{
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
	require.NoError(t, prob.RegisterKind(testKind, &TestSpec{}))
	defer prob.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      prob.Manifest
		expectError bool
	}{
		"nothing-object": {
			given:  `{}`,
			expect: prob.Manifest{},
		},
		"unknown-kind": {
			given: `{"kind":"unknownSpec","spec":{"field":"xyz","desc":"unknown"}}`,
			expect: prob.Manifest{
				Kind: manifest.Kind("unknownSpec"),
				Spec: map[string]any{"field": "xyz", "desc": "unknown"},
			},
		},
		"basic": {
			expect: prob.Manifest{
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
			var got prob.Manifest
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
