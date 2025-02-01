package urth_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestResourceManifest_Unmarshaling(t *testing.T) {
	testCases := map[string]struct {
		given       []byte
		expect      manifest.ResourceManifest
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
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					Kind: "runners",
				},
				Metadata: manifest.ObjectMeta{
					Name: "nginx-demo",
				},
				Spec: &urth.RunnerSpec{
					IsActive:    true,
					Description: "Awesome",
					Requirements: manifest.LabelSelector{
						MatchLabels: manifest.Labels{
							"os": "linux",
						},
					},
				},
			},
		},

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
  prob:
    kind: http
    timeout: 120s
  requirements:
    matchLabels:
      os: "linux"
    matchSelector:
      - { key: "owner", operator: "in",  values: ["trusted", "allowed"] }
      - { key: "env", operator: "notIn",  values: ["dev", "testing"] }
`),
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					APIVersion: "v1",
					Kind:       "scenarios",
				},
				Metadata: manifest.ObjectMeta{
					Name: "simple-web-prober",
					Labels: manifest.Labels{
						"app":      "web-prob",
						"function": "front-end",
					},
				},
				Spec: &urth.ScenarioSpec{
					IsActive:    true,
					RunSchedule: "* * * * *",
					Description: "Awesome",
					Prob: urth.ProbManifest{
						Kind:    "http",
						Timeout: time.Second * 120,
					},
					Requirements: manifest.LabelSelector{
						MatchSelector: manifest.SelectorRules{
							{Key: "owner", Op: manifest.LabelSelectorOpIn, Values: []string{"trusted", "allowed"}},
							{Key: "env", Op: manifest.LabelSelectorOpNotIn, Values: []string{"dev", "testing"}},
						},
						MatchLabels: manifest.Labels{
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
			expect: manifest.ResourceManifest{
				TypeMeta: manifest.TypeMeta{
					APIVersion: "v1",
					Kind:       "artifacts",
				},
				Metadata: manifest.ObjectMeta{
					Name: "artifact-example",
					Labels: manifest.Labels{
						"scenario": "xyz-script",
						"function": "front-end",
					},
				},
				Spec: &urth.ArtifactSpec{
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
			var got manifest.ResourceManifest
			err := yaml.Unmarshal(test.given, &got)
			if test.expectError {
				require.Error(t, err, "expected error: %v", test.expectError)
			} else {
				require.Nil(t, err, "expected error: %v", test.expectError)
				require.EqualValues(t, test.expect, got)
			}
		})
	}
}
