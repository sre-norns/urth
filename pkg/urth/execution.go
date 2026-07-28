package urth

import (
	"errors"
	"fmt"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrNoExecutionSnapshot reports a Result that cannot be executed because it
// carries no record of what it was created to run.
//
// It is a migration condition rather than a bug: every Result written before the
// snapshot existed is in this state. The important part is that it is an error at
// all -- the tempting alternative, filling the gap from the scenario as it stands
// now, is precisely the behaviour the snapshot exists to remove.
var ErrNoExecutionSnapshot = errors.New("result carries no execution snapshot")

// ExecutionSnapshot is the immutable execution input of one run.
//
// A Result is a single execution attempt, and this is what that attempt was asked
// to do. It is copied out of the Scenario at the moment the run is scheduled and
// never revisited, because a Scenario is a mutable resource and a run is not: a
// Result created on Monday and claimed on Tuesday must execute Monday's probe.
// Reloading the Scenario at claim time -- which is what this replaces -- meant an
// edit could silently change what an already-queued run did, without changing the
// Result's version and without leaving a trace in its history.
//
// The scenario identity is stored alongside the probe for the same reason
// ExecutorRef stores names beside IDs: the run's audit trail has to stay readable
// after the Scenario is edited or deleted, which is exactly when someone is
// looking at it.
//
// The effective execution budget lives in Prob.Timeout, captured with the rest of
// the definition. It is not duplicated at this level: two copies of a timeout are
// two things that can disagree, and the server's own ceiling on a run is a
// separate decision made at claim time (see clampRunDuration).
//
// Treat the whole value as secret-bearing. A probe definition is a script or a
// request template and may carry credentials, so it is disclosed only in the
// response to an authenticated claim -- never through Result list/get, and never
// logged. That is why ResultSpec hides it from serialization rather than relying
// on callers to remember.
type ExecutionSnapshot struct {
	// ScenarioUID identifies the scenario this run was created from. It is the
	// UID rather than the name because a name can be reused.
	ScenarioUID manifest.ResourceID `json:"scenarioUid,omitempty"`

	// ScenarioName is the scenario's name at scheduling time, kept so the run
	// stays readable after the scenario is renamed or removed.
	ScenarioName manifest.ResourceName `json:"scenarioName,omitempty"`

	// ScenarioVersion is the exact revision this run executes. It is the answer
	// to "which version of the scenario produced this result".
	ScenarioVersion manifest.Version `json:"scenarioVersion,omitempty"`

	// Requirements is the placement selector as it stood when the run was
	// scheduled. Placement already happened by then, so this is not re-evaluated
	// here; it is recorded so a later admission check -- and an operator asking
	// why this run went to that runner -- reads the requirement the decision was
	// actually made against.
	Requirements manifest.LabelSelector `json:"requirements"`

	// Prob is the complete executable definition, in the same typed form the
	// Scenario holds and a Worker expects.
	Prob prob.Manifest `json:"prob"`
}

// NewExecutionSnapshot captures a scenario as the execution input of one run.
//
// The scenario must be one loaded in full: an association gorm has not populated
// carries an empty Prob, and a snapshot taken from it would be a run with nothing
// to do. Validate rejects that rather than persisting it.
func NewExecutionSnapshot(scenario Scenario) ExecutionSnapshot {
	return ExecutionSnapshot{
		ScenarioUID:     scenario.UID,
		ScenarioName:    scenario.Name,
		ScenarioVersion: scenario.Version,
		Requirements:    scenario.Spec.Requirements,
		Prob:            scenario.Spec.Prob,
	}
}

// IsZero reports whether a Result carries no execution snapshot at all.
//
// This is how a row written before the snapshot existed is recognised: the column
// reads back as NULL, which the JSON serializer turns into the zero value.
func (s ExecutionSnapshot) IsZero() bool {
	return s.ScenarioUID == "" && s.Prob.Kind == "" && s.Prob.Spec == nil
}

// Validate reports whether this snapshot describes something a Worker could run.
//
// It is checked before the Result is persisted, so that a stored pending Result
// is always either executable or explicitly terminal. A Result that only turns
// out to be unrunnable at claim time costs a dispatch, a worker's attention, and
// an execution lease before anyone finds out.
//
// It deliberately does not require the prob kind to be registered in *this*
// process. The API server does not execute probes; a kind it was not built with
// is still meaningful to a Worker that was, and refusing to schedule it here
// would make the server's link-time prober set a scheduling policy.
func (s ExecutionSnapshot) Validate() error {
	if s.ScenarioUID == "" {
		return fmt.Errorf("execution snapshot names no scenario")
	}
	if s.Prob.Kind == "" {
		return fmt.Errorf("execution snapshot for scenario %v has no prob kind", s.ScenarioName)
	}
	if s.Prob.Spec == nil {
		return fmt.Errorf("execution snapshot for scenario %v has no prob spec", s.ScenarioName)
	}

	return nil
}
