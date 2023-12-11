package urth

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

type ProbKind = wyrd.Kind

var probKindRegistry = map[ProbKind]reflect.Type{}

func RegisterProbKind(kind ProbKind, proto any) error {
	val := reflect.ValueOf(proto)
	if !val.CanInterface() {
		return fmt.Errorf("type of %q can not interface", val.Type())
	}

	t := val.Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	probKindRegistry[kind] = t
	return nil
}

func UnregisterProbKind(kind ProbKind) {
	delete(probKindRegistry, kind)
}

func InstanceOf(kind wyrd.Kind) (any, error) {
	t, known := probKindRegistry[kind]
	if !known {
		return nil, fmt.Errorf("%w: %q", wyrd.ErrUnknownKind, kind)
	}

	return reflect.New(t).Interface(), nil
}

type ProbManifest struct {
	// Kind identifies the type of content this scenario implementing
	Kind ProbKind `form:"kind" json:"kind,omitempty" yaml:"kind,omitempty" xml:"kind"`

	// Timeout
	Timeout time.Duration `form:"timeout" json:"timeout,omitempty" yaml:"timeout,omitempty" xml:"timeout,omitempty"`

	// Actual script, of a 'kind' type
	Spec interface{} `json:"-" yaml:"-"`
}

func (u ProbManifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind ProbKind    `json:"kind,omitempty" yaml:"kind,omitempty"`
		Spec interface{} `json:"spec,omitempty"` // needed to strip any json tags
	}{
		Kind: u.Kind,
		Spec: u.Spec,
	})
}

func (s *ProbManifest) UnmarshalJSON(data []byte) error {
	aux := &struct {
		Kind    ProbKind        `json:"kind,omitempty" yaml:"kind,omitempty"`
		Timeout time.Duration   `json:"timeout,omitempty" yaml:"timeout,omitempty"`
		Spec    json.RawMessage `json:"spec,omitempty"`
	}{
		Kind:    s.Kind,
		Timeout: s.Timeout,
	}

	err := json.Unmarshal(data, aux)
	if err != nil {
		return err
	}

	s.Kind = aux.Kind
	s.Spec, err = wyrd.UnmarshalJsonWithRegister(aux.Kind, InstanceOf, aux.Spec)
	return err
}

func (u ProbManifest) MarshalYAML() (interface{}, error) {
	return struct {
		Kind    ProbKind      `json:"kind" yaml:"kind"`
		Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
		Spec    interface{}   `json:"spec,omitempty" yaml:"spec,omitempty"` // needed to strip any json tags
	}{
		Kind:    u.Kind,
		Timeout: u.Timeout,
		Spec:    u.Spec,
	}, nil
}

func (s *ProbManifest) UnmarshalYAML(n *yaml.Node) (err error) {
	type S ProbManifest
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
		if len(obj.Spec.Content) == 0 {
			s.Spec = nil
			return nil
		}
		s.Spec = make(map[string]string)
	}

	return obj.Spec.Decode(s.Spec)
}
