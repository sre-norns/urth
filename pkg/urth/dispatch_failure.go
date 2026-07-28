package urth

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrInvalidDispatchFailure marks a failure report the API will not record.
//
// Distinct from a store error because the two want opposite handling by a
// reporting worker: a report the API refuses as malformed will be refused again,
// while a report it could not store is worth retrying.
var ErrInvalidDispatchFailure = errors.New("invalid dispatch failure report")

// DispatchFailureReason categorises why a dispatch stopped making progress.
//
// The categories are separated because an operator does different things about
// them. A malformed envelope is a bug in Urth; a misroute is a broken consumer
// binding; a policy refusal is a placement or admission mistake; an exhausted
// delivery count usually means workers that keep failing to claim. Collapsing
// them into "dispatch failed" would tell an operator that something is wrong and
// nothing about where to look -- which is the state task 012 exists to fix,
// since all four currently produce only a line in a worker's log.
type DispatchFailureReason string

const (
	// ReasonMalformedEnvelope: the message could not be parsed. Redelivery
	// cannot help, because it will not parse next time either.
	ReasonMalformedEnvelope DispatchFailureReason = "malformed-envelope"

	// ReasonMisroutedDispatch: the message named a runner other than the one
	// whose consumer delivered it. Executing it would defeat placement.
	ReasonMisroutedDispatch DispatchFailureReason = "misrouted-dispatch"

	// ReasonPolicyRefused: the API permanently refused the claim -- the worker
	// may not run this job, and asking again will not change that.
	ReasonPolicyRefused DispatchFailureReason = "policy-refused"

	// ReasonMaxDeliveryExhausted: the broker gave up redelivering. JetStream
	// does not remove a work-queue message for this, so without a record the
	// dispatch simply stops moving and nothing says why.
	ReasonMaxDeliveryExhausted DispatchFailureReason = "max-delivery-exhausted"

	// ReasonUndeliverableDispatch: the relay could never publish the message at
	// all -- an envelope this build cannot encode, a row from a schema it does
	// not know, a Result that has moved on since the row was written. The
	// message never reached the broker, so no worker can report this; the relay
	// is the only component that sees it.
	//
	// A dispatch with no runner assigned is deliberately *not* filed here. That
	// is placement finding nothing rather than a fault, and it is recorded on the
	// run alone; see ReasonNoEligibleRunner and PermanentDispatchSink.
	ReasonUndeliverableDispatch DispatchFailureReason = "undeliverable-dispatch"
)

// DispatchFailureReasons lists every recognised category.
func DispatchFailureReasons() []DispatchFailureReason {
	return []DispatchFailureReason{
		ReasonMalformedEnvelope,
		ReasonMisroutedDispatch,
		ReasonPolicyRefused,
		ReasonMaxDeliveryExhausted,
		ReasonUndeliverableDispatch,
	}
}

// IsValid reports whether a reason is one this build recognises.
//
// Checked on the way in rather than trusted: the reason is a label value and
// part of an idempotency key, so an unrecognised one would both fail label
// validation and open a second record for the same failure.
func (r DispatchFailureReason) IsValid() bool {
	return slices.Contains(DispatchFailureReasons(), r)
}

// String implements fmt.Stringer.
func (r DispatchFailureReason) String() string {
	return string(r)
}

// TerminatesDispatch reports whether this category means the message is gone
// from the queue.
//
// A worker that terminates a message has removed it; a delivery count that ran
// out has not. The difference decides whether the dispatch still needs
// withdrawing, and getting it wrong either leaves a message nothing will claim
// or asks the broker to delete something already deleted.
func (r DispatchFailureReason) TerminatesDispatch() bool {
	return r != ReasonMaxDeliveryExhausted
}

// Well-known dispatch-failure labels.
const (
	// LabelDispatchFailureReason is the category, as a label so that "every
	// dispatch that died of X" is a query rather than a scan.
	LabelDispatchFailureReason = LabelsPrefix + "dispatch-failure.reason"

	// LabelDispatchFailureResolved separates failures an operator has dealt
	// with from the ones still waiting. It is the coarse filter behind "what is
	// still wrong", which is the question a dead-letter queue exists to answer.
	LabelDispatchFailureResolved = LabelsPrefix + "dispatch-failure.resolved"

	// LabelDispatchFailureReporter records who observed the failure: a worker
	// that terminated the message, or the control plane consuming a broker
	// advisory. Worth separating -- a failure only ever reported by advisories
	// means workers are not reporting, which is itself a fault.
	LabelDispatchFailureReporter = LabelsPrefix + "dispatch-failure.reporter"
)

// DispatchFailureReporter names the component that observed a failure.
type DispatchFailureReporter string

const (
	// ReporterWorker: a worker reported it before terminating the message.
	ReporterWorker DispatchFailureReporter = "worker"

	// ReporterControlPlane: the control plane observed a broker advisory.
	ReporterControlPlane DispatchFailureReporter = "control-plane"
)

// KindDispatchFailure is the resource kind for dead-lettered dispatches.
const KindDispatchFailure manifest.Kind = "dispatchFailures"

// DispatchFailureSpecVersion is the current DispatchFailureSpec layout.
const DispatchFailureSpecVersion = 1

// DispatchDetailLimit bounds the stored failure detail.
//
// Bounded because the detail is attacker-influenced in the malformed case: it
// describes a message this build could not parse, and an unbounded copy of that
// is a way to write arbitrary bytes into the database.
const DispatchDetailLimit = 1024

// DispatchFailureSpec is what happened, recorded once and never revised.
//
// It is a resource rather than internal plumbing like DispatchOutboxEntry
// because an operator has to find these: by runner, by reason, by scenario, over
// a time range. Those are label and range queries, which is what the resource
// API already does for every other kind, and a private table would need all of
// it rebuilt.
//
// It deliberately carries no probe definition, no run capability, and no raw
// message payload. ADR 0004 keeps credentials out of dispatch envelopes for the
// same reason they stay out of here: a dead-letter record is read by more people
// and kept longer than the message it describes.
type DispatchFailureSpec struct {
	// SchemaVersion versions this record's layout.
	SchemaVersion int `json:"schemaVersion" yaml:"schemaVersion"`

	// Reason is the category. With EventUID it forms the idempotency key: the
	// same dispatch failing the same way twice is one record, while a dispatch
	// that later fails a different way is genuinely new information.
	Reason DispatchFailureReason `json:"reason" yaml:"reason"`

	// EventUID identifies the dispatch, matching DispatchOutboxEntry.EventUID.
	// It is what ties a failure to the outbox row and the message.
	EventUID string `json:"eventUID" yaml:"eventUID"`

	// DispatchID is the identifier the worker was given for this delivery, when
	// there was one. Empty for a malformed envelope, where it could not be read.
	DispatchID string `json:"dispatchID,omitempty" yaml:"dispatchID,omitempty"`

	ResultUID     manifest.ResourceID   `json:"resultUID" yaml:"resultUID"`
	ResultVersion manifest.Version      `json:"resultVersion" yaml:"resultVersion"`
	ScenarioName  manifest.ResourceName `json:"scenarioName,omitempty" yaml:"scenarioName,omitempty"`
	RunnerUID     manifest.ResourceID   `json:"runnerUID,omitempty" yaml:"runnerUID,omitempty"`

	// ReportedBy names the component that observed the failure.
	ReportedBy DispatchFailureReporter `json:"reportedBy" yaml:"reportedBy"`

	// WorkerUID is the worker that reported it, when one did. Taken from the
	// reporting worker's session, never from its request body -- the same rule
	// the claim path follows, and for the same reason.
	WorkerUID manifest.ResourceID `json:"workerUID,omitempty" yaml:"workerUID,omitempty"`

	// Deliveries is how many times the broker had delivered the message. Zero
	// when the reporter did not know.
	Deliveries int `json:"deliveries,omitempty" yaml:"deliveries,omitempty"`

	// Detail is a short, redacted description for a human. It is never parsed
	// and nothing branches on it; Reason is what code reads.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`

	// OccurredAt is when the failure happened, as distinct from when it was
	// recorded. A worker that could not reach the API reports late, and the gap
	// between the two is worth being able to see.
	OccurredAt time.Time `json:"occurredAt" yaml:"occurredAt"`
}

// DispatchFailureStatus is what has been done about a failure.
//
// Separate from the spec because the spec is history and this is not: an
// operator resolves a failure, and a retry records a new run against it. Nothing
// here rewrites what happened.
type DispatchFailureStatus struct {
	// Resolved marks a failure an operator has finished with, whether by retry
	// or by deciding none is wanted. Unresolved failures are the working set.
	Resolved bool `json:"resolved" yaml:"resolved"`

	// ResolvedAt is when it was resolved.
	ResolvedAt *time.Time `json:"resolvedAt,omitempty" yaml:"resolvedAt,omitempty"`

	// RetryResultUID is the run created by a retry, when one was requested.
	//
	// This is the traceable relation ADR 0004 requires of a retry: the failed
	// Result is immutable history and is never reopened, so the only honest link
	// between attempt and re-attempt is a forward reference recorded here.
	RetryResultUID manifest.ResourceID `json:"retryResultUID,omitempty" yaml:"retryResultUID,omitempty"`

	// RetryResultName is the retry's name, so an operator reading this record
	// does not need a second lookup to go and find the run.
	RetryResultName manifest.ResourceName `json:"retryResultName,omitempty" yaml:"retryResultName,omitempty"`
}

// DispatchFailure is one dead-lettered dispatch.
type DispatchFailure manifest.StatefulResource[DispatchFailureSpec, DispatchFailureStatus]

// GetSpec implements the resource interface used by the store.
func (r DispatchFailure) GetSpec() any { return r.Spec }

// ToManifest renders the resource for the API.
func (r DispatchFailure) ToManifest() manifest.ResourceManifest {
	return manifest.ToManifestWithStatus(manifest.StatefulResource[DispatchFailureSpec, DispatchFailureStatus](r))
}

// NewDispatchFailure converts a manifest into the model.
func NewDispatchFailure(m manifest.ResourceManifest) (DispatchFailure, error) {
	e, err := manifest.ManifestAsStatefulResource[DispatchFailureSpec, DispatchFailureStatus](m)
	entry := DispatchFailure(e)
	if err != nil {
		return entry, fmt.Errorf("failed to convert resource manifest into a DispatchFailure model: %w", err)
	}

	return entry, nil
}

// DispatchFailureName derives the record's resource name from its identity.
//
// Deterministic rather than random, and derived from exactly the pair that makes
// a failure unique. That is what makes recording idempotent without a read: the
// second report of the same failure collides on the name's unique index instead
// of racing a check against an insert.
func DispatchFailureName(eventUID string, reason DispatchFailureReason) manifest.ResourceName {
	return manifest.ResourceName(LabelSafeValue(fmt.Sprintf("%s.%s", eventUID, reason)))
}

// ReportDispatchFailureRequest is a worker's report that a dispatch is dead.
//
// The worker sends this *before* terminating the message, so that a failed
// report leaves the message in the queue rather than removing the only evidence
// that the dispatch existed. The API refusing the report is therefore a reason
// to leave the message alone, not to terminate it anyway.
type ReportDispatchFailureRequest struct {
	// Reason is the category the worker decided on.
	Reason DispatchFailureReason `form:"reason" json:"reason" yaml:"reason"`

	// EventUID identifies the dispatch. Required: it is half the idempotency key.
	EventUID string `form:"eventUID" json:"eventUID" yaml:"eventUID"`

	// DispatchID is the delivery identifier, when the worker could read one.
	DispatchID string `form:"dispatchID,omitempty" json:"dispatchID,omitempty" yaml:"dispatchID,omitempty"`

	// ResultUID names the run, when the worker could read one. A malformed
	// envelope may not yield it, and the report is still worth making.
	ResultUID manifest.ResourceID `form:"resultUID,omitempty" json:"resultUID,omitempty" yaml:"resultUID,omitempty"`

	// ResultVersion is the version the dispatch was for.
	ResultVersion manifest.Version `form:"resultVersion,omitempty" json:"resultVersion,omitempty" yaml:"resultVersion,omitempty"`

	// Deliveries is the broker's delivery count for this message.
	Deliveries int `form:"deliveries,omitempty" json:"deliveries,omitempty" yaml:"deliveries,omitempty"`

	// Detail is a short human-readable description. Truncated on write.
	Detail string `form:"detail,omitempty" json:"detail,omitempty" yaml:"detail,omitempty"`
}

// Validate checks a worker's report before it is trusted.
//
// The runner, worker, and the authority to report at all come from the session,
// so what is left to check is that the report identifies a dispatch and names a
// category this build knows.
func (r ReportDispatchFailureRequest) Validate() error {
	if !r.Reason.IsValid() {
		return fmt.Errorf("%w: unknown reason %q", ErrInvalidDispatchFailure, r.Reason)
	}

	if r.EventUID == "" {
		return fmt.Errorf("%w: report requires an event UID", ErrInvalidDispatchFailure)
	}

	return nil
}

// RetryDispatchFailureRequest asks for a new attempt at a failed dispatch.
//
// It carries no execution input on purpose. A retry re-runs what the failed run
// was created to execute, copied from that run's own execution snapshot; letting
// the caller supply a definition here would make "retry" a way to run something
// the scenario never said, under the identity of a run that failed.
type RetryDispatchFailureRequest struct {
	// Resolve marks the failure resolved once the retry is created. Defaults to
	// true through the API surface; an operator retrying is done with it.
	Resolve *bool `form:"resolve,omitempty" json:"resolve,omitempty" yaml:"resolve,omitempty"`
}

// RetryDispatchFailureResponse is what a retry returns: the failure as it now
// stands, and the run created for it.
//
// Both, rather than just the new run, because the operator surface that
// triggered the retry has to show the record as resolved and linked without a
// second round trip -- and because the link is the part that is easy to lose.
type RetryDispatchFailureResponse struct {
	Failure manifest.ResourceManifest `json:"failure" yaml:"failure"`
	Retry   manifest.ResourceManifest `json:"retry" yaml:"retry"`
}

// dispatchFailureLabels derives the server-owned labels for a failure record.
//
// Server-derived, like every other label snapshot in this system: a reporter
// does not get to say which runner or reason its failure is filed under.
func dispatchFailureLabels(spec DispatchFailureSpec, runnerName manifest.ResourceName) manifest.Labels {
	labels := manifest.Labels{
		LabelDispatchFailureReason:   spec.Reason.String(),
		LabelDispatchFailureResolved: "false",
		LabelDispatchFailureReporter: string(spec.ReportedBy),
	}

	putLabel(labels, LabelResultUID, string(spec.ResultUID))
	putLabel(labels, LabelScenarioName, string(spec.ScenarioName))
	putLabel(labels, LabelRunnerUID, string(spec.RunnerUID))
	putLabel(labels, LabelRunnerName, string(runnerName))
	putLabel(labels, LabelWorkerUID, string(spec.WorkerUID))

	return labels
}

// truncateDetail bounds stored failure detail.
func truncateDetail(detail string) string {
	if len(detail) > DispatchDetailLimit {
		return detail[:DispatchDetailLimit]
	}

	return detail
}
