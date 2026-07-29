package urth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The API-facing half of worker liveness. These tests need the whole resource
// schema, so they run against a real Postgres for the reason described in
// service_outbox_test.go.

// presenceService builds a service that records liveness, and returns it with
// the store handles.
func presenceService(t *testing.T) (urth.Service, *gorm.DB, *dbstore.DBStore) {
	t.Helper()

	// Built in two passes because the presence store needs the gorm handle that
	// newTestService creates. The service is discarded and rebuilt rather than
	// mutated, which keeps ServiceOption the only way to configure one.
	_, db, store := newTestService(t, &stubScheduler{})

	srv := urth.NewService(store, &stubScheduler{},
		urth.WithSigningKeys(testKeys(t)),
		urth.WithWorkerPresence(urth.NewWorkerPresenceStore(db)),
		urth.WithWorkerHeartbeatInterval(30*time.Second),
	)

	return srv, db, store
}

// registerWorker enrols a worker against an existing runner and returns its
// session and identity.
func registerWorker(t *testing.T, srv urth.Service, runnerName, workerName manifest.ResourceName) (urth.APIToken, manifest.ResourceID) {
	t.Helper()

	ctx := context.Background()

	enrolment, found, err := srv.Runners().GetToken(ctx, runnerName)
	require.NoError(t, err)
	require.True(t, found)

	registration, err := srv.Runners().AuthWorker(ctx, enrolment, manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance},
		Metadata: manifest.ObjectMeta{Name: workerName},
		Spec:     &urth.WorkerInstanceSpec{},
	})
	require.NoError(t, err)

	worker, err := urth.NewWorkerInstance(registration.Worker)
	require.NoError(t, err)

	return registration.Session, worker.UID
}

// workerPresence reads back the presence a client would be shown.
func workerPresence(t *testing.T, srv urth.Service, name manifest.ResourceName) urth.WorkerPresenceReport {
	t.Helper()

	found, exists, err := srv.Workers().Get(context.Background(), name)
	require.NoError(t, err)
	require.True(t, exists)

	status, ok := found.Status.(*urth.WorkerInstanceStatus)
	require.True(t, ok, "expected a worker status, got %T", found.Status)

	return status.Presence
}

// A registered worker that has said nothing is `unknown`, and one heartbeat
// makes it online. The starting state matters as much as the transition: a
// worker registered by a build that did not report would otherwise be asserted
// dead on no evidence at all.
func TestHeartbeatMakesAWorkerOnline(t *testing.T) {
	srv, _, store := presenceService(t)
	seedScenarioWithProb(t, store, restProb(probeA))

	session, _ := registerWorker(t, srv, "test-runner", "test-worker")

	require.Equal(t, urth.WorkerConditionUnknown, workerPresence(t, srv, "test-worker").Condition,
		"a worker that has not reported has told us nothing")

	response, err := srv.Workers().Heartbeat(context.Background(), session, urth.WorkerHeartbeatRequest{})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, response.Interval, "the server owns the cadence")
	require.False(t, response.Paused)

	presence := workerPresence(t, srv, "test-worker")
	require.Equal(t, urth.WorkerPresenceOnline, presence.API)
	require.Equal(t, urth.WorkerPresenceUnknown, presence.NATS,
		"a heartbeat says nothing about the worker's queue")
	require.Equal(t, urth.WorkerConditionNATSUnreachable, presence.Condition)
}

// A shutting-down worker is believed at once, so stopping one shows up
// immediately rather than after the offline timeout.
func TestLeavingHeartbeatTakesAWorkerOfflineAtOnce(t *testing.T) {
	srv, _, store := presenceService(t)
	seedScenarioWithProb(t, store, restProb(probeA))

	session, _ := registerWorker(t, srv, "test-runner", "test-worker")
	ctx := context.Background()

	_, err := srv.Workers().Heartbeat(ctx, session, urth.WorkerHeartbeatRequest{})
	require.NoError(t, err)
	require.Equal(t, urth.WorkerPresenceOnline, workerPresence(t, srv, "test-worker").API)

	_, err = srv.Workers().Heartbeat(ctx, session, urth.WorkerHeartbeatRequest{Leaving: true})
	require.NoError(t, err)

	require.Equal(t, urth.WorkerConditionOffline, workerPresence(t, srv, "test-worker").Condition,
		"a worker that said goodbye is offline without waiting out the timeout")
}

// A worker whose registration was dropped has to find out, or it reports into
// nothing forever. Not-found is its cue to register again; the API turns it into
// a 404.
func TestHeartbeatReportsADroppedRegistration(t *testing.T) {
	srv, _, store := presenceService(t)
	seedScenarioWithProb(t, store, restProb(probeA))

	session, workerUID := registerWorker(t, srv, "test-runner", "test-worker")
	ctx := context.Background()

	var worker urth.WorkerInstance
	found, err := store.GetByUID(ctx, &worker, workerUID)
	require.NoError(t, err)
	require.True(t, found)

	deleted, err := srv.Workers().Delete(ctx, worker.GetVersionedID())
	require.NoError(t, err)
	require.True(t, deleted)

	_, err = srv.Workers().Heartbeat(ctx, session, urth.WorkerHeartbeatRequest{})
	require.ErrorIs(t, err, bark.ErrResourceNotFound)
}

// Presence is only worth anything if it cannot be asserted on someone else's
// behalf, so an unsigned, forged or expired session buys nothing.
func TestHeartbeatRefusesAnInvalidSession(t *testing.T) {
	srv, _, store := presenceService(t)
	seedScenarioWithProb(t, store, restProb(probeA))
	registerWorker(t, srv, "test-runner", "test-worker")

	ctx := context.Background()

	_, err := srv.Workers().Heartbeat(ctx, "not-a-token", urth.WorkerHeartbeatRequest{})
	require.ErrorIs(t, err, bark.ErrResourceUnauthorized)

	forged, _, err := urth.IssueWorkerSession(urth.SigningKeys{Session: []byte("wrong-key")},
		"runner-uid", "worker-uid", time.Hour)
	require.NoError(t, err)

	_, err = srv.Workers().Heartbeat(ctx, forged, urth.WorkerHeartbeatRequest{})
	require.ErrorIs(t, err, bark.ErrResourceUnauthorized)

	require.Equal(t, urth.WorkerConditionUnknown, workerPresence(t, srv, "test-worker").Condition,
		"a refused heartbeat must leave no trace of life")
}

// A paused worker is told so, rather than discovering it through claims that are
// refused for no visible reason.
func TestHeartbeatReportsThatAWorkerIsPaused(t *testing.T) {
	srv, _, store := presenceService(t)
	seedScenarioWithProb(t, store, restProb(probeA))

	session, _ := registerWorker(t, srv, "test-runner", "test-worker")
	ctx := context.Background()

	_, exists, err := srv.Workers().SetPaused(ctx, "test-worker", true)
	require.NoError(t, err)
	require.True(t, exists)

	response, err := srv.Workers().Heartbeat(ctx, session, urth.WorkerHeartbeatRequest{})
	require.NoError(t, err)
	require.True(t, response.Paused)
}

// Claiming a run is itself proof of life, so a worker with a steady stream of
// work is confirmed without spending a single extra request -- and without
// waiting out an interval it has no reason to wait out.
func TestClaimingARunCountsAsContact(t *testing.T) {
	srv, _, store := presenceService(t)
	scenario := seedScenarioWithProb(t, store, restProb(probeA))

	ctx := context.Background()

	created, err := srv.Results(scenario.Name).Create(ctx, newRunRequest())
	require.NoError(t, err)

	session, _ := registerWorker(t, srv, "test-runner", "test-worker")

	require.Equal(t, urth.WorkerConditionUnknown, workerPresence(t, srv, "test-worker").Condition,
		"no heartbeat has been sent")

	var result urth.Result
	found, err := store.GetByUID(ctx, &result, created.UID)
	require.NoError(t, err)
	require.True(t, found)

	_, err = srv.Results("").ClaimRun(ctx, result.UID, session, urth.ClaimJobRequest{
		DispatchID:    urth.DispatchEventUID(result.UID, result.Version),
		ResultVersion: result.Version,
	})
	require.NoError(t, err)

	found2, exists, err := srv.Workers().Get(ctx, "test-worker")
	require.NoError(t, err)
	require.True(t, exists)

	status := found2.Status.(*urth.WorkerInstanceStatus)
	require.Equal(t, urth.WorkerPresenceOnline, status.Presence.API)
	require.Equal(t, urth.WorkerContactClaim, status.LastSeenVia,
		"the evidence is recorded, so a client can say the worker was seen claiming work")

	// Registering again must not lose it: a worker rewrites its Spec on every
	// registration, and liveness lives in the server-owned Status precisely so
	// that it survives.
	registerWorker(t, srv, "test-runner", "test-worker")
	require.Equal(t, urth.WorkerPresenceOnline, workerPresence(t, srv, "test-worker").API)
}
