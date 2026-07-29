package urth

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"sort"
	"time"

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

	// load counts the work already committed to each runner. Nil is tolerated:
	// placement then degrades to the lowest-UID choice rather than refusing to
	// place anything, because a missing count is not a reason to stop scheduling.
	load RunnerLoadStore

	// offlineAfter grades worker presence with the same numbers the workers API
	// reports, so a worker the UI calls online is a worker placement counts.
	offlineAfter time.Duration

	// pick draws from a weighted distribution and exists to be replaced in
	// tests. Randomness is confined to the saturated regime; see selectRunner.
	pick func(n int) int

	// decisions counts placements by regime, when a metrics registry is present.
	decisions PlacementCounter
}

// PlacementCounter records how placements are being decided.
//
// An interface rather than a prometheus type so pkg/urth keeps no opinion about
// the metrics stack, and so the common case in tests -- no metrics at all -- is
// a nil check rather than a registry.
type PlacementCounter interface {
	// CountPlacement notes one decision made under the given regime.
	CountPlacement(regime PlacementRegime)
}

// countDecision records the regime a decision was reached under.
func (p placement) countDecision(regime PlacementRegime) {
	if p.decisions != nil {
		p.decisions.CountPlacement(regime)
	}
}

// draw returns an index in [0, n), using the injected source when there is one.
func (p placement) draw(n int) int {
	if n <= 0 {
		return 0
	}

	if p.pick != nil {
		return p.pick(n)
	}

	return rand.IntN(n)
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

	// Capacity is what the chosen runner looked like when it was chosen, and
	// Regime is how it came to be chosen. Both are carried so the log line and
	// the metric describe the decision from the decision itself, rather than
	// each re-deriving an explanation that could disagree with it.
	Capacity RunnerCapacity
	Regime   PlacementRegime
}

// PlacementRegime names how a runner was chosen, for logs and metrics.
//
// Slugs, matching the unschedulable reasons, so the same word reads identically
// in a log line, a metric label and an API response.
type PlacementRegime string

const (
	// PlacementBySpare is the ordinary case: at least one runner had room, and
	// the least loaded of those was chosen deterministically.
	PlacementBySpare PlacementRegime = "spare-capacity"

	// PlacementSaturated is every eligible runner already committed to at least
	// as much work as it has workers for. The choice is a weighted draw; see
	// selectRunner.
	PlacementSaturated PlacementRegime = "saturated"

	// PlacementSoleCandidate is one eligible runner and therefore no decision to
	// make. Capacity is not measured at all in this case.
	PlacementSoleCandidate PlacementRegime = "sole-candidate"

	// PlacementUnmeasured is the fallback when capacity could not be read. The
	// choice degrades to the lowest UID -- the behaviour that predates this --
	// rather than failing a run because a count was unavailable.
	PlacementUnmeasured PlacementRegime = "unmeasured"
)

// RunnerCapacity is what one runner can currently take on.
//
// Advisory throughout: it is read when a run is placed and may be stale by the
// time a worker claims. Nothing here authorises anything -- the claim checks in
// ADR 0002 remain the authority on whether a worker may take a run, and this
// only decides which queue the run is put in.
type RunnerCapacity struct {
	// OnlineWorkers is workers that are reachable on *both* liveness signals and
	// not paused.
	//
	// Strictly WorkerConditionOnline, which is where the two-signal design earns
	// its keep: a worker that cannot reach the API server cannot claim what it
	// is offered, and one absent from its queue never receives it. Neither is
	// capacity, and a single combined "up" flag could not tell either from a
	// worker that is genuinely able to work.
	OnlineWorkers int

	// RegisteredWorkers is every worker of this runner, whatever its presence.
	// Used only as the fallback weight when no runner has anything online.
	RegisteredWorkers int

	// ReadyWorkers is registered and not paused, saying nothing about whether the
	// worker can still be reached.
	//
	// It exists because that is what PlacementPreview.ReadyWorkers has always
	// meant, and narrowing an existing field to the stricter definition would
	// change what a client is told without changing what it asked for. Placement
	// itself never uses this -- OnlineWorkers is the number that decides anything.
	ReadyWorkers int

	// Queued is runs placed on this runner that no worker has claimed yet.
	Queued int

	// Running is runs a worker of this runner is executing now.
	Running int
}

// Committed is work this runner has been given and has not finished.
//
// Queued counts as committed even though no worker is holding it yet, and that
// is the point: it is what makes placement self-spreading. Each placement
// immediately reduces the chosen runner's own spare, so a burst of runs created
// between two claims walks across the fleet instead of piling onto whichever
// runner happened to look idle when the burst began.
func (c RunnerCapacity) Committed() int {
	return c.Queued + c.Running
}

// Spare is how much more this runner could take before it is oversubscribed.
// Negative when it is already behind, which is meaningful: it says by how much.
func (c RunnerCapacity) Spare() int {
	return c.OnlineWorkers - c.Committed()
}

// Pressure is committed work per online worker.
//
// Infinite when nothing is online, so a runner with no reachable workers sorts
// behind every runner that has any -- and behind another empty runner only by
// UID, since there is nothing to choose between them.
func (c RunnerCapacity) Pressure() float64 {
	if c.OnlineWorkers <= 0 {
		return math.Inf(1)
	}

	return float64(c.Committed()) / float64(c.OnlineWorkers)
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
	//
	// Says nothing about whether they can still be reached: a worker whose
	// process died is registered and unpaused until its registration is dropped.
	// OnlineWorkers is the number to read for that.
	ReadyWorkers int `json:"readyWorkers" yaml:"readyWorkers"`

	// OnlineWorkers is how many are reachable on both liveness signals, and so
	// could actually pick a run up now.
	OnlineWorkers int `json:"onlineWorkers" yaml:"onlineWorkers"`

	// QueuedRuns and RunningRuns are the work already committed to the eligible
	// runners: placed and waiting, and executing. They are what a new run would
	// be queued behind.
	QueuedRuns  int `json:"queuedRuns" yaml:"queuedRuns"`
	RunningRuns int `json:"runningRuns" yaml:"runningRuns"`

	// SpareCapacity is how many runs the fleet could start immediately, summed
	// over the runners that have room. Zero does not mean a run would be
	// refused -- it would be queued, which is the whole point of a durable
	// channel -- only that it would wait.
	SpareCapacity int `json:"spareCapacity" yaml:"spareCapacity"`

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

	// UID order is the base ordering everything else is layered on: it makes the
	// deterministic tie-break stable, and it makes the weighted draw reproducible
	// for a given source.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].UID < eligible[j].UID })

	selected, capacity, regime := p.choose(ctx, eligible)
	p.countDecision(regime)

	log.Printf("placed run of %q on runner %q by %s (%d of %d eligible; online=%d queued=%d running=%d spare=%d pressure=%.2f)",
		scenarioName, selected.Name, regime, len(eligible), len(matching),
		capacity.OnlineWorkers, capacity.Queued, capacity.Running, capacity.Spare(), capacity.Pressure())

	return placementDecision{
		Runner:   selected,
		Placed:   true,
		Capacity: capacity,
		Regime:   regime,
	}, nil
}

// choose selects among runners that are all equally entitled to the run.
//
// Everything reaching here has already passed the scenario's requirements and is
// active, so this is not about correctness -- any of them may run the job. It is
// about not sending every run to the same one, which is what sorting by UID and
// taking the first did: a runner won because of its identifier, and kept winning
// however far behind it fell.
func (p placement) choose(ctx context.Context, eligible []Runner) (Runner, RunnerCapacity, PlacementRegime) {
	// One candidate is not a decision, and measuring capacity to confirm that
	// would put two queries on the run-creation path of every deployment with a
	// single runner -- which is most of them.
	if len(eligible) == 1 {
		return eligible[0], RunnerCapacity{}, PlacementSoleCandidate
	}

	capacity, err := p.capacityOf(ctx, eligible)
	if err != nil {
		// Degraded, never fatal. A count that could not be read is not a reason
		// to refuse a run: fall back to the behaviour that predates this, which
		// is at least deterministic, and say so in the regime so the log does not
		// claim a capacity decision that was never made.
		log.Printf("placing without capacity information: %v", err)
		return eligible[0], RunnerCapacity{}, PlacementUnmeasured
	}

	return p.selectRunner(eligible, capacity)
}

// capacityOf assembles the capacity picture for a set of candidate runners.
func (p placement) capacityOf(ctx context.Context, eligible []Runner) (map[manifest.ResourceID]RunnerCapacity, error) {
	capacity, err := p.workerCapacity(ctx, eligible)
	if err != nil {
		return nil, err
	}

	if p.load == nil {
		// No load store configured. Worker counts alone still order the fleet
		// sensibly -- an idle runner and a busy one differ by their online count
		// long before they differ by queue depth -- so this is a reduced signal
		// rather than no signal.
		return capacity, nil
	}

	load, err := p.load.InFlightRuns(ctx)
	if err != nil {
		return nil, err
	}

	for uid, entry := range capacity {
		entry.Queued = load[uid].Queued
		entry.Running = load[uid].Running
		capacity[uid] = entry
	}

	return capacity, nil
}

// selectRunner ranks candidates and returns the winner.
//
// Pure with respect to the database -- everything it needs is in its arguments --
// so the whole ranking is table-testable without a store. `eligible` must
// already be in UID order.
func (p placement) selectRunner(eligible []Runner, capacity map[manifest.ResourceID]RunnerCapacity) (Runner, RunnerCapacity, PlacementRegime) {
	// Regime 1: somebody has room. Deterministic, so that a placement can be
	// reproduced and explained.
	//
	// Ranked by spare first and pressure second, because the two answer different
	// halves of the question: spare is how many more runs this runner could take
	// at once, while pressure breaks the tie between a large runner that is nearly
	// full and a small one that is empty -- the empty one should win, having
	// nothing to do.
	var spare []Runner
	for _, runner := range eligible {
		if capacity[runner.UID].Spare() > 0 {
			spare = append(spare, runner)
		}
	}

	if len(spare) > 0 {
		sort.SliceStable(spare, func(i, j int) bool {
			left, right := capacity[spare[i].UID], capacity[spare[j].UID]

			if left.Spare() != right.Spare() {
				return left.Spare() > right.Spare()
			}
			if left.Pressure() != right.Pressure() {
				return left.Pressure() < right.Pressure()
			}

			return spare[i].UID < spare[j].UID
		})

		return spare[0], capacity[spare[0].UID], PlacementBySpare
	}

	// Regime 2: every runner is already committed to at least as much work as it
	// has workers for. There is no good choice, only a fair one.
	//
	// A weighted draw rather than a deterministic pick, and the reason is not a
	// shrug: when every channel is saturated each drains in proportion to the
	// number of workers on it, so handing out new work in that same proportion
	// keeps the expected *wait* equal across the fleet. Choosing deterministically
	// here would send an unbroken stream to one runner until its counts moved,
	// which is the concentration this change exists to end.
	//
	// Backlog depth is deliberately not part of the weight. Once saturated, a
	// deeper queue on a larger runner is not worse for a new run -- it is being
	// worked off faster.
	weights := make([]int, len(eligible))
	total := 0
	for i, runner := range eligible {
		weights[i] = capacity[runner.UID].OnlineWorkers
		total += weights[i]
	}

	// Nothing online anywhere: an entirely offline fleet, or one that has never
	// reported. Fall back to who has workers at all, then to an even chance.
	// Placing the run regardless is required, not merely tolerated -- the queue
	// is durable, and a run for a fleet that is down waits for it to come back.
	if total == 0 {
		for i, runner := range eligible {
			weights[i] = capacity[runner.UID].RegisteredWorkers
			total += weights[i]
		}
	}

	if total == 0 {
		for i := range weights {
			weights[i] = 1
		}
		total = len(weights)
	}

	target := p.draw(total)
	for i, weight := range weights {
		if target < weight {
			return eligible[i], capacity[eligible[i].UID], PlacementSaturated
		}
		target -= weight
	}

	// Unreachable while the weights sum to total, but a draw that fell off the
	// end must still place the run rather than return a zero Runner.
	return eligible[0], capacity[eligible[0].UID], PlacementSaturated
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

	capacity, err := p.capacityOf(ctx, eligible)
	if err != nil {
		return preview, err
	}

	for _, runner := range eligible {
		entry := capacity[runner.UID]

		preview.RegisteredWorkers += entry.RegisteredWorkers
		preview.OnlineWorkers += entry.OnlineWorkers
		preview.QueuedRuns += entry.Queued
		preview.RunningRuns += entry.Running

		// Only positive spare is summed. A runner that is behind does not cancel
		// out room elsewhere -- the question this answers is "is there anywhere
		// for a run to go right now", and one oversubscribed runner does not make
		// an idle one less idle.
		if spare := entry.Spare(); spare > 0 {
			preview.SpareCapacity += spare
		}

		preview.ReadyWorkers += entry.ReadyWorkers
	}

	return preview, nil
}

// workerCapacity counts the workers of each given runner, graded by presence.
//
// Counted through the runner-UID label rather than the foreign key, because that
// is the association the rest of the system exposes and queries; see
// workerLabels. Presence and pausing are both evaluated in memory: IsPaused
// lives in a worker's server-owned Status and is deliberately not a label, since
// a worker must not be able to advertise itself as un-paused by re-registering,
// and presence is computed rather than stored for the reasons on
// WorkerInstanceStatus.Presence.
//
// One query for the whole eligible set, keyed by runner on the way out.
func (p placement) workerCapacity(ctx context.Context, runners []Runner) (map[manifest.ResourceID]RunnerCapacity, error) {
	capacity := make(map[manifest.ResourceID]RunnerCapacity, len(runners))
	if len(runners) == 0 {
		return capacity, nil
	}

	uids := make([]string, 0, len(runners))
	for _, runner := range runners {
		uids = append(uids, string(runner.UID))
		// Seeded so a runner with no workers at all is present with zeroes,
		// rather than missing and having to be defaulted by every reader.
		capacity[runner.UID] = RunnerCapacity{}
	}

	requirement, err := manifest.NewRequirement(LabelRunnerUID, manifest.In, uids)
	if err != nil {
		return nil, fmt.Errorf("failed to build a worker query for %d runners: %w", len(runners), err)
	}

	var workers []WorkerInstance
	if _, err := p.store.Find(ctx, &workers, manifest.SearchQuery{Selector: manifest.NewSelector(requirement)}); err != nil {
		return nil, fmt.Errorf("failed to list workers of the eligible runners: %w", err)
	}

	// One clock reading for the whole set, so two workers last heard from at the
	// same moment cannot be graded differently by the time the loop reaches them.
	now := time.Now()

	for _, worker := range workers {
		runnerUID := manifest.ResourceID(worker.Labels[LabelRunnerUID])
		entry, eligible := capacity[runnerUID]
		if !eligible {
			// A worker of a runner outside this placement's candidate set. The
			// label query should not return one, but a stale or hand-edited label
			// could, and counting it against a runner that is not a candidate
			// would be worse than ignoring it.
			continue
		}

		entry.RegisteredWorkers++

		if worker.Status.IsPaused {
			capacity[runnerUID] = entry
			continue
		}

		entry.ReadyWorkers++

		// Strictly online: a paused worker will not be given work, and one that
		// has lost either path cannot do it. See RunnerCapacity.OnlineWorkers.
		if WorkerPresenceAt(worker.Status, now, p.offlineAfter).Condition == WorkerConditionOnline {
			entry.OnlineWorkers++
		}

		capacity[runnerUID] = entry
	}

	return capacity, nil
}
