package worker

import (
	"sync"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// inFlightRuns remembers which runs this process is currently executing.
//
// It exists for one case a confirmed acknowledgement cannot cover: the message
// is redelivered while the first execution is still in flight, either because
// the ack was lost after the server recorded nothing, or because the claim
// itself outlasted AckWait. The API's claim is idempotent for the same worker
// and dispatch -- which is correct, and is what lets a worker recover from a
// lost claim *response* -- so the redelivery would be authorised rather than
// refused, and the same external probe would run twice, concurrently, from one
// process.
//
// Nothing here is durable, deliberately. A restarted worker has forgotten
// everything it was running, and that is fine: the Result's execution lease is
// the durable truth, and the reconciler settles a run whose worker died. This
// is a duplicate reducer, not a claim.
//
// The set is bounded by construction. Every entry is held by one handle() call,
// which holds one slot of the consume semaphore for its whole life and releases
// both together -- so it can never hold more than --concurrency entries.
type inFlightRuns struct {
	mu   sync.Mutex
	runs map[manifest.ResourceID]struct{}
}

// acquire claims local ownership of a run, reporting whether it was granted.
//
// Called before the API claim rather than after it, which is the ordering that
// matters: the window this closes includes two deliveries whose claims are in
// flight at the same time, and acquiring only once a claim came back accepted
// would let both of them through.
func (f *inFlightRuns) acquire(uid manifest.ResourceID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if _, held := f.runs[uid]; held {
		return false
	}

	if f.runs == nil {
		f.runs = make(map[manifest.ResourceID]struct{})
	}
	f.runs[uid] = struct{}{}

	return true
}

// release gives up ownership once the run has been executed and reported, or
// abandoned without ever starting.
func (f *inFlightRuns) release(uid manifest.ResourceID) {
	f.mu.Lock()
	defer f.mu.Unlock()

	delete(f.runs, uid)
}
