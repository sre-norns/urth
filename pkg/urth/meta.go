package urth

import (
	"encoding/json"
	"fmt"

	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

// TypeMeta describe individual objects returned by API
type TypeMeta struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty" yaml:"kind,omitempty" binding:"required"`
}

type ObjectMeta struct {
	// System generated unique identified of this object
	UUID ResourceID `json:"uid,omitempty" yaml:"uid,omitempty"`

	// Name is a unique human-readable identifier of a resource
	Name string `json:"name" yaml:"name"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels wyrd.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
}

type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec     interface{} `json:"-" yaml:"-"`
}

func (m *ResourceManifest) GetMetadata() ResourceMeta {
	return ResourceMeta{
		// ID: m.Metadata.UUID,
		Name:   m.Metadata.Name,
		Labels: m.Metadata.Labels,
	}
}

func (u *ResourceManifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta  `json:"metadata"`
		Spec     interface{} // needed to strip any json tags
	}{
		TypeMeta: u.TypeMeta,
		Metadata: u.Metadata,
		Spec:     u.Spec,
	})
}

func (s *ResourceManifest) UnmarshalJSON(data []byte) error {
	aux := &struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta `json:"metadata"`
		Spec     json.RawMessage
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
	}

	err := json.Unmarshal(data, aux)
	if err != nil {
		return err
	}

	s.TypeMeta = aux.TypeMeta
	s.Metadata = aux.Metadata

	switch s.Kind {
	case "scenarios":
		s.Spec = &CreateScenario{}
	case "runners":
		s.Spec = &RunnerDefinition{}
	case "scenario_run_results":
		s.Spec = &InitialScenarioRunResults{}
	case "artifacts":
		s.Spec = &ArtifactValue{}
	default:
		return fmt.Errorf("unknown kind %q", s.Kind)
	}

	if len(aux.Spec) == 0 { // No spec to parse
		return nil
	}

	return json.Unmarshal(aux.Spec, s.Spec)
}

func (u *ResourceManifest) MarshalYAML() (interface{}, error) {
	return struct {
		TypeMeta `json:",inline" yaml:",inline"`
		Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
		Spec     interface{} // needed to strip any json tags
	}{
		TypeMeta: u.TypeMeta,
		Metadata: u.Metadata,
		Spec:     u.Spec,
	}, nil
}

func (s *ResourceManifest) UnmarshalYAML(n *yaml.Node) error {
	type S ResourceManifest
	type T struct {
		*S   `yaml:",inline"`
		Spec yaml.Node `yaml:"spec"`
	}

	obj := &T{S: (*S)(s)}
	if err := n.Decode(obj); err != nil {
		return err
	}

	// FIXME: Should be a map with registered Kinds
	switch s.Kind {
	case "scenarios":
		s.Spec = &CreateScenario{}
	case "runners":
		s.Spec = &RunnerDefinition{}
	case "scenario_run_results":
		s.Spec = &InitialScenarioRunResults{}
	case "artifacts":
		s.Spec = &ArtifactValue{}
	default:
		return fmt.Errorf("unknown kind %q", s.Kind)
	}

	return obj.Spec.Decode(s.Spec)
}
