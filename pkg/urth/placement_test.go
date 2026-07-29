package urth

import (
	"math"
	"testing"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

// Selection is pure with respect to the database, so the whole ranking is
// testable without one. These are the cases review-backlog task 014 lists as
// required, plus the presence cases the liveness work made expressible.

// runnersNamed builds candidates in UID order, as Place hands them over.
func runnersNamed(uids ...string) []Runner {
	runners := make([]Runner, 0, len(uids))
	for _, uid := range uids {
		runners = append(runners, Runner{
			ObjectMeta: manifest.ObjectMeta{UID: manifest.ResourceID(uid), Name: manifest.ResourceName(uid)},
			Spec:       RunnerSpec{IsActive: true},
		})
	}

	return runners
}

// fixedPick is a draw that always returns the same index, so a weighted
// selection can be asserted exactly rather than statistically.
func fixedPick(value int) func(int) int {
	return func(int) int { return value }
}

// The bug this change exists for: placement sorted by UID and took the first,
// so a runner won on its identifier and kept winning however far behind it fell.
func TestPlacementPrefersTheRunnerWithRoom(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		// Sorts first by UID and has nothing left.
		"aaa": {OnlineWorkers: 2, Running: 2},
		"bbb": {OnlineWorkers: 2},
	}

	selected, _, regime := placement{}.selectRunner(eligible, capacity)

	require.Equal(t, manifest.ResourceID("bbb"), selected.UID,
		"capacity must override UID order, or this change does nothing")
	require.Equal(t, PlacementBySpare, regime)
}

// Equal spare, unequal pressure: an empty small runner beats a nearly-full large
// one. Spare alone cannot tell them apart, which is why pressure is the second
// key rather than the only one.
func TestPlacementBreaksEqualSpareByPressure(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		"aaa": {OnlineWorkers: 10, Running: 8}, // spare 2, pressure 0.8
		"bbb": {OnlineWorkers: 2},              // spare 2, pressure 0.0
	}

	selected, _, _ := placement{}.selectRunner(eligible, capacity)

	require.Equal(t, manifest.ResourceID("bbb"), selected.UID)
}

// Identical in every respect: the choice must still be stable, or a fleet of
// equals would shuffle for no reason and placements would stop being explicable.
func TestPlacementTiesBreakByUID(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		"aaa": {OnlineWorkers: 4},
		"bbb": {OnlineWorkers: 4},
	}

	for range 5 {
		selected, _, _ := placement{}.selectRunner(eligible, capacity)
		require.Equal(t, manifest.ResourceID("aaa"), selected.UID)
	}
}

// Queued work counts against capacity as much as running work does. This is what
// makes placement self-spreading: a run placed a moment ago has already reduced
// that runner's spare, so a burst walks across the fleet.
func TestPlacementCountsQueuedWorkAsCommitted(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		"aaa": {OnlineWorkers: 3, Queued: 3}, // nothing running, but fully committed
		"bbb": {OnlineWorkers: 1},
	}

	selected, _, _ := placement{}.selectRunner(eligible, capacity)

	require.Equal(t, manifest.ResourceID("bbb"), selected.UID,
		"a runner with a full queue has no room, whether or not a worker holds the work yet")
}

// When nothing has room there is no good choice, only a fair one: weighted by
// worker count, because a saturated runner drains in proportion to its workers.
func TestPlacementDrawsProportionallyWhenSaturated(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		"aaa": {OnlineWorkers: 1, Running: 1}, // weight 1
		"bbb": {OnlineWorkers: 3, Running: 3}, // weight 3
	}

	// Weights lay out as [aaa][bbb bbb bbb] across a range of 4.
	for target, expected := range map[int]string{0: "aaa", 1: "bbb", 2: "bbb", 3: "bbb"} {
		selected, _, regime := placement{pick: fixedPick(target)}.selectRunner(eligible, capacity)

		require.Equal(t, manifest.ResourceID(expected), selected.UID, "draw %d", target)
		require.Equal(t, PlacementSaturated, regime)
	}
}

// The draw must span exactly the eligible set: every runner reachable, and no
// index falling off the end onto a zero Runner.
func TestPlacementSaturatedDrawCoversEveryRunner(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb", "ccc")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		"aaa": {OnlineWorkers: 1, Running: 1},
		"bbb": {OnlineWorkers: 1, Running: 1},
		"ccc": {OnlineWorkers: 1, Running: 1},
	}

	seen := map[manifest.ResourceID]bool{}
	for target := range 3 {
		selected, _, _ := placement{pick: fixedPick(target)}.selectRunner(eligible, capacity)
		require.NotEmpty(t, selected.UID)
		seen[selected.UID] = true
	}

	require.Len(t, seen, 3, "every eligible runner must be reachable by the draw")
}

// An entirely offline fleet still gets the run. The queue is durable: work waits
// for the fleet to come back rather than being refused, which is what ADR 0004
// requires and what the unschedulable rules in CLAUDE.md deliberately exclude.
func TestPlacementStillPlacesWhenNothingIsOnline(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		"aaa": {RegisteredWorkers: 1},
		"bbb": {RegisteredWorkers: 3},
	}

	// With nothing online the weights fall back to registered workers:
	// [aaa][bbb bbb bbb].
	selected, _, regime := placement{pick: fixedPick(2)}.selectRunner(eligible, capacity)

	require.Equal(t, manifest.ResourceID("bbb"), selected.UID)
	require.Equal(t, PlacementSaturated, regime)
}

// No workers registered anywhere at all -- a fleet that has been decommissioned
// down to its runner records. Still a placement, evenly weighted.
func TestPlacementPlacesWhenNoWorkersAreRegistered(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{"aaa": {}, "bbb": {}}

	for target, expected := range map[int]string{0: "aaa", 1: "bbb"} {
		selected, _, _ := placement{pick: fixedPick(target)}.selectRunner(eligible, capacity)
		require.Equal(t, manifest.ResourceID(expected), selected.UID)
	}
}

// A runner whose workers are all unreachable has no capacity, however many are
// registered. This is the payoff from recording liveness: "registered" and "able
// to take a run" stopped being the same number.
func TestPlacementIgnoresUnreachableWorkersAsCapacity(t *testing.T) {
	eligible := runnersNamed("aaa", "bbb")
	capacity := map[manifest.ResourceID]RunnerCapacity{
		// Ten workers registered, none of them reachable.
		"aaa": {RegisteredWorkers: 10},
		"bbb": {RegisteredWorkers: 1, OnlineWorkers: 1},
	}

	selected, _, regime := placement{}.selectRunner(eligible, capacity)

	require.Equal(t, manifest.ResourceID("bbb"), selected.UID,
		"one reachable worker is worth more than ten that cannot be reached")
	require.Equal(t, PlacementBySpare, regime)
}

// Pressure with nothing online is infinite rather than a division by zero, so an
// empty runner sorts behind every runner that has anyone.
func TestRunnerCapacityPressure(t *testing.T) {
	require.True(t, math.IsInf(RunnerCapacity{}.Pressure(), 1))
	require.True(t, math.IsInf(RunnerCapacity{Running: 3}.Pressure(), 1))

	require.InDelta(t, 0.0, RunnerCapacity{OnlineWorkers: 4}.Pressure(), 1e-9)
	require.InDelta(t, 1.25, RunnerCapacity{OnlineWorkers: 4, Queued: 2, Running: 3}.Pressure(), 1e-9)

	require.Equal(t, 5, RunnerCapacity{OnlineWorkers: 4, Queued: 2, Running: 3}.Committed())
	require.Equal(t, -1, RunnerCapacity{OnlineWorkers: 4, Queued: 2, Running: 3}.Spare())
}
