package prob

import (
	"encoding/json"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"

	"gopkg.in/yaml.v3"
)

type Kind = manifest.Kind

// RunStatus represents the state of script execution once job has been successfully run
type RunStatus string

const (
	// A run completed with a status
	RunNotFinished      RunStatus = ""
	RunFinishedSuccess  RunStatus = "success"
	RunFinishedFailed   RunStatus = "failed"
	RunFinishedError    RunStatus = "errored"
	RunFinishedCanceled RunStatus = "canceled"
	RunFinishedTimeout  RunStatus = "timeout"
)

type Manifest struct {
	// Kind identifies the type of content this scenario implementing
	Kind Kind `json:"kind,omitempty" yaml:"kind,omitempty" xml:"kind" form:"kind"`

	// Timeout
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" xml:"timeout" form:"timeout"`

	// Actual script, of a 'kind' type
	Spec any `json:"-" yaml:"-"`
}

type Artifact struct {
	// Relation type: log / HAR / etc? Determines how content is consumed by clients
	Rel string `form:"rel,omitempty" json:"rel,omitempty" yaml:"rel,omitempty" xml:"rel,omitempty"`

	// MimeType of the content
	MimeType string `form:"mimeType,omitempty" json:"mimeType,omitempty" yaml:"mimeType,omitempty" xml:"mimeType,omitempty"`

	// Blob content of the artifact
	Content []byte `form:"content,omitempty" json:"content,omitempty" yaml:"content,omitempty" xml:"content,omitempty"`
}

func (u Manifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind    Kind          `json:"kind,omitempty"`
		Timeout time.Duration `json:"timeout,omitempty"`
		Spec    any           `json:"spec,omitempty"` // needed to strip any json tags
	}{
		Kind:    u.Kind,
		Timeout: u.Timeout,
		Spec:    u.Spec,
	})
}

func (s *Manifest) UnmarshalJSON(data []byte) error {
	aux := &struct {
		Kind    Kind            `json:"kind,omitempty"`
		Timeout time.Duration   `json:"timeout,omitempty"`
		Spec    json.RawMessage `json:"spec,omitempty"`
	}{
		Kind:    s.Kind,
		Timeout: s.Timeout,
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	m2, err := manifest.UnmarshalJSONWithRegister(aux.Kind, InstanceOf, aux.Spec, nil)
	if err != nil {
		return err
	}

	s.Kind = aux.Kind
	s.Timeout = aux.Timeout
	s.Spec = m2.Spec
	return err
}

func (u Manifest) MarshalYAML() (interface{}, error) {
	return struct {
		Kind    Kind          `json:"kind" yaml:"kind"`
		Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
		Spec    interface{}   `json:"spec,omitempty" yaml:"spec,omitempty"` // needed to strip any json tags
	}{
		Kind:    u.Kind,
		Timeout: u.Timeout,
		Spec:    u.Spec,
	}, nil
}

func (s *Manifest) UnmarshalYAML(n *yaml.Node) (err error) {
	type S Manifest
	type T struct {
		*S   `yaml:",inline"`
		Spec yaml.Node `yaml:"spec"`
	}

	obj := &T{S: (*S)(s)}
	if err := n.Decode(obj); err != nil {
		return err
	}

	m2, err := InstanceOf(s.Kind)
	if err != nil {
		if len(obj.Spec.Content) == 0 {
			s.Spec = nil
			return nil
		}
		m2.Spec = make(map[string]any)
	}

	s.Spec = m2.Spec

	return obj.Spec.Decode(s.Spec)
}
