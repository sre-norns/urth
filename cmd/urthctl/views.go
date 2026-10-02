package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity/cli"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

// The views below are how `get` prints Urth's resources: kubectl-style columns
// in a table, and the resource's manifest for -o yaml|json -- the document
// `apply -f` takes back.

type scenarioView struct{ urth.Scenario }

func (v scenarioView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ToManifest()) }

// MarshalYAML is the manifest as `apply` decodes it: in YAML a duration is
// "3s", where JSON has nanoseconds.
func (v scenarioView) MarshalYAML() (any, error) { return v.ToManifest(), nil }

func (scenarioView) TableHeader(wide bool) []string {
	header := []string{"NAME", "ENABLED", "TYPE", "STATUS", "AGE"}
	if wide {
		header = append(header, "SCHEDULE", "REQUIREMENTS", "NEXT RUN", "LAST RUN")
	}
	return header
}

func (v scenarioView) TableRow(wide bool) []any {
	r := v.Scenario
	lastStatus := "unknown"
	if len(r.Status.Results) > 0 {
		lastStatus = fmt.Sprintf("%v/%v", r.Status.Results[0].Status.Status, r.Status.Results[0].Status.Result)
	}
	probType := "<unknown>"
	if r.Spec.Prob.Kind != "" {
		probType = string(r.Spec.Prob.Kind)
	}
	row := []any{r.Name, r.Spec.IsActive, probType, lastStatus, age(r.ObjectMeta)}
	if wide {
		lastRunDuration, nextRun := "-", "-"
		if len(r.Status.Results) > 0 && r.Status.Results[0].Spec.TimeStarted != nil {
			latest := r.Status.Results[0].Spec
			lastRunDuration = maybeDuration(latest.TimeStarted, latest.TimeEnded)
		}
		if r.Status.NextRun != nil {
			nextRun = r.Status.NextRun.String()
		}
		row = append(row, orDash(string(r.Spec.RunSchedule)), r.Spec.Requirements, nextRun, lastRunDuration)
	}
	return row
}

type runnerView struct{ urth.Runner }

func (v runnerView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ToManifest()) }
func (v runnerView) MarshalYAML() (any, error)    { return v.ToManifest(), nil }

func (runnerView) TableHeader(wide bool) []string {
	header := []string{"NAME", "ENABLED", "ONLINE", "AGE"}
	if wide {
		header = append(header, "REQUIREMENTS", "DESCRIPTION")
	}
	return header
}

func (v runnerView) TableRow(wide bool) []any {
	r := v.Runner
	online := strconv.FormatUint(r.Status.NumberInstances, 10)
	if r.Spec.MaxInstances > 0 {
		online = fmt.Sprintf("%d/%d", r.Status.NumberInstances, r.Spec.MaxInstances)
	}
	row := []any{r.Name, r.Spec.IsActive, online, age(r.ObjectMeta)}
	if wide {
		row = append(row, r.Spec.Requirements, orDash(r.Spec.Description))
	}
	return row
}

type resultView struct{ urth.Result }

func (v resultView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ToManifest()) }
func (v resultView) MarshalYAML() (any, error)    { return v.ToManifest(), nil }

func (resultView) TableHeader(wide bool) []string {
	header := []string{"NAME", "DURATION", "STATUS", "AGE"}
	if wide {
		header = append(header, "RESULT", "KIND", "ARTIFACTS")
	}
	return header
}

func (v resultView) TableRow(wide bool) []any {
	r := v.Result
	row := []any{r.Name, maybeDuration(r.Spec.TimeStarted, r.Spec.TimeEnded), r.Status.Status, age(r.ObjectMeta)}
	if wide {
		row = append(row, r.Status.Result, r.Spec.ProbKind, r.Status.NumberArtifacts)
	}
	return row
}

type artifactView struct{ urth.Artifact }

func (v artifactView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ToManifest()) }
func (v artifactView) MarshalYAML() (any, error)    { return v.ToManifest(), nil }

func (artifactView) TableHeader(bool) []string {
	return []string{"NAME", "MIME TYPE", "AGE"}
}

func (v artifactView) TableRow(bool) []any {
	return []any{v.Name, orDash(v.Spec.MimeType), age(v.ObjectMeta)}
}

type dispatchFailureView struct{ urth.DispatchFailure }

func (v dispatchFailureView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ToManifest()) }
func (v dispatchFailureView) MarshalYAML() (any, error)    { return v.ToManifest(), nil }

func (dispatchFailureView) TableHeader(wide bool) []string {
	header := []string{"NAME", "REASON", "SCENARIO", "RUNNER", "RESOLVED", "AGE"}
	if wide {
		header = append(header, "REPORTER", "DELIVERIES", "RETRY", "DETAIL")
	}
	return header
}

func (v dispatchFailureView) TableRow(wide bool) []any {
	failure := v.DispatchFailure
	row := []any{
		failure.Name,
		failure.Spec.Reason,
		orDash(string(failure.Spec.ScenarioName)),
		orDash(failure.Labels[urth.LabelRunnerName]),
		resolvedLabel(failure),
		failureAge(failure),
	}
	if wide {
		row = append(row,
			failure.Spec.ReportedBy,
			failure.Spec.Deliveries,
			orDash(string(failure.Status.RetryResultName)),
			failure.Spec.Detail,
		)
	}
	return row
}

func views[T, V any](items []T, view func(T) V) []V {
	out := make([]V, len(items))
	for i, item := range items {
		out[i] = view(item)
	}
	return out
}

func age(meta manifest.ObjectMeta) string {
	if meta.UpdatedAt == nil {
		return cli.HumanizeAge(time.Time{})
	}
	return cli.HumanizeAge(*meta.UpdatedAt)
}

func maybeDuration(start *time.Time, end *time.Time) string {
	if start == nil {
		return "not-started"
	}
	if end == nil {
		return fmt.Sprintf("pending: %v", time.Since(*start).Round(time.Second))
	}

	return end.Sub(*start).Round(time.Second).String()
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}

	return value
}

type grantView struct{ manifest.ResourceManifest }

func (v grantView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ResourceManifest) }
func (v grantView) MarshalYAML() (any, error)    { return v.ResourceManifest, nil }

func (grantView) TableHeader(wide bool) []string {
	header := []string{"NAME", "RUNNER", "ROLES", "PHASE"}
	if wide {
		header = append(header, "UID", "VERSION")
	}
	return header
}

func (v grantView) TableRow(wide bool) []any {
	var spec urth.RunnerAuthorizationSpec
	var status urth.RunnerAuthorizationStatus
	// Registered kinds decode typed; convert through JSON so either form reads.
	if data, err := json.Marshal(v.Spec); err == nil {
		_ = json.Unmarshal(data, &spec)
	}
	if data, err := json.Marshal(v.Status); err == nil {
		_ = json.Unmarshal(data, &status)
	}
	roles := make([]string, len(spec.Roles))
	for i, role := range spec.Roles {
		roles[i] = string(role)
	}
	row := []any{v.Metadata.Name, orDash(string(spec.RunnerRef)), orDash(strings.Join(roles, ",")), orDash(status.Phase)}
	if wide {
		row = append(row, v.Metadata.UID, v.Metadata.Version)
	}
	return row
}

// decodeManifest reads one manifest, as JSON when it is a JSON document --
// what `get -o json` prints -- and as YAML otherwise. JSON is YAML too, but
// YAML's decoders read a JSON duration (nanoseconds) as an error.
//
// The status is dropped: it is the server's, and a document read back from
// `get` carries one -- a scenario's lists its latest runs, as manifests the
// server will not take back.
func decodeManifest(content []byte) (m manifest.ResourceManifest, err error) {
	if trimmed := bytes.TrimSpace(content); len(trimmed) > 0 && trimmed[0] == '{' {
		err = json.Unmarshal(content, &m)
	} else {
		err = yaml.Unmarshal(content, &m)
	}
	m.Status = nil
	return m, err
}
