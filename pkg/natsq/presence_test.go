package natsq_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// A presence subject carries the entire announcement, so what it can be parsed
// back into is the whole of the trust boundary.
func TestPresenceSubjectRoundTrips(t *testing.T) {
	runnerUID := manifest.ResourceID(uuid.NewString())
	workerUID := manifest.ResourceID(uuid.NewString())

	announcement, ok := natsq.ParsePresenceSubject(natsq.PresenceSubject(runnerUID, workerUID))

	require.True(t, ok)
	require.Equal(t, runnerUID, announcement.RunnerUID)
	require.Equal(t, workerUID, announcement.WorkerUID)
}

// The runner comes first so that a worker's publish permission can be scoped to
// its own runner's prefix. If that ordering ever changed, the grant would stop
// containing anything and any worker could announce for any fleet.
func TestRunnerPresencePrefixCoversItsWorkers(t *testing.T) {
	runnerUID := manifest.ResourceID(uuid.NewString())
	workerUID := manifest.ResourceID(uuid.NewString())

	prefix := natsq.RunnerPresenceSubjectPrefix(runnerUID)
	subject := natsq.PresenceSubject(runnerUID, workerUID)

	require.Equal(t, prefix[:len(prefix)-1], subject[:len(prefix)-1],
		"a worker's subject must sit under its runner's grant")
}

// Malformed subjects are rejected rather than yielding a partial or bogus ID.
//
// The UUID requirement is not fussiness: these values go into a query against
// `uuid` columns, and Postgres answers a malformed literal with an error rather
// than an empty result. Without the check, a worker publishing rubbish under its
// own prefix would turn every announcement into a logged database failure.
func TestParsePresenceSubjectRejectsMalformedSubjects(t *testing.T) {
	valid := uuid.NewString()

	for name, subject := range map[string]string{
		"empty":              "",
		"wrong prefix":       "other.v1.presence." + valid + "." + valid,
		"wrong version":      "urth.v2.presence." + valid + "." + valid,
		"a log subject":      "urth.v1.logs." + valid + "." + valid,
		"missing the worker": "urth.v1.presence." + valid,
		"an extra element":   "urth.v1.presence." + valid + "." + valid + ".extra",
		"empty runner":       "urth.v1.presence.." + valid,
		"empty worker":       "urth.v1.presence." + valid + ".",
		"runner not an ID":   "urth.v1.presence.not-a-uuid." + valid,
		"worker not an ID":   "urth.v1.presence." + valid + ".not-a-uuid",
	} {
		t.Run(name, func(t *testing.T) {
			_, ok := natsq.ParsePresenceSubject(subject)
			require.False(t, ok)
		})
	}
}
