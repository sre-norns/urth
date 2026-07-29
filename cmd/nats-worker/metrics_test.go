package main

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
)

// What the worker exports is what nobody else can see. From the broker's side a
// message that leaves the queue looks the same whether it left cleanly, after a
// confirmation that had to be retried, or as the second copy of a run already in
// flight -- so these three have to be counted here or not at all.

// scrape gathers the worker's registry and returns the metric families by name.
func scrape(t *testing.T, registry *prometheus.Registry) map[string]*dto.MetricFamily {
	t.Helper()

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		byName[family.GetName()] = family
	}

	return byName
}

// counter returns a counter's value, keyed by its single label value when it has
// one. A metric that was never touched reads as absent, which is Prometheus'
// behaviour and worth failing on rather than papering over with a zero.
func counter(t *testing.T, families map[string]*dto.MetricFamily, name, label string) float64 {
	t.Helper()

	family, ok := families[name]
	if !ok {
		t.Fatalf("metric %q was not reported", name)
	}

	for _, metric := range family.GetMetric() {
		if label == "" && len(metric.GetLabel()) == 0 {
			return metric.GetCounter().GetValue()
		}

		for _, pair := range metric.GetLabel() {
			if pair.GetValue() == label {
				return metric.GetCounter().GetValue()
			}
		}
	}

	t.Fatalf("metric %q has no sample for label %q", name, label)

	return 0
}

// histogramCount returns how many observations a histogram has recorded.
func histogramCount(t *testing.T, families map[string]*dto.MetricFamily, name string) uint64 {
	t.Helper()

	family, ok := families[name]
	if !ok {
		t.Fatalf("metric %q was not reported", name)
	}
	if len(family.GetMetric()) != 1 {
		t.Fatalf("metric %q has %d samples, want 1", name, len(family.GetMetric()))
	}

	return family.GetMetric()[0].GetHistogram().GetSampleCount()
}

// TestClaimOutcomesAreCounted proves the disposition an operator most needs to
// distinguish is distinguishable. A stale delivery and a transient failure both
// end with a message that did not run, and reading them as one number turns "the
// queue is draining normally" and "the API server is unwell" into the same graph.
func TestClaimOutcomesAreCounted(t *testing.T) {
	metrics, registry := newWorkerMetrics()

	w := newTestWorker(nil)
	w.metrics = metrics
	w.runnerUID = testRunnerUID
	w.executeJob = func(context.Context, natsq.DispatchEnvelope, urth.AuthJobResponse) {}
	w.handle(context.Background(), &fakeMsg{data: testEnvelope("run-1")})

	stale := newTestWorker(apiError(http.StatusConflict))
	stale.metrics = metrics
	stale.runnerUID = testRunnerUID
	stale.handle(context.Background(), &fakeMsg{data: testEnvelope("run-2")})

	families := scrape(t, registry)

	if got := counter(t, families, "urth_worker_claims_total", "accepted"); got != 1 {
		t.Errorf("accepted claims = %v, want 1", got)
	}
	if got := counter(t, families, "urth_worker_claims_total", "stale"); got != 1 {
		t.Errorf("stale claims = %v, want 1", got)
	}
	if got := histogramCount(t, families, "urth_worker_ack_confirm_seconds"); got != 1 {
		t.Errorf("ack confirmations = %v, want 1", got)
	}
}

// TestDuplicateDeliveryIsCounted makes the deduplication visible. It is otherwise
// completely silent: the run executes once, the message goes away, and nothing
// distinguishes a fleet whose acknowledgements are reliably landing from one
// where every job is being delivered twice and quietly dropped.
func TestDuplicateDeliveryIsCounted(t *testing.T) {
	metrics, registry := newWorkerMetrics()

	w := newTestWorker(nil)
	w.metrics = metrics
	w.runnerUID = testRunnerUID

	if !w.inFlight.acquire("run-1") {
		t.Fatal("could not take ownership of the run")
	}

	w.handle(context.Background(), &fakeMsg{data: testEnvelope("run-1")})

	if got := counter(t, scrape(t, registry), "urth_worker_duplicate_deliveries_total", ""); got != 1 {
		t.Errorf("duplicate deliveries = %v, want 1", got)
	}
}

// TestUnconfirmedAckIsCounted is the signal for the failure this task cannot
// eliminate, only bound. A confirmation that never arrives means the worker is
// executing a run whose message may still be redeliverable, and the only place
// that condition is ever visible is this counter.
func TestUnconfirmedAckIsCounted(t *testing.T) {
	metrics, registry := newWorkerMetrics()

	msg := &fakeMsg{doubleAckErr: errors.New("no responders")}
	if err := confirmAck(context.Background(), msg, 300*time.Millisecond, metrics); err == nil {
		t.Fatal("confirmAck reported success although every attempt failed")
	}

	families := scrape(t, registry)

	if got := counter(t, families, "urth_worker_ack_unconfirmed_total", ""); got != 1 {
		t.Errorf("unconfirmed acknowledgements = %v, want 1", got)
	}
	if got := counter(t, families, "urth_worker_ack_confirm_retries_total", ""); got < 1 {
		t.Errorf("confirmation retries = %v, want at least 1", got)
	}
}

// TestWorkerMetricsAreNilSafe covers the shipped default. --metrics-address is
// empty unless an operator asks for a listener, so every one of these call sites
// runs against a nil receiver on an ordinary worker; a panic here would be a
// crash on the claim path caused entirely by observability.
func TestWorkerMetricsAreNilSafe(t *testing.T) {
	var metrics *workerMetrics

	metrics.claimed(claimAccepted)
	metrics.ran("success")
	metrics.ackConfirmed(time.Millisecond)
	metrics.ackRetried()
	metrics.ackUnconfirmed()
	metrics.duplicateDelivery()
}
