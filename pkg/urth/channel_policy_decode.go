package urth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/yaml.v3"
)

// UnmarshalJSON rejects obsolete and misspelled Runner policy fields.
func (policy *RunnerSpec) UnmarshalJSON(data []byte) error {
	type plain RunnerSpec
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, ok := raw["requirements"]; ok {
		return fmt.Errorf("runner spec.requirements is obsolete; use jobRequirements and workerRequirements")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	var v plain
	if err := d.Decode(&v); err != nil {
		return err
	}
	*policy = RunnerSpec(v)
	return nil
}

// UnmarshalYAML applies the same strict policy schema as JSON.
func (policy *RunnerSpec) UnmarshalYAML(n *yaml.Node) error {
	var raw map[string]any
	if err := n.Decode(&raw); err != nil {
		return err
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return policy.UnmarshalJSON(data)
}
