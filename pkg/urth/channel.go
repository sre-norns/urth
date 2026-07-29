package urth

import (
	"context"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// RunnerChannelObserver reports what the transport can see about a runner's
// queue.
//
// Owned here and implemented by transport packages, for the reason
// WorkerTransportProvider and RunnerChannelReconciler are: the domain knows that
// a runner has somewhere to collect work and that it is worth knowing whether
// anyone is waiting at it, and nothing about streams or consumers.
//
// This is the fleet-level counterpart to per-worker presence, and it is kept
// even though the per-worker signal is more precise, because it needs no
// cooperation from any worker at all. When every worker of a runner has gone
// quiet but the queue still shows waiting pulls, the thing that broke is the
// presence reporting, not the fleet -- a distinction neither signal can draw on
// its own.
type RunnerChannelObserver interface {
	// ObserveRunnerChannel reads the current state of a runner's queue.
	ObserveRunnerChannel(ctx context.Context, runnerUID manifest.ResourceID) (RunnerChannelStatus, error)
}

// RunnerChannelStatus is a snapshot of a runner's queue.
//
// Advisory throughout. It is read at the moment a runner is displayed and may be
// stale by the time anyone acts on it, which is why nothing on the dispatch path
// consults it.
type RunnerChannelStatus struct {
	// Observed reports whether these numbers mean anything.
	//
	// False covers three cases that a caller must not tell apart by reading
	// zeroes: no transport is configured, the runner's queue has not been
	// provisioned yet, and the broker could not be reached. A client shows
	// nothing rather than an authoritative-looking zero.
	Observed bool `form:"observed" json:"observed" yaml:"observed" xml:"observed"`

	// Pullers is how many worker pull requests are parked on the queue. It is
	// the closest thing the transport has to "somebody is listening".
	Pullers int `form:"pullers" json:"pullers" yaml:"pullers" xml:"pullers"`

	// Pending is queued work no worker has taken yet.
	Pending uint64 `form:"pending" json:"pending" yaml:"pending" xml:"pending"`

	// AckPending is work handed to a worker that has not been acknowledged.
	AckPending int `form:"ackPending" json:"ackPending" yaml:"ackPending" xml:"ackPending"`
}
