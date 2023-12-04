package wyrd_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/sre-norns/urth/pkg/wyrd"
	"github.com/stretchr/testify/require"
)

func TestManifestMarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testCases := map[string]struct {
		given       wyrd.ResourceManifest
		expect      string
		expectError bool
	}{
		"nothing": {
			given:  wyrd.ResourceManifest{},
			expect: `{"metadata":{"name":""}}`,
		},
		"min-spec": {
			given: wyrd.ResourceManifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			expect: `{"metadata":{"name":""},"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			given: wyrd.ResourceManifest{
				TypeMeta: wyrd.TypeMeta{
					Kind: wyrd.Kind("testSpec"),
				},
				Metadata: wyrd.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			expect: `{"kind":"testSpec","metadata":{"name":"test-spec"},"spec":{"value":42,"name":"meaning"}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("marshal:json:%s", name), func(t *testing.T) {
			got, err := json.Marshal(test.given)
			require.NoError(t, err)
			require.Equal(t, test.expect, string(got))
		})
	}
}

func TestManifestUnmarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `json:"value"`
		Name  string `json:"name"`
	}

	testKind := wyrd.Kind("testSpec")

	err := wyrd.RegisterKind(testKind, &TestSpec{})
	require.NoError(t, err)
	defer wyrd.UnregisterKind(testKind)

	testCases := map[string]struct {
		given       string
		expect      wyrd.ResourceManifest
		expectError bool
	}{
		"nothing": {
			given:       `{"metadata":{"name":""}}`,
			expect:      wyrd.ResourceManifest{},
			expectError: true,
		},
		"unknown-kind": {
			given:       `{"kind":"unknownSpec", "metadata":{"name":""},"spec":{"value":1,"name":"life"}}`,
			expect:      wyrd.ResourceManifest{},
			expectError: true,
		},
		"min-spec": {
			expect: wyrd.ResourceManifest{
				TypeMeta: wyrd.TypeMeta{
					Kind: testKind,
				},
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			given: `{"kind":"testSpec", "metadata":{"name":""},"spec":{"value":1,"name":"life"}}`,
		},
		"basic": {
			expect: wyrd.ResourceManifest{
				TypeMeta: wyrd.TypeMeta{
					Kind: testKind,
				},
				Metadata: wyrd.ObjectMeta{
					Name: "test-spec",
				},
				Spec: &TestSpec{
					Value: 42,
					Name:  "meaning",
				},
			},
			given: `{"kind":"testSpec","metadata":{"name":"test-spec"},"spec":{"value":42,"name":"meaning"}}`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("unmarshal:json:%s", name), func(t *testing.T) {
			var got wyrd.ResourceManifest
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
