package urth

import (
	"fmt"
	"testing"

	"github.com/sre-norns/urth/pkg/wyrd"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestResourceManifest_Unmarshaling(t *testing.T) {
	testCases := map[string]struct {
		given       []byte
		expect      ResourceManifest
		expectError bool
	}{
		"unknown_kind": {
			expectError: true,
			given: []byte(`
kind: jumper
metadata:
	name: X-y-z
spec:
	description: Awesome
	active: true
	requirements:
	MatchLabels:
		- os: linux
`),
		},
		"runner": {
			given: []byte(`
kind: runners
metadata:
  name: nginx-demo
spec:
  active: true
  description: Awesome
  requirements:
    matchLabels:
      os: linux
`),
			expect: ResourceManifest{
				TypeMeta: TypeMeta{
					Kind: "runners",
				},
				Metadata: ObjectMeta{
					Name: "nginx-demo",
				},
				Spec: &RunnerDefinition{
					IsActive:    true,
					Description: "Awesome",
					Requirements: wyrd.LabelSelector{
						MatchLabels: wyrd.Labels{
							"os": "linux",
						},
					},
				},
			},
		},

		// requirements:
		// matchLabels:
		//   os: "linux"
		// matchSelector:
		//  - Key: "owner"
		//    Op: "in"
		//    Values:
		// 	- "allowed"
		// 	- "trusted"

		"scenario": {
			given: []byte(`
apiVersion: v1
kind: scenarios
metadata:
  name: simple-web-prober
  labels:
    app: web-prob
    function: front-end
spec:
  description: "Awesome"
  active: true
  schedule: "* * * * *"
  script:
    kind: "http/get"
  requirements:
    matchLabels:
      os: "linux"
    matchSelector:
      - { key: "owner", operator: "in",  values: ["trusted", "allowed"] }
      - { key: "env", operator: "notIn",  values: ["dev", "testing"] }
`),
			expect: ResourceManifest{
				TypeMeta: TypeMeta{
					APIVersion: "v1",
					Kind:       "scenarios",
				},
				Metadata: ObjectMeta{
					Name: "simple-web-prober",
					Labels: wyrd.Labels{
						"app":      "web-prob",
						"function": "front-end",
					},
				},
				Spec: &ScenarioSpec{
					IsActive:    true,
					RunSchedule: "* * * * *",
					Description: "Awesome",
					Script: &ScenarioScript{
						Kind: "http/get",
					},
					Requirements: wyrd.LabelSelector{
						MatchSelector: []wyrd.Selector{
							{Key: "owner", Op: "in", Values: []string{"trusted", "allowed"}},
							{Key: "env", Op: "notIn", Values: []string{"dev", "testing"}},
						},
						MatchLabels: wyrd.Labels{
							"os": "linux",
						},
					},
				},
			},
		},

		"artifact": {
			given: []byte(`
apiVersion: v1
kind: artifacts
metadata:
 name: artifact-example
 labels:
  scenario: xyz-script
  function: front-end
spec:
 rel: "har"
 mimeType: "data"
`),
			expect: ResourceManifest{
				TypeMeta: TypeMeta{
					APIVersion: "v1",
					Kind:       "artifacts",
				},
				Metadata: ObjectMeta{
					Name: "artifact-example",
					Labels: wyrd.Labels{
						"scenario": "xyz-script",
						"function": "front-end",
					},
				},
				Spec: &ArtifactSpec{
					Rel:      "har",
					MimeType: "data",
					Content:  nil,
				},
			},
		},
	}

	for name, tc := range testCases {
		test := tc
		t.Run(fmt.Sprintf("unmarshal:%s", name), func(t *testing.T) {
			var got ResourceManifest
			err := yaml.Unmarshal(test.given, &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.Nil(t, err, "expected error: %v", test.expectError)
			}

			require.EqualValues(t, test.expect, got)
		})
	}
}
