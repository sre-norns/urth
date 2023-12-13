package wyrd

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"gopkg.in/yaml.v3"
)

var (
	ErrUnknownKind        = fmt.Errorf("unknown kind")
	ErrUnexpectedSpecType = fmt.Errorf("unexpected spec type")
)

// Type to represent an ID of a resource
type ResourceID uint

const InvalidResourceID ResourceID = 0

func (r ResourceID) String() string {
	return strconv.FormatInt(int64(r), 10)
	// return string(r)
}

type Kind string

var metaKindRegistry = map[Kind]reflect.Type{}

func RegisterKind(kind Kind, proto any) error {
	val := reflect.ValueOf(proto)
	if !val.CanInterface() {
		return fmt.Errorf("type of %q can not interface", val.Type())
	}

	t := val.Type()
	if val.Kind() == reflect.Pointer {
		t = val.Elem().Type()
	}

	metaKindRegistry[kind] = t
	return nil
}

func UnregisterKind(kind Kind) {
	delete(metaKindRegistry, kind)
}

type KindFactory func(kind Kind) (any, error)

func InstanceOf(kind Kind) (any, error) {
	t, known := metaKindRegistry[kind]
	if !known {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKind, kind)
	}

	return reflect.New(t).Interface(), nil
}

func KindOf(maybeManifest any) (result Kind, known bool) {
	val := reflect.ValueOf(maybeManifest)
	t := val.Type()
	if val.Kind() == reflect.Pointer {
		t = val.Elem().Type()
	}

	// Linear scan over map to find key with value equals give: not that terrible when the map is small
	for kind, v := range metaKindRegistry {
		if v == t {
			return kind, true
		}
	}

	return
}

func MustKnowKindOf(maybeManifest any) (kind Kind) {
	kind, ok := KindOf(maybeManifest)
	if !ok {
		panic(ErrUnknownKind)
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

	// A sequence number representing a specific generation of the resource.
	// Populated by the system. Read-only.
	Version uint64 `form:"version" json:"version" yaml:"version" xml:"version" gorm:"default:1"`

	// Name is a unique human-readable identifier of a resource
	Name string `json:"name" yaml:"name" binding:"required" gorm:"uniqueIndex"`

	// Labels is map of string keys and values that can be used to organize and categorize
	// (scope and select) resources.
	Labels Labels `form:"labels,omitempty" json:"labels,omitempty" yaml:"labels,omitempty" xml:"labels,omitempty"`
}

type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     any        `json:"-" yaml:"-"`
}

func (u ResourceManifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta `json:"metadata"`
		Spec     any        `json:"spec,omitempty"` // needed to strip any json tags
	}{
		TypeMeta: u.TypeMeta,
		Metadata: u.Metadata,
		Spec:     u.Spec,
	})
}

func UnmarshalJsonWithRegister(kind Kind, factory KindFactory, specData json.RawMessage) (any, error) {
	spec, err := factory(kind)
	if err != nil { // Kind is not known, get raw message if not-nil
		if len(specData) != 0 { // Is there a spec to parse
			t := make(map[string]any)
			if err := json.Unmarshal(specData, &t); err == nil {
				spec = t
			} else {
				spec = specData
			}
		}
		return spec, nil
	}

	if len(specData) == 0 { // No spec to parse
		return nil, nil
	}

	err = json.Unmarshal(specData, spec)
	return spec, err
}

func (s *ResourceManifest) UnmarshalJSON(data []byte) (err error) {
	aux := struct {
		TypeMeta `json:",inline"`
		Metadata ObjectMeta `json:"metadata"`
		Spec     json.RawMessage
	}{
		TypeMeta: s.TypeMeta,
		Metadata: s.Metadata,
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	s.TypeMeta = aux.TypeMeta
	s.Metadata = aux.Metadata
	s.Spec, err = UnmarshalJsonWithRegister(aux.Kind, InstanceOf, aux.Spec)
	return
}

func (u ResourceManifest) MarshalYAML() (interface{}, error) {
	return struct {
		TypeMeta `json:",inline" yaml:",inline"`
		Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
		Spec     any        `json:"spec" yaml:"spec,omitempty"` // needed to strip any json tags
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
	if err = n.Decode(obj); err != nil {
		return
	}

	s.Spec, err = InstanceOf(s.Kind)
	if err != nil {
		if len(obj.Spec.Content) == 0 {
			s.Spec = nil
			return nil
		}
		s.Spec = make(map[string]string)
	}

	return obj.Spec.Decode(s.Spec)
}
