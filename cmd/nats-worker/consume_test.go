package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// fakeMsg is a jetstream.Msg that records which acknowledgement it received, so a
// claim disposition can be checked without a live NATS server. Only the ack family
// is meaningful here; the accessors return zero values.
//
// Ack and DoubleAck are counted separately, and that separation is the point: they
// are the same acknowledgement but only one of them waits for the server to record
// it, so a test that treats them alike cannot tell the bug this guards against from
// its fix.
type fakeMsg struct {
	mu sync.Mutex

	acked       int
	doubleAcked int
	nakedDelay  time.Duration
	naked       bool
	termed      bool

	// data is the encoded dispatch envelope this message carries.
	data []byte

	// doubleAckErr, when set, is what every confirmation attempt returns.
	doubleAckErr error

	// record, when set, receives an entry for each acknowledgement, so a test can
	// assert ordering against the claim and the probe.
	record func(string)
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *fakeMsg) Data() []byte                              { return m.data }
func (m *fakeMsg) Headers() nats.Header                      { return nil }
func (m *fakeMsg) Subject() string                           { return "" }
func (m *fakeMsg) Reply() string                             { return "" }

func (m *fakeMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.acked++
	if m.record != nil {
		m.record("ack")
	}

	return nil
}

func (m *fakeMsg) DoubleAck(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.doubleAcked++
	if m.doubleAckErr != nil {
		return m.doubleAckErr
	}
	if m.record != nil {
		m.record("double-ack")
	}

	return nil
}

func (m *fakeMsg) Nak() error { m.naked = true; return nil }
func (m *fakeMsg) NakWithDelay(d time.Duration) error {
	m.naked = true
	m.nakedDelay = d
	return nil
}
func (m *fakeMsg) InProgress() error           { return nil }
func (m *fakeMsg) Term() error                 { m.termed = true; return nil }
func (m *fakeMsg) TermWithReason(string) error { m.termed = true; return nil }

// acks reports how many acknowledgements of each kind this message received.
func (m *fakeMsg) acks() (plain, confirmed int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.acked, m.doubleAcked
}

// confirmingAck is an ackConfirmer that succeeds, standing in for a broker that
// recorded the acknowledgement.
func confirmingAck(ctx context.Context, msg jetstream.Msg) error {
	return msg.DoubleAck(ctx)
}

// reportedOK is a dispatchReporter that records the failure and allows the
// message to be removed, standing in for a control plane that accepted a report.
func reportedOK(recorded *urth.DispatchFailureReason) dispatchReporter {
	return func(reason urth.DispatchFailureReason, _ string) bool {
		if recorded != nil {
			*recorded = reason
		}

		return true
	}
}

// reportFailed stands in for a control plane that could not be reached.
func reportFailed(reason *urth.DispatchFailureReason) dispatchReporter {
	return func(r urth.DispatchFailureReason, _ string) bool {
		if reason != nil {
			*reason = r
		}

		return false
	}
}

// TestApplyDisposition proves each claim outcome maps to exactly one queue action,
// and that a claim interrupted by shutdown leaves the message untouched for
// redelivery -- the outcome that did not exist in the prototype, where every
// failure collapsed into ack-or-nak.
// The accepted row is the one that matters here: a granted claim must be
// acknowledged with server confirmation, never with a fire-and-forget Ack, because
// an unconfirmed ack leaves a redelivery window the worker cannot see.
func TestApplyDisposition(t *testing.T) {
	cases := []struct {
		name          string
		outcome       claimOutcome
		wantExecute   bool
		wantAck       int
		wantDoubleAck int
		wantNak       bool
		wantTerm      bool
	}{
		{name: "accepted confirms the ack and executes", outcome: claimAccepted, wantExecute: true, wantDoubleAck: 1},
		{name: "retry naks for redelivery", outcome: claimRetry, wantNak: true},
		{name: "stale acks and drops", outcome: claimStale, wantAck: 1},
		{name: "terminal terminates", outcome: claimTerminal, wantTerm: true},
		{name: "abandon leaves the message untouched", outcome: claimAbandon},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &fakeMsg{}
			execute := applyDisposition(context.Background(), msg, tc.outcome,
				manifest.ResourceID("run-1"), reportedOK(nil), confirmingAck)

			plain, confirmed := msg.acks()

			if execute != tc.wantExecute {
				t.Errorf("execute = %v, want %v", execute, tc.wantExecute)
			}
			if plain != tc.wantAck {
				t.Errorf("Ack calls = %v, want %v", plain, tc.wantAck)
			}
			if confirmed != tc.wantDoubleAck {
				t.Errorf("DoubleAck calls = %v, want %v", confirmed, tc.wantDoubleAck)
			}
			if msg.naked != tc.wantNak {
				t.Errorf("naked = %v, want %v", msg.naked, tc.wantNak)
			}
			if msg.termed != tc.wantTerm {
				t.Errorf("termed = %v, want %v", msg.termed, tc.wantTerm)
			}
		})
	}
}

// TestUnconfirmedAckStillExecutes locks the rule that keeps a lost acknowledgement
// from becoming a lost run. The claim is already committed in Postgres -- the
// Result is `running`, leased, with this worker recorded as its executor -- so
// refusing to execute because the broker did not answer would strand a run the
// control plane believes is in progress, and there is no second claim to make.
func TestUnconfirmedAckStillExecutes(t *testing.T) {
	msg := &fakeMsg{}
	failing := func(context.Context, jetstream.Msg) error {
		return errors.New("no response from stream")
	}

	if !applyDisposition(context.Background(), msg, claimAccepted,
		manifest.ResourceID("run-1"), reportedOK(nil), failing) {
		t.Fatal("an unconfirmed acknowledgement must not prevent the claimed run from executing")
	}

	if msg.naked || msg.termed {
		t.Error("an unconfirmed acknowledgement must not nak or terminate a claim that already committed")
	}
}

// TestAckConfirmationIsDetachedFromShutdown proves the acknowledgement outlives a
// cancelled worker context. Shutdown drains in-flight runs, so a run whose claim
// just committed is still going to be executed and reported; inheriting the
// cancellation would guarantee that its acknowledgement never lands, and the run
// would be redelivered to another worker while this one was still running it.
func TestAckConfirmationIsDetachedFromShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msg := &fakeMsg{}
	var seen error
	ack := func(ackCtx context.Context, m jetstream.Msg) error {
		seen = ackCtx.Err()
		return m.DoubleAck(ackCtx)
	}

	applyDisposition(ctx, msg, claimAccepted, manifest.ResourceID("run-1"), reportedOK(nil), ack)

	if seen != nil {
		t.Fatalf("the acknowledgement inherited a cancelled context: %v", seen)
	}
	if _, confirmed := msg.acks(); confirmed != 1 {
		t.Fatalf("DoubleAck calls = %v, want 1", confirmed)
	}
}

// TestApplyDispositionRetryDelays confirms a retry is a delayed NAK, so a
// struggling API server is not immediately hammered with the same claim.
func TestApplyDispositionRetryDelays(t *testing.T) {
	msg := &fakeMsg{}
	applyDisposition(context.Background(), msg, claimRetry, manifest.ResourceID("run-1"), reportedOK(nil), confirmingAck)
	if msg.nakedDelay <= 0 {
		t.Fatalf("retry should NAK with a positive delay, got %v", msg.nakedDelay)
	}
}

// TestPermanentRefusalIsReportedBeforeTermination proves the ordering the
// dead-letter path depends on: the failure is recorded, and only then is the
// message removed. A terminated message is not redelivered, so a report made
// afterwards would be a report that a crash could lose entirely.
func TestPermanentRefusalIsReportedBeforeTermination(t *testing.T) {
	msg := &fakeMsg{}
	var reported urth.DispatchFailureReason

	applyDisposition(context.Background(), msg, claimTerminal, manifest.ResourceID("run-1"), reportedOK(&reported), confirmingAck)

	if reported != urth.ReasonPolicyRefused {
		t.Errorf("reported reason = %q, want %q", reported, urth.ReasonPolicyRefused)
	}
	if !msg.termed {
		t.Error("a reported permanent refusal should terminate the message")
	}
}

// TestUnreportedRefusalKeepsTheMessage is the regression that matters most here.
// If the control plane cannot be told why a dispatch is dead, terminating anyway
// destroys the only evidence it ever existed -- JetStream will not redeliver a
// terminated message and the Result stays pending with nothing to explain it.
func TestUnreportedRefusalKeepsTheMessage(t *testing.T) {
	msg := &fakeMsg{}

	applyDisposition(context.Background(), msg, claimTerminal, manifest.ResourceID("run-1"), reportFailed(nil), confirmingAck)

	if msg.termed {
		t.Error("a dispatch failure that could not be reported must not be terminated")
	}
	if !msg.naked {
		t.Error("an unreported failure should be left for redelivery")
	}
	if msg.nakedDelay <= 0 {
		t.Errorf("redelivery should be delayed, got %v", msg.nakedDelay)
	}
}

// TestPermanentReportRefusalClassification locks the rule that decides whether an
// unrecorded failure spins forever or is dropped: only a 4xx means the report
// will never be accepted. A 5xx or a transport error is the control plane being
// briefly unwell, and must not be read as a verdict on the message.
func TestPermanentReportRefusalClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"bad request is permanent", apiError(http.StatusBadRequest), true},
		{"forbidden is permanent", apiError(http.StatusForbidden), true},
		{"conflict is permanent", apiError(http.StatusConflict), true},
		{"service unavailable is transient", apiError(http.StatusServiceUnavailable), false},
		{"internal error is transient", apiError(http.StatusInternalServerError), false},
		{"opaque transport error is transient", errors.New("connection reset by peer"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := permanentReportRefusal(tc.err); got != tc.want {
				t.Fatalf("permanentReportRefusal(%v) = %v, want %v (status %d)",
					tc.err, got, tc.want, httpStatusOf(tc.err))
			}
		})
	}
}

// TestClassifyClaimFailure locks the status-class -> disposition table. The
// headline regression: a 5xx (transient) claim failure must never become a
// terminal or stale outcome, because either one deletes the dispatch for a run
// that may still be pending.
func TestClassifyClaimFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want claimOutcome
	}{
		{"service unavailable retries", apiError(http.StatusServiceUnavailable), claimRetry},
		{"internal error retries", apiError(http.StatusInternalServerError), claimRetry},
		{"conflict is stale", apiError(http.StatusConflict), claimStale},
		{"forbidden is terminal", apiError(http.StatusForbidden), claimTerminal},
		{"unauthorized is terminal", apiError(http.StatusUnauthorized), claimTerminal},
		{"bad request is terminal", apiError(http.StatusBadRequest), claimTerminal},
		{"not found is terminal", apiError(http.StatusNotFound), claimTerminal},
		{"opaque transport error retries", errors.New("connection reset by peer"), claimRetry},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyClaimFailure(tc.err); got != tc.want {
				t.Fatalf("classifyClaimFailure(%v) = %s, want %s", tc.err, outcomeName(got), outcomeName(tc.want))
			}
		})
	}
}

func apiError(code int) error {
	return &bark.ErrorResponse{Code: code, Message: http.StatusText(code)}
}

// --- claim() shutdown handling -------------------------------------------------

// stubService and stubResults let claim() run against a canned ClaimRun response
// without a real API. Only the two methods claim() reaches are implemented; the
// embedded interfaces satisfy the rest of the surface.
type stubService struct {
	urth.Service
	results   urth.RunResultAPI
	artifacts urth.ArtifactAPI
}

func (s stubService) Results(manifest.ResourceName) urth.RunResultAPI { return s.results }
func (s stubService) Artifacts() urth.ArtifactAPI                     { return s.artifacts }

type stubResults struct {
	urth.RunResultAPI
	auth urth.AuthJobResponse
	err  error

	// onClaim, when set, runs as the claim commits, so a test can place the claim
	// in an ordering against the acknowledgement and the probe.
	onClaim func()
}

func (s stubResults) ClaimRun(context.Context, manifest.ResourceID, urth.APIToken, urth.ClaimJobRequest) (urth.AuthJobResponse, error) {
	if s.onClaim != nil {
		s.onClaim()
	}

	return s.auth, s.err
}

func newTestWorker(claimErr error) *worker {
	return &worker{
		config:      &workerConfig{RunnerConfig: runner.NewDefaultConfig()},
		apiClient:   stubService{results: stubResults{err: claimErr}},
		claimBudget: 5 * time.Second,
		ackBudget:   time.Second,
	}
}

// TestClaimAbandonsOnShutdown proves that a claim cut short by worker shutdown is
// abandoned rather than classified: the run is neither acked away nor terminated
// on the strength of a request that never got an answer.
func TestClaimAbandonsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // simulate shutdown before the claim resolves

	w := newTestWorker(apiError(http.StatusServiceUnavailable))
	_, outcome := w.claim(ctx, natsq.DispatchEnvelope{ResultUID: "run-1"})

	if outcome != claimAbandon {
		t.Fatalf("claim during shutdown = %s, want abandon", outcomeName(outcome))
	}
}

// TestClaimClassifiesLiveFailure confirms that when the worker is not shutting
// down, a claim failure is classified by its status class -- here a 409 becomes a
// stale drop, not a retry.
func TestClaimClassifiesLiveFailure(t *testing.T) {
	w := newTestWorker(apiError(http.StatusConflict))
	_, outcome := w.claim(context.Background(), natsq.DispatchEnvelope{ResultUID: "run-1"})

	if outcome != claimStale {
		t.Fatalf("live 409 claim = %s, want stale", outcomeName(outcome))
	}
}

// --- handle(): ordering and duplicate delivery ---------------------------------

const testRunnerUID = manifest.ResourceID("runner-1")

// testEnvelope is a dispatch this worker would accept: right schema, right runner.
func testEnvelope(resultUID manifest.ResourceID) []byte {
	data, err := natsq.MarshalEnvelope(natsq.DispatchEnvelope{
		SchemaVersion: natsq.DispatchEnvelopeVersion,
		ResultUID:     resultUID,
		ResultVersion: 1,
		ScenarioName:  "a-scenario",
		RunnerUID:     testRunnerUID,
		DispatchID:    "dispatch-1",
	})
	if err != nil {
		panic(err)
	}

	return data
}

// TestHandleAcknowledgesBeforeExecuting is the ordering ADR 0004 §4 requires: the
// claim commits, the broker confirms the acknowledgement, and only then does the
// probe start. Each of the three steps appends to one log, so a change that moves
// the acknowledgement after execution -- which would make the ack-wait timer span
// an arbitrarily long probe -- shows up as a reordered slice rather than as a
// timing flake.
func TestHandleAcknowledgesBeforeExecuting(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(what string) { events = append(events, what) }

	w := newTestWorker(nil)
	w.runnerUID = testRunnerUID
	w.apiClient = stubService{results: stubResults{onClaim: func() { mu.Lock(); record("claim"); mu.Unlock() }}}
	w.executeJob = func(context.Context, natsq.DispatchEnvelope, urth.AuthJobResponse) {
		mu.Lock()
		record("execute")
		mu.Unlock()
	}

	msg := &fakeMsg{data: testEnvelope("run-1"), record: record}
	w.handle(context.Background(), msg)

	want := []string{"claim", "double-ack", "execute"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("handshake order = %v, want %v", events, want)
	}
}

// TestHandleDoesNotAckOrExecuteAFailedClaim is the other half of the ordering: a
// claim the API refused must leave the queue untouched by any acknowledgement and
// must not start a probe. Acking here would delete the only message for a run that
// may still be pending.
func TestHandleDoesNotAckOrExecuteAFailedClaim(t *testing.T) {
	w := newTestWorker(apiError(http.StatusServiceUnavailable))
	w.runnerUID = testRunnerUID

	var executed bool
	w.executeJob = func(context.Context, natsq.DispatchEnvelope, urth.AuthJobResponse) { executed = true }

	msg := &fakeMsg{data: testEnvelope("run-1")}
	w.handle(context.Background(), msg)

	if executed {
		t.Error("a refused claim must not execute the probe")
	}
	if plain, confirmed := msg.acks(); plain != 0 || confirmed != 0 {
		t.Errorf("acks after a refused claim = (%d plain, %d confirmed), want none", plain, confirmed)
	}
	if !msg.naked {
		t.Error("a transient claim failure should leave the message for redelivery")
	}
}

// TestHandleDeduplicatesConcurrentRedelivery is the regression this task exists
// for. A message redelivered while its run is still executing gets a *valid*
// authorization back -- the API's claim is idempotent for the same worker and
// dispatch, deliberately, because that is what lets a worker recover from a lost
// claim response. Without an in-process record of what it is already running, the
// worker cannot tell that recovery from a duplicate, and executes the same
// external probe twice at once.
func TestHandleDeduplicatesConcurrentRedelivery(t *testing.T) {
	w := newTestWorker(nil)
	w.runnerUID = testRunnerUID

	var executions atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})

	// Only the first execution holds the slot open. A second one returns at once
	// so that a regression here fails by counting two invocations rather than by
	// deadlocking the test and waiting out the package timeout.
	w.executeJob = func(context.Context, natsq.DispatchEnvelope, urth.AuthJobResponse) {
		if executions.Add(1) == 1 {
			close(started)
			<-release
		}
	}

	first := &fakeMsg{data: testEnvelope("run-1")}
	done := make(chan struct{})
	go func() {
		defer close(done)
		w.handle(context.Background(), first)
	}()

	<-started // the first execution is in flight

	duplicate := &fakeMsg{data: testEnvelope("run-1")}
	w.handle(context.Background(), duplicate)

	if got := executions.Load(); got != 1 {
		t.Errorf("probe invocations = %d, want 1", got)
	}

	// Acked and dropped: it describes work this process already owns, so holding
	// it would reserve one of the runner's MaxAckPending slots for the length of a
	// probe, and naking it would bring it back every AckWait until MaxDeliver filed
	// a dead letter for a dispatch that was delivered perfectly well.
	if plain, confirmed := duplicate.acks(); plain != 1 || confirmed != 0 {
		t.Errorf("duplicate acks = (%d plain, %d confirmed), want (1, 0)", plain, confirmed)
	}
	if duplicate.naked || duplicate.termed {
		t.Error("a duplicate delivery must not be naked or terminated")
	}

	close(release)
	<-done

	// Ownership is given up once the run is done, or a later legitimate dispatch
	// for a re-run of the same Result would be mistaken for a duplicate forever.
	if !w.inFlight.acquire("run-1") {
		t.Error("ownership was not released after the run finished")
	}
}
