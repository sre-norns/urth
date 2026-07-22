// Package natsq carries Urth jobs and run logs over NATS and JetStream.
//
// It occupies the same position as pkg/redqueue -- an implementation of
// urth.Scheduler plus the worker-side machinery to consume what it publishes --
// so that both transports can be built and run side by side while the migration
// described in ADR 0004 proceeds.
//
// The package is named natsq rather than nats so that it does not shadow the
// NATS client import in files that need both.
package natsq

import (
	"encoding/json"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// DispatchEnvelopeVersion is the schema version of the messages this package
// publishes. A consumer that does not recognise the version must not guess at
// the payload: it terminates the message rather than executing something it
// only partly understands.
const DispatchEnvelopeVersion = 1

// DispatchEnvelope is what travels on urth.v1.jobs.<runner-uid>.
//
// It is deliberately almost empty. The prototype's asynq transport marshalled
// the whole urth.Job -- prob kind, spec, and script -- into the queue, which
// meant that anything able to read the queue could read every scenario's
// script. ADR 0004 forbids that: the envelope says only that work exists and
// which Result it belongs to, and a worker learns what to actually run by
// claiming the job through the authenticated API.
//
// So this struct carries no prob, no credentials, no artifacts, and no
// independently mutable copy of the scenario. Adding any of those back
// re-opens the hole.
type DispatchEnvelope struct {
	// SchemaVersion identifies the envelope layout. See DispatchEnvelopeVersion.
	SchemaVersion int `json:"schemaVersion"`

	// ResultUID identifies the Result this dispatch is for. It is the Result,
	// not the Scenario, because a Result is the single run being asked for.
	ResultUID manifest.ResourceID `json:"resultUid"`

	// ResultVersion is the Result's version at publication time. A worker
	// presents it when claiming so the API can reject a dispatch that has been
	// overtaken by a newer state.
	ResultVersion manifest.Version `json:"resultVersion"`

	// ScenarioName is carried for logging and for addressing the scenario-scoped
	// Result endpoints. It is a convenience, never an authorisation input.
	ScenarioName manifest.ResourceName `json:"scenarioName"`

	// RunnerUID is the runner this job was dispatched to. It matches the subject
	// the message was published on, and lets a worker detect a message that
	// reached it through a misconfigured consumer.
	RunnerUID manifest.ResourceID `json:"runnerUid"`

	// DispatchID uniquely identifies this dispatch attempt. It is used three
	// ways, which is why it must be stable for a given attempt: as the
	// Nats-Msg-Id for JetStream's duplicate window, as the RunID recorded on the
	// Result, and as the idempotency key that lets a worker retry a claim whose
	// response it lost without starting a second run.
	DispatchID string `json:"dispatchId"`

	// Trace carries tracing metadata across the queue boundary.
	Trace map[string]string `json:"trace,omitempty"`
}

// Validate reports whether the envelope is one this build can act on.
func (e DispatchEnvelope) Validate() error {
	if e.SchemaVersion != DispatchEnvelopeVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, e.SchemaVersion, DispatchEnvelopeVersion)
	}
	if e.ResultUID == "" {
		return fmt.Errorf("%w: no result UID", ErrInvalidEnvelope)
	}
	if e.RunnerUID == "" {
		return fmt.Errorf("%w: no runner UID", ErrInvalidEnvelope)
	}
	if e.DispatchID == "" {
		return fmt.Errorf("%w: no dispatch ID", ErrInvalidEnvelope)
	}

	return nil
}

// MarshalEnvelope encodes a dispatch for publication.
//
// JSON rather than the YAML the asynq path used: this is a versioned wire
// format between separately deployed components, and it is worth being able to
// read a message straight out of `nats stream view` when diagnosing a stuck
// queue.
func MarshalEnvelope(e DispatchEnvelope) ([]byte, error) {
	return json.Marshal(&e)
}

// UnmarshalEnvelope decodes and validates a dispatch.
func UnmarshalEnvelope(data []byte) (DispatchEnvelope, error) {
	var e DispatchEnvelope
	if err := json.Unmarshal(data, &e); err != nil {
		return e, fmt.Errorf("%w: %w", ErrInvalidEnvelope, err)
	}

	return e, e.Validate()
}
