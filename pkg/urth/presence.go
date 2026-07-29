package urth

import "time"

// Worker liveness, in two signals that are deliberately not combined.
//
// A worker reaches Urth over two independent paths: HTTPS to the API server, and
// NATS to its runner's queue. Either can fail on its own, and which one failed is
// the whole diagnosis -- a worker parked on the queue but silent to the API is a
// firewall on the API port, while one heartbeating with nothing on the queue is a
// broker that is down or blocked. Collapsing the two into a single boolean throws
// away exactly the information an operator needs, so both are recorded and both
// are reported.
//
// Nothing here is consulted when placing a run. A dispatch to a runner whose
// workers are all offline waits in that runner's durable queue, which is the
// behaviour ADR 0004 requires; presence is for the operator, not the scheduler.

const (
	// DefaultWorkerHeartbeatInterval is how often a worker is asked to report.
	//
	// A minute rather than a few seconds because it is not the only evidence: a
	// worker claiming jobs proves itself with every claim, and one shutting down
	// cleanly says so. The interval only has to catch a worker that is idle and
	// has stopped without a word.
	DefaultWorkerHeartbeatInterval = 1 * time.Minute

	// DefaultWorkerOfflineAfterFactor is how many reporting intervals may be
	// missed before a signal is called offline. Three, so that one lost message
	// and one slow round trip do not between them declare a healthy worker dead.
	DefaultWorkerOfflineAfterFactor = 3

	// DefaultWorkerRetention is how long a worker that has gone quiet on both
	// signals is kept before the reconciler drops its registration.
	//
	// Long enough to survive a maintenance window -- an operator looking at the
	// fleet the morning after an outage should still see who was there.
	DefaultWorkerRetention = 24 * time.Hour

	// MinWorkerHeartbeatInterval is the floor a worker clamps a server-offered
	// interval to, so that a misconfigured or hostile server cannot turn the
	// reporting loop into a busy loop.
	MinWorkerHeartbeatInterval = 5 * time.Second
)

// WorkerPresence is what one signal says about a worker.
type WorkerPresence string

const (
	// WorkerPresenceOnline means the signal was seen recently enough.
	WorkerPresenceOnline WorkerPresence = "online"

	// WorkerPresenceOffline means the signal was once seen and has since lapsed,
	// or the worker said it was leaving.
	WorkerPresenceOffline WorkerPresence = "offline"

	// WorkerPresenceUnknown means this signal has never been seen at all.
	//
	// It is a third state rather than a synonym for offline on purpose. A record
	// written before liveness reporting existed, or one belonging to a worker
	// that does not report -- the asynq prototype -- has told us nothing, and
	// asserting it dead on no evidence would be a worse lie than the green dot
	// this replaces. It is also what keeps eviction off those records.
	WorkerPresenceUnknown WorkerPresence = "unknown"
)

// WorkerCondition is the verdict from both signals together.
//
// The values are label-grammar-safe slugs, matching ReasonNoEligibleRunner and
// the other unschedulable reasons, so that they read the same in a log line, a
// UI and -- should presence ever become queryable -- a label value. They are not
// labels today; see the note on Presence in WorkerInstanceStatus.
type WorkerCondition string

const (
	// WorkerConditionOnline is both signals present: the worker is reachable and
	// is on its queue.
	WorkerConditionOnline WorkerCondition = "online"

	// WorkerConditionOffline is neither signal present. The worker is gone.
	WorkerConditionOffline WorkerCondition = "offline"

	// WorkerConditionUnknown is neither signal ever seen.
	WorkerConditionUnknown WorkerCondition = "unknown"

	// WorkerConditionAPIUnreachable is a worker on its queue that has stopped
	// reaching the API server. It can receive dispatches and cannot claim them,
	// so it will take no work -- typically a firewall on the API port, or a
	// session it can no longer renew.
	WorkerConditionAPIUnreachable WorkerCondition = "api-unreachable"

	// WorkerConditionNATSUnreachable is a worker still talking to the API server
	// that has fallen off its queue. It is healthy and idle for want of a
	// broker -- typically NATS down, or its port blocked.
	WorkerConditionNATSUnreachable WorkerCondition = "nats-unreachable"
)

// WorkerContact records which evidence last proved a worker was reachable.
//
// Kept because "last seen 20 seconds ago" answers a different question depending
// on how: a heartbeat says the process is up, while a claim says it is up and
// taking work.
type WorkerContact string

const (
	// WorkerContactHeartbeat is the worker's periodic report.
	WorkerContactHeartbeat WorkerContact = "heartbeat"

	// WorkerContactClaim is a worker claiming a run. A busy worker is confirmed
	// alive by its work rather than by waiting out a heartbeat interval.
	WorkerContactClaim WorkerContact = "claim"
)

// WorkerPresenceReport is what the server believes about one worker's liveness.
//
// Both signals are reported alongside the verdict rather than only the verdict,
// so that a client can explain the answer instead of merely showing it.
type WorkerPresenceReport struct {
	// API is whether the worker has recently reached the API server.
	API WorkerPresence `form:"api" json:"api" yaml:"api" xml:"api"`

	// NATS is whether the worker has recently been heard on its queue.
	NATS WorkerPresence `form:"nats" json:"nats" yaml:"nats" xml:"nats"`

	// Condition is the two signals combined.
	Condition WorkerCondition `form:"condition" json:"condition" yaml:"condition" xml:"condition"`
}

// WorkerPresenceAt is the single definition of what the recorded timestamps mean.
//
// One function, called by the workers API, the reconciler's eviction pass and
// anything else that needs the answer -- for the reason placement is a type of
// its own: a second implementation of "is this worker alive" would drift from the
// first, and the symptom would be a UI showing a worker the reconciler has
// already decided to evict.
func WorkerPresenceAt(status WorkerInstanceStatus, now time.Time, offlineAfter time.Duration) WorkerPresenceReport {
	if offlineAfter <= 0 {
		offlineAfter = DefaultWorkerHeartbeatInterval * DefaultWorkerOfflineAfterFactor
	}

	report := WorkerPresenceReport{
		API:  presenceOf(status.LastSeenTime, now, offlineAfter),
		NATS: presenceOf(status.NATSLastSeenTime, now, offlineAfter),
	}

	// A worker that said goodbye is offline on both signals regardless of how
	// recently it was heard, because being heard is exactly what it just told us
	// to stop expecting. Guarded on the timestamps so that a worker which left
	// and has since come back is not held down by its own farewell.
	if left := status.LeftAt; left != nil &&
		!left.Before(derefTime(status.LastSeenTime)) &&
		!left.Before(derefTime(status.NATSLastSeenTime)) {
		report.API = downgradeToOffline(report.API)
		report.NATS = downgradeToOffline(report.NATS)
	}

	report.Condition = conditionOf(report.API, report.NATS)

	return report
}

// presenceOf grades one signal's timestamp.
func presenceOf(seen *time.Time, now time.Time, offlineAfter time.Duration) WorkerPresence {
	if seen == nil || seen.IsZero() {
		return WorkerPresenceUnknown
	}

	if now.Sub(*seen) > offlineAfter {
		return WorkerPresenceOffline
	}

	return WorkerPresenceOnline
}

// downgradeToOffline marks a signal offline without inventing evidence for one
// that was never seen: a worker that left having never reported over NATS still
// has nothing to say about NATS.
func downgradeToOffline(presence WorkerPresence) WorkerPresence {
	if presence == WorkerPresenceUnknown {
		return WorkerPresenceUnknown
	}

	return WorkerPresenceOffline
}

// conditionOf combines the two signals.
//
// An unknown signal is treated as "no evidence of life on this path" when paired
// with a live one, which is why a worker heartbeating but never heard over NATS
// reads as nats-unreachable: that is precisely the case of a worker whose broker
// connection never came up, and it is the most useful thing to say about it.
func conditionOf(api, nats WorkerPresence) WorkerCondition {
	apiUp := api == WorkerPresenceOnline
	natsUp := nats == WorkerPresenceOnline

	switch {
	case apiUp && natsUp:
		return WorkerConditionOnline
	case apiUp:
		return WorkerConditionNATSUnreachable
	case natsUp:
		return WorkerConditionAPIUnreachable
	case api == WorkerPresenceUnknown && nats == WorkerPresenceUnknown:
		return WorkerConditionUnknown
	default:
		return WorkerConditionOffline
	}
}

// derefTime reads an optional timestamp, treating absence as the zero time so
// that "never seen" compares as older than anything.
func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}

	return *value
}

// IsSilent reports whether both signals have lapsed past the given cutoff and at
// least one of them was ever seen.
//
// The second half is what keeps the reconciler off records it has no evidence
// about: a worker with neither timestamp is unknown, not offline, and evicting
// it would delete the asynq prototype's registrations and every record written
// before liveness reporting existed.
func (s WorkerInstanceStatus) IsSilent(cutoff time.Time) bool {
	if s.LastSeenTime == nil && s.NATSLastSeenTime == nil {
		return false
	}

	return derefTime(s.LastSeenTime).Before(cutoff) && derefTime(s.NATSLastSeenTime).Before(cutoff)
}
