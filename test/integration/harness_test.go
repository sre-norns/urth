package integration

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/sre-norns/urth/pkg/apiserver"
	"github.com/sre-norns/urth/pkg/controllers"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/worker"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// postgresTestURLEnv names a disposable Postgres, matching pkg/urth's tests.
//
// Unset means skip, with the instruction attached: the rest of the suite is
// deliberately runnable with no containers, and CI supplies this as a service
// through `make audit/postgres`. A URL that is set but unreachable fails rather
// than skips, so a CI database that quietly went missing shows up red.
const postgresTestURLEnv = "URTH_TEST_POSTGRES_URL"

func TestMain(m *testing.M) {
	// Suppresses gin's route dump. The request log stays: `go test` shows a
	// package's output only when it fails, which is exactly when the statuses
	// the API answered with are worth reading.
	gin.SetMode(gin.TestMode)

	if err := registerTestProb(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register the test prob kind: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// harness is one scenario's worth of Urth: its own Postgres schema, its own
// embedded broker, a composed API server on an ephemeral listener, and whatever
// workers the test starts.
//
// Every part of it is real except the probe. The task is explicit that Postgres,
// HTTP and JetStream behaviour must not be replaced with mocks of the boundaries
// under test -- a fake of either half would agree with whatever the other half
// assumed, which is how the two claim contracts drifted apart in the first
// place.
type harness struct {
	t   *testing.T
	ctx context.Context

	// DB is scoped to this test's schema. Nothing it writes is visible to
	// another test, and its cleanup cannot reach the developer's dev data --
	// unlike the DropTable fixtures in pkg/urth, which is the trap filed in
	// TODO.md. Migrating those to this is out of scope here.
	DB *gorm.DB

	// Server is the composed control plane. Its loops are registered but never
	// started; see relayOnce and reconcileOnce.
	Server *apiserver.Server

	// HTTP serves Server.Router on an ephemeral port. Workers reach the API
	// through it exactly as they would a deployed one.
	HTTP *httptest.Server

	// Config is what Server was composed with, so a test can read the numbers it
	// has to reason about (AckWait, MaxJobAge) rather than restating them.
	Config apiserver.Config

	natsPort  int
	natsStore string
	nats      *natsserver.Server

	// releasePort gives up the placeholder listener holding natsPort. See
	// reservePort.
	releasePort func()

	// diagnostics is a client connection used only by dump.
	diagnostics *nats.Conn
}

// harnessOption adjusts a harness before it is composed.
type harnessOption func(*harnessSettings)

type harnessSettings struct {
	serverOptions []apiserver.Option
}

// withPublisherDecorator wraps the transport publisher the relay is handed.
//
// The only way to express "the broker accepted this message and then the relay
// died" without a second process: the decorator publishes for real and then
// fails, which is precisely the window the event UID exists to close.
func withPublisherDecorator(fn func(urth.DispatchPublisher) urth.DispatchPublisher) harnessOption {
	return func(s *harnessSettings) {
		s.serverOptions = append(s.serverOptions, apiserver.WithPublisherDecorator(fn))
	}
}

func newHarness(t *testing.T, options ...harnessOption) *harness {
	t.Helper()

	var settings harnessSettings
	for _, option := range options {
		option(&settings)
	}

	port, releasePort := reservePort(t)

	h := &harness{
		t:           t,
		ctx:         t.Context(),
		DB:          openTestSchema(t),
		natsPort:    port,
		releasePort: releasePort,
		natsStore:   t.TempDir(),
	}

	h.startNATS()
	// Registered here rather than in startNATS, and once, because cleanups run
	// last-in-first-out: a shutdown registered by a *restart* would run before
	// the workers a test started earlier, taking the broker away from processes
	// that are still draining. Registered at composition time, it runs after
	// them, which is the order a real shutdown has.
	t.Cleanup(h.shutdownNATS)

	require.NoError(t, h.DB.AutoMigrate(apiserver.Models()...),
		"schema migration failed; the harness owns a private schema, so this is not a collision with anyone else")

	h.Config = apiserver.Config{
		Transport: apiserver.TransportNATS,
		NATS:      h.natsConfig(),

		SessionTTL: time.Hour,
		// Short enough that a test can outlive a run's lease by backdating the
		// deadline rather than by waiting, and long enough that no run here
		// expires while it is legitimately executing.
		MaxRunDuration: time.Minute,

		WorkerHeartbeatInterval: time.Minute,
		WorkerRetention:         24 * time.Hour,

		// The loops are composed -- Dispatch.Relay and Dispatch.Reconciler are
		// non-nil -- but Start is never called, so nothing here races a ticker.
		// Every scenario drives RunOnce at the moment it means to. That is what
		// makes assertions like "the second relay pass republishes the same
		// event UID" statements about the relay rather than about timing.
		Controllers: controllers.Config{
			RelayEnabled:      true,
			RelayPollInterval: 250 * time.Millisecond,
			RelayBatchSize:    32,
			RelayLease:        30 * time.Second,

			ReconcileEnabled:   true,
			ReconcileInterval:  time.Minute,
			ReconcileLease:     5 * time.Minute,
			ReconcileBatchSize: 256,

			PendingDispatchGrace: 30 * time.Minute,

			// The advisory watcher would be the only loop with a live NATS
			// subscription, and nothing here asserts on it.
			AdvisoriesEnabled: false,

			ShutdownTimeout: 15 * time.Second,
		},
	}

	server, err := apiserver.New(h.ctx, h.DB, h.Config, settings.serverOptions...)
	require.NoError(t, err, "failed to compose the API server")
	t.Cleanup(func() { _ = server.Close() })
	h.Server = server

	h.HTTP = httptest.NewServer(server.Router)
	t.Cleanup(h.HTTP.Close)

	return h
}

// natsConfig is a valid transport configuration with the limits pulled down to
// sizes a test can reach.
//
// It must satisfy natsq.Config.Validate: a suite running against a combination
// the api-server would refuse to start with proves nothing about the shipped
// system.
func (h *harness) natsConfig() natsq.Config {
	cfg := natsq.Config{
		ClientConfig: natsq.ClientConfig{URL: h.natsURL()},

		Replicas:         1,
		MaxJobs:          256,
		MaxBytes:         1 << 20,
		MaxJobsPerRunner: 64,
		MaxMsgSize:       8 << 10,
		DuplicateWindow:  time.Minute,
		MaxJobAge:        time.Hour,

		// Short, because redelivery is the subject of several scenarios and the
		// shipped 30s default would make each of them a half-minute wait. It is
		// still comfortably longer than a claim against a local Postgres.
		AckWait:       2 * time.Second,
		MaxDeliver:    5,
		MaxAckPending: 8,

		MaxRunnerSeries: 100,
	}

	require.NoError(h.t, cfg.Validate(), "the harness transport configuration must be one the api-server would accept")

	return cfg
}

func (h *harness) natsURL() string {
	return fmt.Sprintf("nats://127.0.0.1:%d", h.natsPort)
}

// startNATS runs an in-process NATS server with JetStream, on the port this
// harness reserved and over the store directory it keeps.
//
// Both are fixed for the harness rather than chosen by the server, so that
// stopNATS/startNATS is a broker restart and not a different broker: the jobs
// stream is file-backed, so a dispatch published before an outage is still there
// afterwards.
func (h *harness) startNATS() {
	h.t.Helper()

	opts := &natsserver.Options{
		Host:      "127.0.0.1",
		Port:      h.natsPort,
		JetStream: true,
		StoreDir:  h.natsStore,
		NoLog:     true,
		NoSigs:    true,
	}

	srv, err := natsserver.NewServer(opts)
	require.NoError(h.t, err, "failed to start the embedded NATS server")

	// The placeholder listener is given up here and nowhere earlier: it is what
	// stops another scenario's reservation being handed this port in between.
	h.releasePort()

	go srv.Start()
	require.True(h.t, srv.ReadyForConnections(20*time.Second), "the embedded NATS server did not become ready")

	h.nats = srv
}

// shutdownNATS stops whichever broker instance is currently running.
func (h *harness) shutdownNATS() {
	if h.nats != nil {
		h.nats.Shutdown()
		h.nats = nil
	}
}

// stopNATS takes the broker away, as an outage rather than a teardown.
func (h *harness) stopNATS() {
	h.t.Helper()

	if h.diagnostics != nil {
		h.diagnostics.Close()
		h.diagnostics = nil
	}

	require.NotNil(h.t, h.nats, "the broker is already stopped")
	h.nats.Shutdown()
	h.nats.WaitForShutdown()
	h.nats = nil
}

// client builds an API client that reaches this harness over HTTP.
//
// Over a socket, deliberately, rather than by handing the worker the service
// directly: half of what this suite is for is that a claim outcome becomes an
// HTTP status and is read back as one. A worker holding the service object would
// never exercise that mapping.
func (h *harness) client(token urth.APIToken) *urth.RestAPIClient {
	h.t.Helper()

	client, err := urth.NewRestAPIClient(h.HTTP.URL+"/api", urth.APIClientConfig{
		Token:      token,
		Timeout:    30 * time.Second,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	})
	require.NoError(h.t, err)

	return client
}

// applyRunner registers an active runner with the given advertised labels.
func (h *harness) applyRunner(name manifest.ResourceName, labels manifest.Labels) urth.Runner {
	h.t.Helper()

	runner := urth.Runner{
		ObjectMeta: manifest.ObjectMeta{Name: name, Labels: labels},
		Spec:       urth.RunnerSpec{IsActive: true},
	}
	require.NoError(h.t, h.Server.Store.Create(h.ctx, &runner))

	return runner
}

// applyScenario creates an active scenario running the test prob.
func (h *harness) applyScenario(name manifest.ResourceName, spec testProbSpec, requirements manifest.LabelSelector) urth.Scenario {
	h.t.Helper()

	scenario := urth.Scenario{
		ObjectMeta: manifest.ObjectMeta{Name: name},
		Spec: urth.ScenarioSpec{
			IsActive:     true,
			Requirements: requirements,
			Prob: prob.Manifest{
				Kind: testProbKind,
				Spec: &spec,
			},
		},
	}
	require.NoError(h.t, h.Server.Store.Create(h.ctx, &scenario))

	return scenario
}

// enrolmentToken issues the secret an operator would hand a worker.
func (h *harness) enrolmentToken(runnerName manifest.ResourceName) urth.APIToken {
	h.t.Helper()

	token, found, err := h.Server.Service.Runners().GetToken(h.ctx, runnerName)
	require.NoError(h.t, err)
	require.True(h.t, found, "no runner named %q", runnerName)

	return token
}

// createRun triggers a run of a scenario, as POST /scenarios/:id/results does.
func (h *harness) createRun(scenarioName manifest.ResourceName) urth.Result {
	h.t.Helper()

	result, err := h.Server.Service.Results(scenarioName).Create(h.ctx, manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{APIVersion: "v1", Kind: urth.KindResult},
		// Unnamed, as a triggered run is: the server generates the name. See
		// resultsAPIImpl.Create.
		Metadata: manifest.ObjectMeta{},
		Spec:     &urth.ResultSpec{},
	})
	require.NoError(h.t, err)

	return result
}

// relayOnce drives exactly one relay pass and reports how many it published.
func (h *harness) relayOnce() (int, error) {
	h.t.Helper()

	require.NotNil(h.t, h.Server.Dispatch.Relay, "this harness composed no relay")

	return h.Server.Dispatch.Relay.RunOnce(h.ctx)
}

// mustRelay drives one relay pass and requires it to publish `want` entries.
func (h *harness) mustRelay(want int) {
	h.t.Helper()

	published, err := h.relayOnce()
	require.NoError(h.t, err)
	require.Equal(h.t, want, published, "relay published %d entries, want %d", published, want)
}

// reconcileOnce drives exactly one reconciler scan.
func (h *harness) reconcileOnce() urth.ReconcileReport {
	h.t.Helper()

	require.NotNil(h.t, h.Server.Dispatch.Reconciler, "this harness composed no reconciler")

	report, err := h.Server.Dispatch.Reconciler.RunOnce(h.ctx)
	require.NoError(h.t, err)

	return report
}

// result reads a run back from the store.
func (h *harness) result(uid manifest.ResourceID) urth.Result {
	h.t.Helper()

	var stored urth.Result
	found, err := h.Server.Store.GetByUID(h.ctx, &stored, uid)
	require.NoError(h.t, err)
	require.True(h.t, found, "no run %v", uid)

	return stored
}

// outbox reads the dispatch entries written for a run, newest last.
func (h *harness) outbox(resultUID manifest.ResourceID) []urth.DispatchOutboxEntry {
	h.t.Helper()

	var entries []urth.DispatchOutboxEntry
	require.NoError(h.t, h.DB.Where("result_uid = ?", resultUID).Order("id ASC").Find(&entries).Error)

	return entries
}

// artifacts lists what a run left behind, found by the server-derived label
// rather than by name -- the two workers name artifacts differently and the
// label is the same either way.
func (h *harness) artifacts(resultUID manifest.ResourceID) []manifest.ResourceManifest {
	h.t.Helper()

	selector, err := manifest.ParseSelector(fmt.Sprintf("%s=%s", urth.LabelResultUID, resultUID))
	require.NoError(h.t, err)

	found, _, err := h.Server.Service.Artifacts().List(h.ctx, manifest.SearchQuery{Selector: selector})
	require.NoError(h.t, err)

	return found
}

// dispatchFailures lists the dead letters recorded so far.
func (h *harness) dispatchFailures() []urth.DispatchFailure {
	h.t.Helper()

	failures, _, err := h.Server.Service.DispatchFailures().List(h.ctx, manifest.SearchQuery{})
	require.NoError(h.t, err)

	return failures
}

// jetStream opens (once) a client connection for the assertions and injections
// that have to speak to the broker directly.
func (h *harness) jetStream() jetstream.JetStream {
	h.t.Helper()

	require.NotNil(h.t, h.nats, "the broker is stopped")

	if h.diagnostics == nil || !h.diagnostics.IsConnected() {
		conn, err := nats.Connect(h.natsURL())
		require.NoError(h.t, err)

		h.diagnostics = conn
		h.t.Cleanup(conn.Close)
	}

	js, err := jetstream.New(h.diagnostics)
	require.NoError(h.t, err)

	return js
}

// streamState reports what the jobs stream currently holds.
//
// LastSeq is the assertion that matters most here: it counts every message the
// stream ever accepted, and the stream is created fresh for each harness. A
// republication that JetStream suppressed as a duplicate does not advance it, so
// "LastSeq is 1" is a direct statement that the broker accepted one job.
func (h *harness) streamState() jetstream.StreamState {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	stream, err := h.jetStream().Stream(ctx, natsq.JobsStreamName)
	require.NoError(h.t, err)

	info, err := stream.Info(ctx)
	require.NoError(h.t, err)

	return info.State
}

// consumerInfo reports what a runner's queue looks like from the broker's side.
func (h *harness) consumerInfo(runnerUID manifest.ResourceID) *jetstream.ConsumerInfo {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	consumer, err := h.jetStream().Consumer(ctx, natsq.JobsStreamName, natsq.RunnerConsumerName(runnerUID))
	require.NoError(h.t, err)

	info, err := consumer.Info(ctx)
	require.NoError(h.t, err)

	return info
}

// redeliver offers a dispatch to a runner's queue a second time.
//
// The broker's own redelivery cannot be provoked on demand -- it happens when an
// AckWait elapses with a message unacknowledged, which is a race, not a lever --
// and the ADR row being exercised is about what a worker does with a second
// delivery rather than about what caused one. A fresh Nats-Msg-Id is what keeps
// JetStream's duplicate window from suppressing it, leaving the queue in exactly
// the state a genuine redelivery leaves it in: the same dispatch, offered again.
//
// onto names the runner's subject to publish on, which is not always the
// dispatch's own runner: a message that reached the wrong queue is its own
// failure mode.
func (h *harness) redeliver(entry urth.DispatchOutboxEntry, onto manifest.ResourceID) {
	h.t.Helper()

	data, err := natsq.MarshalEnvelope(natsq.DispatchEnvelope{
		SchemaVersion: natsq.DispatchEnvelopeVersion,
		ResultUID:     entry.ResultUID,
		ResultVersion: entry.ResultVersion,
		ScenarioName:  entry.ScenarioName,
		RunnerUID:     entry.RunnerUID,
		DispatchID:    entry.EventUID,
	})
	require.NoError(h.t, err)

	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	_, err = h.jetStream().Publish(ctx, natsq.JobSubject(onto), data,
		jetstream.WithMsgID(fmt.Sprintf("%s.redelivery.%s", entry.EventUID, randomSuffix())))
	require.NoError(h.t, err)
}

// eventually waits for a condition with a deadline, dumping state if it never
// holds.
//
// Every wait in this suite goes through here. A bare sleep is both slower than
// it needs to be and unreliable on a loaded CI runner, and a bare poll loop that
// times out tells you only that something did not happen.
func (h *harness) eventually(timeout time.Duration, what string, condition func() bool) {
	h.t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}

		select {
		case <-h.ctx.Done():
			h.t.Fatalf("waiting for %s: test context cancelled", what)
		case <-time.After(25 * time.Millisecond):
		}
	}

	h.dump()
	h.t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// awaitTerminal waits for a run to stop moving, and returns it.
func (h *harness) awaitTerminal(uid manifest.ResourceID, timeout time.Duration) urth.Result {
	h.t.Helper()

	h.eventually(timeout, fmt.Sprintf("run %v to reach a terminal state", uid), func() bool {
		switch h.result(uid).Status.Status {
		case urth.JobCompleted, urth.JobErrored, urth.JobExpired:
			return true
		default:
			return false
		}
	})

	return h.result(uid)
}

// backdate rewrites a timestamp column so a test can reach a rule expressed in
// hours without waiting hours.
//
// Simulating age rather than shortening the timeout, on purpose. The rules being
// tested -- "a pending run older than MaxJobAge plus the grace", "a lease that
// expired more than the upload grace ago" -- are the production numbers, and a
// harness that reconfigured them down to milliseconds would be asserting that a
// comparison works rather than that the shipped policy does.
func (h *harness) backdate(table, column string, key string, id any, by time.Duration) {
	h.t.Helper()

	// The offset is cast rather than bound as a duration: gorm sends a
	// time.Duration as a bigint, and Postgres refuses to subtract one from a
	// timestamptz. Microseconds because that is the resolution the column has.
	statement := fmt.Sprintf("UPDATE %s SET %s = %s - CAST(? AS interval) WHERE %s = ?", table, column, column, key)
	require.NoError(h.t, h.DB.Exec(statement, fmt.Sprintf("%d microseconds", by.Microseconds()), id).Error)
}

// dump prints everything worth seeing when a scenario fails: the runs, the
// outbox with attempts and last error, and what the broker still holds.
func (h *harness) dump() {
	h.t.Helper()

	h.t.Log("--- integration harness state ---")

	var results []urth.Result
	if err := h.DB.Order("created_at ASC").Find(&results).Error; err != nil {
		h.t.Logf("results: query failed: %v", err)
	}
	for _, result := range results {
		h.t.Logf("result %v (%s) v%d: status=%s result=%q runner=%v worker=%v dispatch=%q deadline=%v labels=%v",
			result.UID, result.Name, result.Version, result.Status.Status, result.Status.Result,
			result.Status.Executor.RunnerID, result.Status.Executor.WorkerID,
			result.Status.DispatchID, result.Status.Deadline.UTC(), result.Labels)
	}

	var entries []urth.DispatchOutboxEntry
	if err := h.DB.Order("id ASC").Find(&entries).Error; err != nil {
		h.t.Logf("outbox: query failed: %v", err)
	}
	for _, entry := range entries {
		h.t.Logf("outbox %d: event=%s result=%v v%d runner=%v attempts=%d published=%v seq=%d retired=%v lastErr=%q",
			entry.ID, entry.EventUID, entry.ResultUID, entry.ResultVersion, entry.RunnerUID,
			entry.Attempts, entry.PublishedAt, entry.PublishedSeq, entry.RetiredAt, entry.LastError)
	}

	var failures []urth.DispatchFailure
	if err := h.DB.Order("id ASC").Find(&failures).Error; err != nil {
		h.t.Logf("dispatch failures: query failed: %v", err)
	}
	for _, failure := range failures {
		h.t.Logf("dead letter %s: reason=%s result=%v detail=%q", failure.Name, failure.Spec.Reason, failure.Spec.ResultUID, failure.Spec.Detail)
	}

	h.dumpJetStream()
}

func (h *harness) dumpJetStream() {
	h.t.Helper()

	if h.nats == nil {
		h.t.Log("jetstream: the broker is stopped")
		return
	}

	if h.diagnostics == nil {
		conn, err := nats.Connect(h.natsURL())
		if err != nil {
			h.t.Logf("jetstream: cannot connect for diagnostics: %v", err)
			return
		}
		h.diagnostics = conn
		h.t.Cleanup(conn.Close)
	}

	js, err := jetstream.New(h.diagnostics)
	if err != nil {
		h.t.Logf("jetstream: %v", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := js.Stream(ctx, natsq.JobsStreamName)
	if err != nil {
		h.t.Logf("jetstream: stream %s: %v", natsq.JobsStreamName, err)
		return
	}

	info, err := stream.Info(ctx)
	if err != nil {
		h.t.Logf("jetstream: stream info: %v", err)
		return
	}
	h.t.Logf("jetstream stream %s: messages=%d bytes=%d first=%d last=%d consumers=%d",
		info.Config.Name, info.State.Msgs, info.State.Bytes,
		info.State.FirstSeq, info.State.LastSeq, info.State.Consumers)

	consumers := stream.ListConsumers(ctx)
	for consumer := range consumers.Info() {
		h.t.Logf("jetstream consumer %s: pending=%d ackPending=%d redelivered=%d delivered=%d waiting=%d",
			consumer.Name, consumer.NumPending, consumer.NumAckPending,
			consumer.NumRedelivered, consumer.Delivered.Consumer, consumer.NumWaiting)
	}
	if err := consumers.Err(); err != nil {
		h.t.Logf("jetstream: listing consumers: %v", err)
	}
}

// workerHandle is a running worker and the means to stop it.
type workerHandle struct {
	*worker.Worker

	cancel context.CancelFunc

	// done is closed when Run returns, and exit holds what it returned. Closed
	// rather than sent to, so that both the readiness check and a later stop can
	// observe it: a worker whose Run gave up early has to be visible where it
	// happened, not as an unexplained silence three assertions later.
	done chan struct{}
	exit atomic.Pointer[error]
}

// stopped reports whether Run has returned, and with what.
func (w *workerHandle) stopped() (error, bool) {
	select {
	case <-w.done:
		if err := w.exit.Load(); err != nil {
			return *err, true
		}

		return nil, true
	default:
		return nil, false
	}
}

// stop asks the worker to shut down and waits for its loop to return.
//
// A test that means "this worker died" calls this while a probe is running: the
// worker drains what it has claimed, which for a probe blocked on a cancelled
// context means the run is abandoned with its lease still held -- exactly the
// state the reconciler exists to settle.
func (w *workerHandle) stop(t *testing.T) {
	t.Helper()

	w.cancel()

	select {
	case <-w.done:
	case <-time.After(30 * time.Second):
		t.Fatal("worker did not stop within 30s")
	}
}

// workerOption adjusts a worker before it is started.
type workerOption func(*workerSetup)

type workerSetup struct {
	workerOptions []worker.Option
	wrapClient    func(urth.Service) urth.Service
	tune          func(*worker.Config)
}

// withProbeRunner replaces probe execution for this worker.
func withProbeRunner(fn worker.ProbeRunner) workerOption {
	return func(s *workerSetup) {
		s.workerOptions = append(s.workerOptions, worker.WithProbeRunner(fn))
	}
}

// withClientWrapper interposes on this worker's API client.
//
// The failures it exists for are the ones on the wire between the worker and the
// control plane -- a claim whose response never arrives -- which no failpoint
// inside either process can express, because both of them behaved correctly.
func withClientWrapper(fn func(urth.Service) urth.Service) workerOption {
	return func(s *workerSetup) { s.wrapClient = fn }
}

// withWorkerConfig adjusts a worker's configuration.
func withWorkerConfig(fn func(*worker.Config)) workerOption {
	return func(s *workerSetup) { s.tune = fn }
}

// startWorker registers a worker against a runner and starts its consume loop.
func (h *harness) startWorker(runnerName manifest.ResourceName, options ...workerOption) *workerHandle {
	h.t.Helper()

	var setup workerSetup
	for _, option := range options {
		option(&setup)
	}

	token := h.enrolmentToken(runnerName)

	cfg := worker.NewDefaultConfig()
	cfg.Name = manifest.ResourceName(fmt.Sprintf("test-worker-%s", randomSuffix()))
	cfg.Concurrency = 2
	cfg.APIRegistrationTimeout = 30 * time.Second
	cfg.HeartbeatInterval = time.Minute
	cfg.RunnerConfig.Timeout = 30 * time.Second
	// Log streaming is a separate publication over the same connection and
	// nothing here tails a run; leaving it on would only add traffic.
	cfg.StreamLogs = false
	cfg.NATS = natsq.ClientConfig{URL: h.natsURL()}
	// The prober writes into it only through the log artifact, but
	// runner.RunnerConfig declares it and puppeteer would use it.
	cfg.WorkingDirectory = h.t.TempDir()

	if setup.tune != nil {
		setup.tune(&cfg)
	}

	var client urth.Service = h.client(token)
	if setup.wrapClient != nil {
		client = setup.wrapClient(client)
	}

	instance := worker.New(&cfg, client, token, setup.workerOptions...)

	ctx, cancel := context.WithCancel(h.ctx)
	handle := &workerHandle{Worker: instance, cancel: cancel, done: make(chan struct{})}

	go func() {
		err := instance.Run(ctx)
		handle.exit.Store(&err)
		close(handle.done)
	}()

	h.t.Cleanup(func() {
		cancel()
		select {
		case <-handle.done:
		case <-time.After(30 * time.Second):
			buf := make([]byte, 1<<20)
			h.t.Errorf("worker did not stop during cleanup; goroutines:\n%s", buf[:runtime.Stack(buf, true)])
		}
	})

	// Registered is not the same as ready.
	//
	// Run registers, *then* connects to NATS and binds the runner's consumer,
	// and only then starts pulling. Returning as soon as the worker had an
	// identity meant a test could take the broker away while the worker was
	// still binding, killing its Run outright -- and because nothing looked at
	// what Run returned, the symptom was a queue with a message in it and no
	// consumer, sixty seconds later, in a test about something else entirely.
	// A waiting pull is the first moment the worker can actually be given work.
	h.eventually(60*time.Second, "the worker to start consuming", func() bool {
		if err, done := handle.stopped(); done {
			h.t.Fatalf("worker stopped before it began consuming: %v", err)
		}

		runnerUID := instance.RunnerUID()
		if runnerUID == "" {
			return false
		}

		return h.consumerInfo(runnerUID).NumWaiting > 0
	})

	return handle
}

// openTestSchema gives one test a private Postgres schema.
//
// A schema rather than a set of tables it drops: the existing fixtures in
// pkg/urth DropTable the whole model set against whatever `store-url` points at,
// which is the developer's dev database, and silently take every runner and
// scenario applied by hand with them. A schema is created for the test, dropped
// with it, and cannot reach anything outside itself -- which also makes the
// suite parallel-safe for free.
func openTestSchema(t *testing.T) *gorm.DB {
	t.Helper()

	baseURL := os.Getenv(postgresTestURLEnv)
	if baseURL == "" {
		t.Skipf("set %s to run the integration suite (see `make run-postgres-podman`)", postgresTestURLEnv)
	}

	base, err := gorm.Open(postgres.Open(baseURL), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err, "failed to reach the Postgres named by %s", postgresTestURLEnv)

	schema := "urthtest_" + randomSuffix()
	require.NoError(t, base.Exec("CREATE SCHEMA "+schema).Error)

	scoped, err := gorm.Open(postgres.Open(withSearchPath(t, baseURL, schema)), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	t.Cleanup(func() {
		// The scoped pool goes first: Postgres refuses to drop a schema whose
		// objects are still open in another session, and the failure would show
		// up as an accumulation of leftover schemas rather than as an error.
		if sqlDB, err := scoped.DB(); err == nil {
			_ = sqlDB.Close()
		}

		if err := base.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Errorf("failed to drop the test schema %s: %v", schema, err)
		}

		if sqlDB, err := base.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return scoped
}

// withSearchPath points a connection URL at one schema.
func withSearchPath(t *testing.T, baseURL, schema string) string {
	t.Helper()

	parsed, err := url.Parse(baseURL)
	require.NoError(t, err, "%s must be a URL", postgresTestURLEnv)

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()

	return parsed.String()
}

// reservePort takes an ephemeral port and holds it until the broker is ready to
// bind it, returning the port and the release.
//
// A port is reserved at all -- rather than letting NATS choose one -- because a
// restart on a different address is a different broker, not an outage, and the
// outage scenario needs the client to be reconnecting to the same place.
//
// The placeholder listener is held rather than closed immediately, because the
// operating system will hand a just-released ephemeral port straight to the next
// caller that asks for one. With a scenario per port and the whole suite running
// in parallel, that collision is not rare, and it presents as "the embedded NATS
// server did not become ready" in whichever scenario lost the race -- a failure
// that says nothing about what it was testing.
func reservePort(t *testing.T) (int, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var once sync.Once
	release := func() { once.Do(func() { _ = listener.Close() }) }
	t.Cleanup(release)

	return listener.Addr().(*net.TCPAddr).Port, release
}

func randomSuffix() string {
	var buf [8]byte
	for i := range buf {
		buf[i] = byte(rand.UintN(256))
	}

	return hex.EncodeToString(buf[:])
}
