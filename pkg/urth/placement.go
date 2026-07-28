package urth

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// placement answers which runner, if any, may take a run.
//
// It is a type of its own rather than a method on the results API because two
// callers need the same answer and must not be able to disagree about it: the
// creation path, which decides where a run goes, and the preview endpoint, which
// tells an operator where it would go before they ask for one. A second
// implementation of "which runners match" would drift from the first, and the
// symptom would be a UI that offers a run the server then refuses.
type placement struct {
	store dbstore.TransactionalStore
}

// placementDecision is the outcome of trying to place a run.
//
// Not-placed is a decision, not an error: nothing has gone wrong with the
// request, and the caller's job is to record a run that cannot happen rather
// than to fail. Reason carries why, as one of the label-grammar slugs recorded
// in LabelResultUnschedulable.
type placementDecision struct {
	Runner Runner
	Placed bool
	Reason string
}

// PlacementPreview is what placement would decide for a scenario right now.
//
// It exists so that "can this scenario run" is answerable before a run is
// created. Without it the only way to find out is to trigger a run and read its
// terminal state, which is a poor way to learn that a fleet was decommissioned.
//
// The runner counts are split because they prompt different actions: a selector
// that matches nothing needs a runner or an edit, while matching runners that
// are all disabled needs one of them enabled. The worker counts are advisory --
// a dispatch to a runner with no workers connected waits in that runner's queue
// rather than failing, which is the durable-channel behaviour ADR 0003 requires.
type PlacementPreview struct {
	// Requirements is the scenario's selector rendered as text, for display. It
	// is not label grammar and never becomes a label value.
	Requirements string `json:"requirements" yaml:"requirements"`

	// MatchingRunners is how many runners the selector matches, in any state.
	MatchingRunners int `json:"matchingRunners" yaml:"matchingRunners"`

	// EligibleRunners is how many of those are active, and so could be placed on.
	EligibleRunners int `json:"eligibleRunners" yaml:"eligibleRunners"`

	// RegisteredWorkers is how many workers have registered against an eligible
	// runner.
	RegisteredWorkers int `json:"registeredWorkers" yaml:"registeredWorkers"`

	// ReadyWorkers is how many of those are not paused, and so may claim work.
	ReadyWorkers int `json:"readyWorkers" yaml:"readyWorkers"`

	// Schedulable reports whether a run created now would be dispatched.
	Schedulable bool `json:"schedulable" yaml:"schedulable"`

	// Reason is the slug a run would be stranded with when not schedulable, so
	// that a client shows the same words the run will carry.
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`

	// Detail is human-readable context -- currently a selector parse error.
	// Nothing branches on it.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
}

// selectorFor parses a resource's placement requirements.
//
// One place, so that the scenario validation performed on the way in, the
// placement decision, and the preview all accept exactly the same selectors.
func selectorFor(requirements manifest.LabelSelector) (manifest.Selector, error) {
	selector, err := manifest.ParseSelector(requirements.AsLabels())
	if err != nil {
		return nil, fmt.Errorf("failed to parse scenario requirements: %w", err)
	}

	return selector, nil
}

// candidates lists the runners a selector matches, and the eligible subset.
//
// Only an active runner is eligible: dispatching to a disabled one would queue
// work that, by the claim rules, no worker of that runner may take.
func (p placement) candidates(ctx context.Context, requirements manifest.LabelSelector) (matching, eligible []Runner, err error) {
	selector, err := selectorFor(requirements)
	if err != nil {
		return nil, nil, err
	}

	if _, err := p.store.Find(ctx, &matching, manifest.SearchQuery{Selector: selector}); err != nil {
		return nil, nil, fmt.Errorf("failed to list runners to schedule a scenario: %w", err)
	}

	for _, runner := range matching {
		if runner.Spec.IsActive {
			eligible = append(eligible, runner)
		}
	}

	return matching, eligible, nil
}

// Place selects the runner a run should be dispatched to.
//
// Placement is where a scenario's requirements finally mean something. The
// prototype parsed the selector, listed the runners it matched, logged how many
// there were, and then threw the list away -- every job went to one shared queue
// and any worker could take it, so a scenario that declared it needed a runner
// inside a particular network had no way of getting one.
func (p placement) Place(ctx context.Context, requirements manifest.LabelSelector, scenarioName manifest.ResourceName) (placementDecision, error) {
	matching, eligible, err := p.candidates(ctx, requirements)
	if err != nil {
		// A selector that does not parse is a property of the scenario, not a
		// fault in this request: the run is recorded as unschedulable in the same
		// way as one nothing matches, and the parse error is logged once here
		// rather than returned to whoever happened to trigger the run.
		log.Printf("cannot place a run of %q: %v", scenarioName, err)
		return placementDecision{Reason: ReasonInvalidRequirements}, nil
	}

	if len(eligible) == 0 {
		log.Printf("no active runner matches requirements %q for scenario %q (%d considered)",
			requirements.AsLabels(), scenarioName, len(matching))

		return placementDecision{Reason: ReasonNoEligibleRunner}, nil
	}

	// Deterministic selection by UID. Least-loaded or round-robin placement wants
	// queue depth per runner, which belongs to the scheduler service that does
	// not exist yet; picking stably means a scenario's runs land on one runner
	// instead of scattering, which is easier to reason about in the meantime.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].UID < eligible[j].UID })
	selected := eligible[0]

	log.Printf("placed run of %q on runner %q (%d of %d eligible)",
		scenarioName, selected.Name, len(eligible), len(matching))

	return placementDecision{Runner: selected, Placed: true}, nil
}

// Preview reports what Place would decide, without creating anything.
func (p placement) Preview(ctx context.Context, requirements manifest.LabelSelector) (PlacementPreview, error) {
	preview := PlacementPreview{Requirements: requirements.AsLabels()}

	matching, eligible, err := p.candidates(ctx, requirements)
	if err != nil {
		// Reported as an unschedulable scenario rather than as a failed request:
		// this endpoint exists to explain why a run cannot be made, and "the
		// selector is malformed" is one of the answers it is asked for.
		preview.Reason = ReasonInvalidRequirements
		preview.Detail = err.Error()

		return preview, nil
	}

	preview.MatchingRunners = len(matching)
	preview.EligibleRunners = len(eligible)
	preview.Schedulable = len(eligible) > 0
	if !preview.Schedulable {
		preview.Reason = ReasonNoEligibleRunner
	}

	registered, ready, err := p.workerCounts(ctx, eligible)
	if err != nil {
		return preview, err
	}

	preview.RegisteredWorkers = registered
	preview.ReadyWorkers = ready

	return preview, nil
}

// workerCounts counts the workers registered against the given runners.
//
// Counted through the runner-UID label rather than the foreign key, because that
// is the association the rest of the system exposes and queries; see
// workerLabels. Paused workers are counted separately in memory: IsPaused lives
// in a worker's server-owned Status and is not a label, deliberately, since a
// worker must not be able to advertise itself as un-paused by re-registering.
func (p placement) workerCounts(ctx context.Context, runners []Runner) (registered, ready int, err error) {
	if len(runners) == 0 {
		return 0, 0, nil
	}

	uids := make([]string, 0, len(runners))
	for _, runner := range runners {
		uids = append(uids, string(runner.UID))
	}

	requirement, err := manifest.NewRequirement(LabelRunnerUID, manifest.In, uids)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to build a worker query for %d runners: %w", len(runners), err)
	}

	var workers []WorkerInstance
	if _, err := p.store.Find(ctx, &workers, manifest.SearchQuery{Selector: manifest.NewSelector(requirement)}); err != nil {
		return 0, 0, fmt.Errorf("failed to list workers of the eligible runners: %w", err)
	}

	for _, worker := range workers {
		if !worker.Status.IsPaused {
			ready++
		}
	}

	return len(workers), ready, nil
}
