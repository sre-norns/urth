package main

import (
	"context"
	"errors"
	"net/http"
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
type fakeMsg struct {
	acked      bool
	nakedDelay time.Duration
	naked      bool
	termed     bool
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *fakeMsg) Data() []byte                              { return nil }
func (m *fakeMsg) Headers() nats.Header                      { return nil }
func (m *fakeMsg) Subject() string                           { return "" }
func (m *fakeMsg) Reply() string                             { return "" }
func (m *fakeMsg) Ack() error                                { m.acked = true; return nil }
func (m *fakeMsg) DoubleAck(context.Context) error           { m.acked = true; return nil }
func (m *fakeMsg) Nak() error                                { m.naked = true; return nil }
func (m *fakeMsg) NakWithDelay(d time.Duration) error {
	m.naked = true
	m.nakedDelay = d
	return nil
}
func (m *fakeMsg) InProgress() error           { return nil }
func (m *fakeMsg) Term() error                 { m.termed = true; return nil }
func (m *fakeMsg) TermWithReason(string) error { m.termed = true; return nil }

// TestApplyDisposition proves each claim outcome maps to exactly one queue action,
// and that a claim interrupted by shutdown leaves the message untouched for
// redelivery -- the outcome that did not exist in the prototype, where every
// failure collapsed into ack-or-nak.
func TestApplyDisposition(t *testing.T) {
	cases := []struct {
		name        string
		outcome     claimOutcome
		wantExecute bool
		wantAck     bool
		wantNak     bool
		wantTerm    bool
	}{
		{name: "accepted acks and executes", outcome: claimAccepted, wantExecute: true, wantAck: true},
		{name: "retry naks for redelivery", outcome: claimRetry, wantNak: true},
		{name: "stale acks and drops", outcome: claimStale, wantAck: true},
		{name: "terminal terminates", outcome: claimTerminal, wantTerm: true},
		{name: "abandon leaves the message untouched", outcome: claimAbandon},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &fakeMsg{}
			execute := applyDisposition(msg, tc.outcome, manifest.ResourceID("run-1"))

			if execute != tc.wantExecute {
				t.Errorf("execute = %v, want %v", execute, tc.wantExecute)
			}
			if msg.acked != tc.wantAck {
				t.Errorf("acked = %v, want %v", msg.acked, tc.wantAck)
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

// TestApplyDispositionRetryDelays confirms a retry is a delayed NAK, so a
// struggling API server is not immediately hammered with the same claim.
func TestApplyDispositionRetryDelays(t *testing.T) {
	msg := &fakeMsg{}
	applyDisposition(msg, claimRetry, manifest.ResourceID("run-1"))
	if msg.nakedDelay <= 0 {
		t.Fatalf("retry should NAK with a positive delay, got %v", msg.nakedDelay)
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
	results urth.RunResultAPI
}

func (s stubService) Results(manifest.ResourceName) urth.RunResultAPI { return s.results }

type stubResults struct {
	urth.RunResultAPI
	auth urth.AuthJobResponse
	err  error
}

func (s stubResults) ClaimRun(context.Context, manifest.ResourceID, urth.APIToken, urth.ClaimJobRequest) (urth.AuthJobResponse, error) {
	return s.auth, s.err
}

func newTestWorker(claimErr error) *worker {
	return &worker{
		config:    &workerConfig{RunnerConfig: runner.NewDefaultConfig()},
		apiClient: stubService{results: stubResults{err: claimErr}},
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
