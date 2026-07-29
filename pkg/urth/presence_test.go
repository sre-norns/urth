package urth

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The presence rules exist to tell four situations apart that all used to look
// like a green dot. These cases are the specification of what each one means.
func TestWorkerPresenceAt(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	const offlineAfter = 3 * time.Minute

	at := func(ago time.Duration) *time.Time {
		moment := now.Add(-ago)
		return &moment
	}

	for name, test := range map[string]struct {
		status    WorkerInstanceStatus
		api       WorkerPresence
		nats      WorkerPresence
		condition WorkerCondition
	}{
		"both signals fresh": {
			status:    WorkerInstanceStatus{LastSeenTime: at(time.Second), NATSLastSeenTime: at(time.Second)},
			api:       WorkerPresenceOnline,
			nats:      WorkerPresenceOnline,
			condition: WorkerConditionOnline,
		},
		// A worker parked on its queue that has stopped reaching the API server.
		// It will be offered work and can claim none of it, which is a different
		// problem from being gone and wants a different fix.
		"heard on the queue, silent to the API": {
			status:    WorkerInstanceStatus{LastSeenTime: at(10 * time.Minute), NATSLastSeenTime: at(time.Second)},
			api:       WorkerPresenceOffline,
			nats:      WorkerPresenceOnline,
			condition: WorkerConditionAPIUnreachable,
		},
		// The mirror image: a healthy worker with nowhere to collect work from.
		"reaching the API, absent from the queue": {
			status:    WorkerInstanceStatus{LastSeenTime: at(time.Second), NATSLastSeenTime: at(10 * time.Minute)},
			api:       WorkerPresenceOnline,
			nats:      WorkerPresenceOffline,
			condition: WorkerConditionNATSUnreachable,
		},
		// A worker whose broker connection never came up at all reads the same as
		// one that lost it: never heard is no better evidence of life than heard
		// and gone quiet.
		"reaching the API, never heard on the queue": {
			status:    WorkerInstanceStatus{LastSeenTime: at(time.Second)},
			api:       WorkerPresenceOnline,
			nats:      WorkerPresenceUnknown,
			condition: WorkerConditionNATSUnreachable,
		},
		"both signals lapsed": {
			status:    WorkerInstanceStatus{LastSeenTime: at(time.Hour), NATSLastSeenTime: at(time.Hour)},
			api:       WorkerPresenceOffline,
			nats:      WorkerPresenceOffline,
			condition: WorkerConditionOffline,
		},
		// Nothing has ever been heard: a record written before liveness reporting
		// existed, or a worker that does not report. Asserting it dead would be a
		// worse lie than the green dot this replaced.
		"never heard from at all": {
			status:    WorkerInstanceStatus{},
			api:       WorkerPresenceUnknown,
			nats:      WorkerPresenceUnknown,
			condition: WorkerConditionUnknown,
		},
		// A clean shutdown is believed immediately rather than waiting out the
		// timeout, which is what makes stopping a worker show up at once.
		"said goodbye": {
			status: WorkerInstanceStatus{
				LastSeenTime:     at(time.Second),
				NATSLastSeenTime: at(time.Second),
				LeftAt:           at(time.Second),
			},
			api:       WorkerPresenceOffline,
			nats:      WorkerPresenceOffline,
			condition: WorkerConditionOffline,
		},
		// ...but a worker that left and came back is not held down by its own
		// farewell.
		"left, then came back": {
			status: WorkerInstanceStatus{
				LastSeenTime:     at(time.Second),
				NATSLastSeenTime: at(time.Second),
				LeftAt:           at(time.Minute),
			},
			api:       WorkerPresenceOnline,
			nats:      WorkerPresenceOnline,
			condition: WorkerConditionOnline,
		},
	} {
		t.Run(name, func(t *testing.T) {
			report := WorkerPresenceAt(test.status, now, offlineAfter)

			require.Equal(t, test.api, report.API, "API signal")
			require.Equal(t, test.nats, report.NATS, "NATS signal")
			require.Equal(t, test.condition, report.Condition, "combined condition")
		})
	}
}

// Eviction acts on silence, and silence is only meaningful from something that
// was once heard. A worker that has never reported has told us nothing, and
// dropping registrations on an absence of evidence is how a working fleet gets
// deleted -- the asynq prototype reports neither signal.
func TestIsSilentIgnoresWorkersThatNeverReported(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-time.Hour)

	ancient := now.Add(-24 * time.Hour)
	recent := now.Add(-time.Minute)

	require.False(t, WorkerInstanceStatus{}.IsSilent(cutoff),
		"a worker that never reported is unknown, not silent")

	require.True(t, WorkerInstanceStatus{LastSeenTime: &ancient}.IsSilent(cutoff),
		"heard once, long ago, and never since")

	require.False(t, WorkerInstanceStatus{LastSeenTime: &ancient, NATSLastSeenTime: &recent}.IsSilent(cutoff),
		"a worker still announcing itself over NATS is present, whatever its API route is doing")

	require.False(t, WorkerInstanceStatus{NATSLastSeenTime: &recent}.IsSilent(cutoff),
		"one live signal is enough to be present")
}
