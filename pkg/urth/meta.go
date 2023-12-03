package urth

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

var (
	ErrUnknownKind = fmt.Errorf("unknown kind")
)

type Kind string

var metaKindRegistry = map[Kind]reflect.Type{}

func RegisterKind(kind Kind, proto any) error {
	t := reflect.ValueOf(proto)
	if t.Kind() != reflect.Pointer || !t.CanInterface() {
		return fmt.Errorf("pointer expected")
	}

	metaKindRegistry[kind] = t.Elem().Type()
	return nil
}

func InstanceOf(kind Kind) (any, error) {
	t, known := metaKindRegistry[kind]
	if !known {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}

	return reflect.New(t).Interface(), nil
}

func KindOf(maybeManifest any) (result Kind, known bool) {
	value := reflect.ValueOf(maybeManifest)
	if value.Kind() != reflect.Pointer || !value.CanInterface() {
		return
	}

	t := value.Elem().Type()
	// Linear scan over map to find key with value equals give: not that terrible when the map is small
	for kind, v := range metaKindRegistry {
		if v == t {
			return kind, true
		}
	}

	return
}

// TypeMeta describe individual objects returned by API
type TypeMeta struct {
	APIVersion string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       Kind   `json:"kind,omitempty" yaml:"kind,omitempty" binding:"required"`
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

	s.Spec, err = InstanceOf(s.Kind)
	if err != nil {
		return err
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

func (s *ResourceManifest) UnmarshalYAML(n *yaml.Node) (err error) {
	type S ResourceManifest
	type T struct {
		*S   `yaml:",inline"`
		Spec yaml.Node `yaml:"spec"`
	}

	obj := &T{S: (*S)(s)}
	if err := n.Decode(obj); err != nil {
		return err
	}

	s.Spec, err = InstanceOf(s.Kind)
	if err != nil {
		return
	}

	return obj.Spec.Decode(s.Spec)
}
