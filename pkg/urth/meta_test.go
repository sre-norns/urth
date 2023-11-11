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
kind: runner
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
					Kind: "runner",
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
		"scenario": {
			given: []byte(`
apiVersion: v1
kind: scenario
metadata:
 name: simple-web-prober
 labels:
  app: web-prob
  function: front-end
spec:
 active: true
 schedule: "* * * * *"
 script:
   kind: application/javascript
`),
			expect: ResourceManifest{
				TypeMeta: TypeMeta{
					APIVersion: "v1",
					Kind:       "scenario",
				},
				Metadata: ObjectMeta{
					Name: "simple-web-prober",
					Labels: wyrd.Labels{
						"app":      "web-prob",
						"function": "front-end",
					},
				},
				Spec: &CreateScenario{
					IsActive:    true,
					RunSchedule: "* * * * *",
					// Description: "Awesome",
					Script: &ScenarioScript{
						Kind: "application/javascript",
					},
					// Requirements: wyrd.LabelSelector{
					// 	MatchLabels: wyrd.Labels{
					// 		"os": "linux",
					// 	},
					// },
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
