package urth

import (
	"fmt"

	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

// TypeMeta describe individual objects returned by API
type TypeMeta struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty" yaml:"kind,omitempty"`
}

type ObjectMeta struct {
	// System generated unique identified of this object
	UUID ResourceID `json:"uid,omitempty" yaml:"uid,omitempty"`

	// Name is a unique human-readable identifier of a resource
	Name string `json:"name" yaml:"name"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels wyrd.Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty" gorm:"-"`
}

type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec     interface{} `json:"-" yaml:"-"`
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

	switch s.Kind {
	case "scenario":
		s.Spec = new(CreateScenario)
	case "runner":
		s.Spec = new(RunnerDefinition)
	default:
		return fmt.Errorf("unknown kind %q", s.Kind)
	}

	return obj.Spec.Decode(s.Spec)
}
