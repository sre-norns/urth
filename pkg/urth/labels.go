package urth

import "strings"

// LabelSafeValue converts an arbitrary string into something usable as a label
// value, returning "" when nothing usable remains -- in which case the label
// should be left off rather than written empty, since an empty value is not
// valid either.
//
// Several labels are derived from values that were never constrained to the
// label grammar, and each one has bitten:
//
//   - a MIME type contains '/' ("text/plain"),
//   - the puppeteer prober derives an artifact's kind from a file extension,
//     which leads with '.' (".png"),
//   - a build version derived from VCS state carries a '+' suffix
//     ("v0.0.0-20260720134510-2c9cd4c33f2d+dirty").
//
// The consequences differ by path. Artifact labels are merged by the server
// after the manifest has been validated, so a malformed value is persisted
// unnoticed -- and a malformed label is precisely what breaks the audit queries
// these labels exist to serve. Worker labels are validated on registration, so
// there the same mistake is fatal: a worker built from a dirty tree cannot
// register at all.
func LabelSafeValue(value string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '_', r == '.', r == '-':
			return r
		default:
			return '-'
		}
	}, value)

	// The grammar requires the first and last characters to be alphanumeric.
	return strings.TrimFunc(mapped, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9')
	})
}

// putLabel sets a label, skipping it when the value cannot be represented.
func putLabel(labels map[string]string, key, value string) {
	if safe := LabelSafeValue(value); safe != "" {
		labels[key] = safe
	}
}

// Common prefix for Urth managed resources

const (
	LabelsPrefix = "urth/"

	LabelWorkerCapPrefix     = LabelsPrefix + "capability."
	LabelWorkerCapProbPrefix = LabelWorkerCapPrefix + "prob."

	// Well-known worker labels:
	LabelWorkerOS           = LabelsPrefix + "worker.os"
	LabelWorkerArch         = LabelsPrefix + "worker.arch"
	LabelWorkerBuildVersion = LabelsPrefix + "worker.build.version"
	LabelWorkerName         = LabelsPrefix + "worker.name"
	LabelWorkerUID          = LabelsPrefix + "worker.uid"
	LabelWorkerVersion      = LabelsPrefix + "worker.version"

	// Well-known runner labels:
	LabelRunnerName    = LabelsPrefix + "runner.name"
	LabelRunnerUID     = LabelsPrefix + "runner.uid"
	LabelRunnerVersion = LabelsPrefix + "runner.version"

	// Well-known scenario labels:
	LabelScenarioName    = LabelsPrefix + "scenario.name"
	LabelScenarioUID     = LabelsPrefix + "scenario.uid"
	LabelScenarioVersion = LabelsPrefix + "scenario.version"
	LabelScenarioKind    = LabelsPrefix + "scenario.kind"

	// Well-known result labels:
	LabelResultName    = LabelsPrefix + "result.name"
	LabelResultUID     = LabelsPrefix + "result.uid"
	LabelResultVersion = LabelsPrefix + "result.version"

	LabelResultJobState = LabelsPrefix + "result.state"
	LabelResultStatus   = LabelsPrefix + "result.result"

	LabelResultMessageID = "run.messageId"

	// Well-known artifact labels:
	LabelArtifactKind = LabelsPrefix + "artifact.kind"
	LabelArtifactMime = LabelsPrefix + "artifact.mime"

	// LabelArtifactDataClass records what an artifact's content may expose:
	// `clean`, `redacted`, `secret-bearing` or `unknown`. See prob.DataClass.
	//
	// Exposing the classification as a label means retention policy, access
	// control and audits can select on it the same way as on any other resource
	// property -- "every artifact still held that may carry credentials" is a
	// label query rather than a scan of stored blobs.
	LabelArtifactDataClass = LabelsPrefix + "artifact.data-class"

	// LabelArtifactMayContainSecrets is the coarse form of
	// LabelArtifactDataClass, for the common case of separating artifacts that
	// are known to be free of credentials from everything else. Unclassified
	// artifacts count as unsafe, so this is `true` for them.
	LabelArtifactMayContainSecrets = LabelsPrefix + "artifact.may-contain-secrets"
)
