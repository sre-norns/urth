package wyrd_test

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/urth/pkg/wyrd"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
		t.Run(name, func(t *testing.T) {
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
			expectError: false,
		},
		"unknown-kind": {
			given: `{"kind":"unknownSpec", "metadata":{"name":""},"spec":{"field":"xyz","desc":"unknown"}}`,
			expect: wyrd.ResourceManifest{
				TypeMeta: wyrd.TypeMeta{
					Kind: wyrd.Kind("unknownSpec"),
				},
				Metadata: wyrd.ObjectMeta{},
				Spec:     json.RawMessage(`{"field":"xyz","desc":"unknown"}`),
			},
			expectError: false,
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
		t.Run(name, func(t *testing.T) {
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

func TestManifestMarshaling_YAML(t *testing.T) {
	type TestSpec struct {
		Value int    `yaml:"value"`
		Name  string `yaml:"name"`
	}

	testCases := map[string]struct {
		given       wyrd.ResourceManifest
		expect      string
		expectError bool
	}{
		"nothing": {
			given: wyrd.ResourceManifest{},
			expect: `metadata:
    name: ""
`,
		},
		"min-spec": {
			given: wyrd.ResourceManifest{
				Spec: &TestSpec{
					Value: 1,
					Name:  "life",
				},
			},
			expect: `metadata:
    name: ""
spec:
    value: 1
    name: life
`,
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
			expect: `kind: testSpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := yaml.Marshal(test.given)
			require.NoError(t, err)
			require.Equal(t, test.expect, string(got))
		})
	}
}

func TestManifestUnmarshaling_YAML(t *testing.T) {
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
			given: `metadata:
    name: "xyz"
`,
			expect: wyrd.ResourceManifest{
				Metadata: wyrd.ObjectMeta{
					Name: "xyz",
				},
			},
			expectError: !true,
		},
		"unknown-kind": {
			given: `kind: unknownSpec
metadata:
    name: ""
spec:
    value: 1
    name: life
`,
			expect: wyrd.ResourceManifest{
				TypeMeta: wyrd.TypeMeta{
					Kind: wyrd.Kind("unknownSpec"),
				},
				Spec: map[string]string{
					"value": "1",
					"name":  "life",
				},
			},
			expectError: false,
		},
		"min-spec": {
			expect: wyrd.ResourceManifest{
				TypeMeta: wyrd.TypeMeta{
					Kind: testKind,
				},
				Spec: &TestSpec{},
			},
			given: `kind: testSpec
`,
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
			given: `kind: testSpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
`,
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			var got wyrd.ResourceManifest
			err := yaml.Unmarshal([]byte(test.given), &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}

func TestCustomUnmarshaling_JSON(t *testing.T) {
	type TestSpec struct {
		Value int    `yaml:"value"`
		Name  string `yaml:"name"`
	}
	testKind := wyrd.Kind("testSpec")

	err := wyrd.RegisterKind(testKind, &TestSpec{})
	require.NoError(t, err)
	defer wyrd.UnregisterKind(testKind)

	testCases := map[string]struct {
		givenKind   wyrd.Kind
		givenData   json.RawMessage
		expect      any
		expectError bool
	}{
		"unknown-kind-nil-data": {
			givenKind: "",
			givenData: nil,
			expect:    nil,
		},
		"unknown-kind-empty-data": {
			givenKind: "",
			givenData: json.RawMessage{},
			expect:    nil,
		},
		"known-kind-no-data": {
			givenKind: "testSpec",
			givenData: json.RawMessage{},
			expect:    nil,
		},
		"known-kind-with-data": {
			givenKind: "testSpec",
			givenData: json.RawMessage(`{"value":321,"name":"que"}`),
			expect: &TestSpec{
				Value: 321,
				Name:  "que",
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(name, func(t *testing.T) {
			got, err := wyrd.UnmarshalJsonWithRegister(test.givenKind, wyrd.InstanceOf, test.givenData)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.NoError(t, err, "expected error: %v", test.expectError)
				require.Equal(t, test.expect, got)
			}
		})
	}
}
