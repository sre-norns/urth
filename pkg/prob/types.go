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

// DataClass describes what an artifact's content may expose, so that retention,
// access control and audits have something to act on without inspecting bytes.
//
// It is declared by the prober that produced the artifact, because only the
// producer knows what it captured. An artifact that makes no declaration is
// DataClassUnknown: the absence of a claim is not a claim of safety.
type DataClass string

const (
	// DataClassUnknown is the zero value: the producing prober made no
	// declaration, so the content must be treated as potentially sensitive.
	DataClassUnknown DataClass = ""

	// DataClassClean means the content cannot carry credentials by
	// construction -- metrics samples, timings, and similar derived data.
	DataClassClean DataClass = "clean"

	// DataClassRedacted means the content is derived from a live exchange, but
	// values identified as credentials were removed before it was written.
	DataClassRedacted DataClass = "redacted"

	// DataClassSecretBearing means the content is a faithful capture of a live
	// exchange and may contain credentials. This is not a defect: for a
	// recording whose purpose is replay, fidelity and redaction are the same
	// bytes, and the honest option is to say so and protect it accordingly.
	DataClassSecretBearing DataClass = "secret-bearing"
)

// MayContainSecrets reports whether the artifact must be handled as though it
// carries credentials. Unknown counts as unsafe, so a prober that forgets to
// classify its output fails towards caution.
func (c DataClass) MayContainSecrets() bool {
	return c != DataClassClean && c != DataClassRedacted
}

// String renders the class for display and labelling. The unknown class is
// spelled out rather than left empty, so that it is selectable in a query.
func (c DataClass) String() string {
	if c == DataClassUnknown {
		return "unknown"
	}

	return string(c)
}

type Artifact struct {
	// Relation type: log / HAR / etc? Determines how content is consumed by clients
	Rel string `form:"rel,omitempty" json:"rel,omitempty" yaml:"rel,omitempty" xml:"rel,omitempty"`

	// MimeType of the content
	MimeType string `form:"mimeType,omitempty" json:"mimeType,omitempty" yaml:"mimeType,omitempty" xml:"mimeType,omitempty"`

	// DataClass declares what this content may expose. See DataClass.
	DataClass DataClass `form:"dataClass,omitempty" json:"dataClass,omitempty" yaml:"dataClass,omitempty" xml:"dataClass,omitempty"`

	// Blob content of the artifact
	Content []byte `form:"content,omitempty" json:"content,omitempty" yaml:"content,omitempty" xml:"content,omitempty"`
}

func (m Manifest) MarshalJSON() ([]byte, error) {
	return json.Marshal(&struct {
		Kind    Kind          `json:"kind,omitempty"`
		Timeout time.Duration `json:"timeout,omitempty"`
		Spec    any           `json:"spec,omitempty"` // needed to strip any json tags
	}{
		Kind:    m.Kind,
		Timeout: m.Timeout,
		Spec:    m.Spec,
	})
}

func (m *Manifest) UnmarshalJSON(data []byte) error {
	aux := &struct {
		Kind    Kind            `json:"kind,omitempty"`
		Timeout time.Duration   `json:"timeout,omitempty"`
		Spec    json.RawMessage `json:"spec,omitempty"`
	}{
		Kind:    m.Kind,
		Timeout: m.Timeout,
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	m2, err := manifest.UnmarshalJSONWithRegister(aux.Kind, InstanceOf, aux.Spec, nil)
	if err != nil {
		return err
	}

	m.Kind = aux.Kind
	m.Timeout = aux.Timeout
	m.Spec = m2.Spec
	return err
}

func (m Manifest) MarshalYAML() (interface{}, error) {
	return struct {
		Kind    Kind          `json:"kind" yaml:"kind"`
		Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
		Spec    interface{}   `json:"spec,omitempty" yaml:"spec,omitempty"` // needed to strip any json tags
	}{
		Kind:    m.Kind,
		Timeout: m.Timeout,
		Spec:    m.Spec,
	}, nil
}

func (m *Manifest) UnmarshalYAML(n *yaml.Node) (err error) {
	type S Manifest
	type T struct {
		*S   `yaml:",inline"`
		Spec yaml.Node `yaml:"spec"`
	}

	obj := &T{S: (*S)(m)}
	if err := n.Decode(obj); err != nil {
		return err
	}

	m2, err := InstanceOf(m.Kind)
	if err != nil {
		if len(obj.Spec.Content) == 0 {
			m.Spec = nil
			return nil
		}
		m2.Spec = make(map[string]any)
	}

	m.Spec = m2.Spec

	return obj.Spec.Decode(m.Spec)
}
