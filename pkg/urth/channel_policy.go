package urth

import (
	"fmt"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"golang.org/x/mod/semver"
)

// JobRequirements defines the finite set of jobs accepted by a channel.
// An empty ProbeKinds set accepts no jobs. Durations are inclusive Go durations.
// Privileges lists the Worker privileges accepted jobs may need; a job needing
// one not listed is rejected, and every admitted Worker must hold all of them.
type JobRequirements struct {
	Labels      manifest.LabelSelector `json:"labels,omitempty" yaml:"labels,omitempty"`
	ProbeKinds  []string               `json:"probeKinds" yaml:"probeKinds"`
	Privileges  []string               `json:"privileges,omitempty" yaml:"privileges,omitempty"`
	MinDuration string                 `json:"minDuration,omitempty" yaml:"minDuration,omitempty"`
	MaxDuration string                 `json:"maxDuration,omitempty" yaml:"maxDuration,omitempty"`
}

type WorkerRequirements struct {
	Labels           manifest.LabelSelector `json:"labels,omitempty" yaml:"labels,omitempty"`
	Version          string                 `json:"version,omitempty" yaml:"version,omitempty"`
	ProbeVersions    map[string]string      `json:"probeVersions,omitempty" yaml:"probeVersions,omitempty"`
	RuntimeVersions  map[string]string      `json:"runtimeVersions,omitempty" yaml:"runtimeVersions,omitempty"`
	OperatingSystems []string               `json:"operatingSystems,omitempty" yaml:"operatingSystems,omitempty"`
	Architectures    []string               `json:"architectures,omitempty" yaml:"architectures,omitempty"`
	Privileges       []string               `json:"privileges,omitempty" yaml:"privileges,omitempty"`
	MinDuration      string                 `json:"minDuration,omitempty" yaml:"minDuration,omitempty"`
	MaxDuration      string                 `json:"maxDuration,omitempty" yaml:"maxDuration,omitempty"`
}

// WorkerCapabilities is a validated declaration, not remote attestation.
// Raw development versions remain visible but cannot satisfy version constraints.
type WorkerCapabilities struct {
	Version         string            `json:"version,omitempty" yaml:"version,omitempty"`
	OS              string            `json:"os" yaml:"os"`
	Architecture    string            `json:"architecture" yaml:"architecture"`
	ProbeVersions   map[string]string `json:"probeVersions" yaml:"probeVersions"`
	RuntimeVersions map[string]string `json:"runtimeVersions,omitempty" yaml:"runtimeVersions,omitempty"`
	Privileges      []string          `json:"privileges,omitempty" yaml:"privileges,omitempty"`
	MinDuration     string            `json:"minDuration" yaml:"minDuration"`
	MaxDuration     string            `json:"maxDuration" yaml:"maxDuration"`
}

func durationInterval(min, max string, defaults bool) (time.Duration, time.Duration, error) {
	if defaults {
		if min == "" {
			min = "1ns"
		}
		if max == "" {
			max = "1m"
		}
	}
	lo, e := time.ParseDuration(min)
	if e != nil || lo <= 0 {
		return 0, 0, fmt.Errorf("minDuration must be a positive Go duration")
	}
	hi, e := time.ParseDuration(max)
	if e != nil || hi < lo {
		return 0, 0, fmt.Errorf("maxDuration must be a Go duration at least minDuration")
	}
	return lo, hi, nil
}

// Version ranges are whitespace-separated comparators against full semantic
// versions. There are no wildcards, implicit latest versions, or OR expressions.
type versionComparator struct{ op, version string }

func parseVersionRange(constraint string) ([]versionComparator, error) {
	var result []versionComparator
	var lower, upper string
	lowerOpen, upperOpen := false, false
	for _, term := range strings.Fields(constraint) {
		op := "="
		target := term
		for _, prefix := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(term, prefix) {
				op = prefix
				target = strings.TrimPrefix(term, prefix)
				break
			}
		}
		target = "v" + strings.TrimPrefix(target, "v")
		core := strings.SplitN(strings.SplitN(target, "-", 2)[0], "+", 2)[0]
		if !semver.IsValid(target) || strings.Count(core, ".") != 2 {
			return nil, fmt.Errorf("invalid semantic version comparator %q", term)
		}
		result = append(result, versionComparator{op, target})
		if op == ">" || op == ">=" || op == "=" {
			if lower == "" || semver.Compare(target, lower) > 0 {
				lower = target
				lowerOpen = op == ">"
			} else if semver.Compare(target, lower) == 0 && op == ">" {
				lowerOpen = true
			}
		}
		if op == "<" || op == "<=" || op == "=" {
			if upper == "" || semver.Compare(target, upper) < 0 {
				upper = target
				upperOpen = op == "<"
			} else if semver.Compare(target, upper) == 0 && op == "<" {
				upperOpen = true
			}
		}
	}
	if constraint != "" && len(result) == 0 {
		return nil, fmt.Errorf("empty version range")
	}
	if lower != "" && upper != "" {
		cmp := semver.Compare(lower, upper)
		if cmp > 0 || cmp == 0 && (lowerOpen || upperOpen) {
			return nil, fmt.Errorf("contradictory version range %q", constraint)
		}
	}
	return result, nil
}
func versionMatches(raw, constraint string) (bool, error) {
	terms, err := parseVersionRange(constraint)
	if err != nil {
		return false, err
	}
	if len(terms) == 0 {
		return true, nil
	}
	v := "v" + strings.TrimPrefix(raw, "v")
	core := strings.SplitN(strings.SplitN(v, "-", 2)[0], "+", 2)[0]
	if !semver.IsValid(v) || strings.Count(core, ".") != 2 {
		return false, nil
	}
	for _, term := range terms {
		cmp := semver.Compare(v, term.version)
		op := term.op
		if !(op == "=" && cmp == 0 || op == ">=" && cmp >= 0 || op == "<=" && cmp <= 0 || op == ">" && cmp > 0 || op == "<" && cmp < 0) {
			return false, nil
		}
	}
	return true, nil
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func (c WorkerCapabilities) Validate() error {
	if c.OS == "" || c.Architecture == "" || len(c.ProbeVersions) == 0 {
		return fmt.Errorf("capabilities require os, architecture and probeVersions")
	}
	if _, _, err := durationInterval(c.MinDuration, c.MaxDuration, false); err != nil {
		return err
	}
	for k := range c.ProbeVersions {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("empty probe capability")
		}
	}
	return nil
}

func (policy RunnerSpec) ValidatePolicy() error {
	if _, e := policy.JobRequirements.Labels.AsSelector(); e != nil {
		return fmt.Errorf("jobRequirements.labels: %w", e)
	}
	if _, e := policy.WorkerRequirements.Labels.AsSelector(); e != nil {
		return fmt.Errorf("workerRequirements.labels: %w", e)
	}
	lo, hi, e := durationInterval(policy.JobRequirements.MinDuration, policy.JobRequirements.MaxDuration, true)
	if e != nil {
		return e
	}
	w := policy.WorkerRequirements
	if w.MinDuration != "" {
		d, e := time.ParseDuration(w.MinDuration)
		if e != nil || d <= 0 || d > lo {
			return fmt.Errorf("worker minDuration cannot exceed job minDuration")
		}
	}
	if w.MaxDuration != "" {
		d, e := time.ParseDuration(w.MaxDuration)
		if e != nil || d < hi {
			return fmt.Errorf("worker maxDuration cannot be below job maxDuration")
		}
	}
	for _, v := range append([]string{w.Version}, mapValues(w.ProbeVersions, w.RuntimeVersions)...) {
		if _, e := versionMatches("0.0.0", v); e != nil {
			return e
		}
	}
	seen := map[string]bool{}
	for _, k := range policy.JobRequirements.ProbeKinds {
		if strings.TrimSpace(k) == "" || seen[k] {
			return fmt.Errorf("probeKinds must contain distinct nonempty kinds")
		}
		seen[k] = true
	}
	if e := validatePrivileges("jobRequirements.privileges", policy.JobRequirements.Privileges); e != nil {
		return e
	}
	if e := validatePrivileges("workerRequirements.privileges", w.Privileges); e != nil {
		return e
	}
	if e := policy.PropagatedLabels.Validate(); e != nil {
		return e
	}
	for k := range policy.PropagatedLabels {
		if strings.HasPrefix(k, LabelsPrefix) {
			return fmt.Errorf("propagatedLabels cannot use reserved key %q", k)
		}
	}
	return nil
}
func validatePrivileges(field string, values []string) error {
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] || !contains(prob.KnownPrivileges(), v) {
			return fmt.Errorf("%s must contain distinct known privileges %v, got %q", field, prob.KnownPrivileges(), v)
		}
		seen[v] = true
	}
	return nil
}

// ValidateProbeKinds rejects channel probe kinds this server has no prober for.
// A misspelled kind would otherwise be accepted and then, through the coverage
// rule, refuse every Worker at enrollment. It needs the probers linked into the
// binary, so it runs on the server's write path rather than in ValidatePolicy,
// which clients also call when they decode a Runner.
func (policy RunnerSpec) ValidateProbeKinds() error {
	for _, k := range policy.JobRequirements.ProbeKinds {
		if _, ok := prob.FindRegistration(prob.Kind(k)); !ok {
			return fmt.Errorf("jobRequirements.probeKinds: unknown probe kind %q", k)
		}
	}
	return nil
}

// jobPrivileges reports what a concrete job needs beyond its probe kind. A job
// whose kind this binary cannot classify needs something no Worker can hold.
func jobPrivileges(s ExecutionSnapshot) ([]string, error) {
	registration, ok := prob.FindRegistration(s.Prob.Kind)
	if !ok {
		return nil, fmt.Errorf("probe kind %q is not known to this server", s.Prob.Kind)
	}
	if registration.Privileges == nil {
		return nil, nil
	}
	return registration.Privileges(s.Prob.Spec)
}

func mapValues(maps ...map[string]string) []string {
	var r []string
	for _, m := range maps {
		for _, v := range m {
			r = append(r, v)
		}
	}
	return r
}

func (policy RunnerSpec) AcceptsJob(s ExecutionSnapshot, labels manifest.Labels) error {
	if err := policy.ValidatePolicy(); err != nil {
		return err
	}
	if !contains(policy.JobRequirements.ProbeKinds, string(s.Prob.Kind)) {
		return fmt.Errorf("probe kind rejected by channel")
	}
	required, e := jobPrivileges(s)
	if e != nil {
		return fmt.Errorf("job privileges cannot be determined: %w", e)
	}
	for _, p := range required {
		if !contains(policy.JobRequirements.Privileges, p) {
			return fmt.Errorf("job privilege %q rejected by channel", p)
		}
	}
	selector, e := policy.JobRequirements.Labels.AsSelector()
	if e != nil || !selector.Matches(labels) {
		return fmt.Errorf("job labels rejected by channel")
	}
	lo, hi, _ := durationInterval(policy.JobRequirements.MinDuration, policy.JobRequirements.MaxDuration, true)
	d := executionDuration(s)
	if d < lo || d > hi {
		return fmt.Errorf("job duration rejected by channel")
	}
	return nil
}
func executionDuration(s ExecutionSnapshot) time.Duration {
	if s.Prob.Timeout > 0 {
		return s.Prob.Timeout
	}
	return time.Minute
}

func (policy RunnerSpec) AdmitCapabilities(c WorkerCapabilities, labels manifest.Labels) error {
	if e := policy.ValidatePolicy(); e != nil {
		return e
	}
	if e := c.Validate(); e != nil {
		return e
	}
	w := policy.WorkerRequirements
	selector, e := w.Labels.AsSelector()
	if e != nil || !selector.Matches(labels) {
		return fmt.Errorf("worker labels rejected by channel")
	}
	if len(w.OperatingSystems) > 0 && !contains(w.OperatingSystems, c.OS) || len(w.Architectures) > 0 && !contains(w.Architectures, c.Architecture) {
		return fmt.Errorf("worker platform rejected by channel")
	}
	for _, v := range w.Privileges {
		if !contains(c.Privileges, v) {
			return fmt.Errorf("required privilege missing")
		}
	}
	if ok, e := versionMatches(c.Version, w.Version); e != nil || !ok {
		return fmt.Errorf("worker version rejected by channel")
	}
	for i, req := range []map[string]string{w.ProbeVersions, w.RuntimeVersions} {
		actual := c.ProbeVersions
		if i == 1 {
			actual = c.RuntimeVersions
		}
		for k, v := range req {
			raw, present := actual[k]
			if !present {
				return fmt.Errorf("required capability %q missing", k)
			}
			if ok, e := versionMatches(raw, v); e != nil || !ok {
				return fmt.Errorf("capability version %q rejected", k)
			}
		}
	}
	for _, p := range policy.JobRequirements.Privileges {
		if !contains(c.Privileges, p) {
			return fmt.Errorf("worker lacks privilege %q accepted jobs may need", p)
		}
	}
	for _, k := range policy.JobRequirements.ProbeKinds {
		if _, ok := c.ProbeVersions[k]; !ok {
			return fmt.Errorf("worker cannot cover channel probe %q", k)
		}
	}
	lo, hi, _ := durationInterval(policy.JobRequirements.MinDuration, policy.JobRequirements.MaxDuration, true)
	if w.MinDuration != "" {
		lo, _ = time.ParseDuration(w.MinDuration)
	}
	if w.MaxDuration != "" {
		hi, _ = time.ParseDuration(w.MaxDuration)
	}
	cl, ch, _ := durationInterval(c.MinDuration, c.MaxDuration, false)
	if cl > lo || ch < hi {
		return fmt.Errorf("worker cannot cover channel duration interval")
	}
	return nil
}
func (c WorkerCapabilities) CanExecute(s ExecutionSnapshot) bool {
	if c.Validate() != nil {
		return false
	}
	if _, ok := c.ProbeVersions[string(s.Prob.Kind)]; !ok {
		return false
	}
	required, err := jobPrivileges(s)
	if err != nil {
		return false
	}
	for _, p := range required {
		if !contains(c.Privileges, p) {
			return false
		}
	}
	lo, hi, _ := durationInterval(c.MinDuration, c.MaxDuration, false)
	d := executionDuration(s)
	return d >= lo && d <= hi
}
