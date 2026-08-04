package worker

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// workerMetrics is what this process can tell an operator about its own claim
// handshake.
//
// Deliberately about the handshake rather than about the queue: the queue is
// JetStream's to describe and the api-server already exports it
// (natsq.JetStreamCollector). What only the worker knows is what happened
// between pulling a message and starting a probe -- whether the claim was
// granted, whether the acknowledgement was ever confirmed by the server, and
// whether a redelivery arrived for a run this process was already executing.
// Those three are indistinguishable from the broker's side: a message that
// leaves the queue looks the same whether it left cleanly or after being
// delivered twice.
//
// Every method is nil-safe. A worker started without --metrics-address has no
// registry, and so do the tests that build a worker literal; making each call
// site check would spread that decision over the claim path for no benefit.
type workerMetrics struct {
	claims              *prometheus.CounterVec
	runs                *prometheus.CounterVec
	ackConfirmSeconds   prometheus.Histogram
	ackRetriesTotal     prometheus.Counter
	ackUnconfirmedTotal prometheus.Counter
	duplicateTotal      prometheus.Counter
}

// newWorkerMetrics builds the worker's collectors and its registry.
//
// A registry of its own rather than prometheus.DefaultRegisterer, matching the
// api-server: what this endpoint exposes should be a decision made here rather
// than whatever any imported package happened to register into the global.
func newWorkerMetrics() (*workerMetrics, *prometheus.Registry) {
	const namespace = "urth_worker"

	m := &workerMetrics{
		claims: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: namespace + "_claims_total",
			Help: "Claim attempts by what the worker decided to do about the message.",
		}, []string{"outcome"}),

		runs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: namespace + "_runs_total",
			Help: "Probes executed by this worker, by the result they reported.",
		}, []string{"result"}),

		ackConfirmSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: namespace + "_ack_confirm_seconds",
			Help: "Time from a committed claim to the server confirming its acknowledgement.",
			// Bucketed for a handshake, not for a probe: this is one request to
			// the broker, and the interesting question is whether it took
			// milliseconds or ate into the reserve.
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		}),

		ackRetriesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: namespace + "_ack_confirm_retries_total",
			Help: "Acknowledgement confirmations retried after an attempt failed or timed out.",
		}),

		ackUnconfirmedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: namespace + "_ack_unconfirmed_total",
			Help: "Claims whose acknowledgement was never confirmed within its budget; the probe ran anyway.",
		}),

		duplicateTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: namespace + "_duplicate_deliveries_total",
			Help: "Redeliveries dropped because this worker was already executing that run.",
		}),
	}

	registry := prometheus.NewRegistry()

	// Process and Go runtime metrics: the baseline any runbook assumes is there,
	// and what says whether the worker itself is healthy before its own numbers
	// are worth reading.
	registry.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	registry.MustRegister(m.claims, m.runs, m.ackConfirmSeconds,
		m.ackRetriesTotal, m.ackUnconfirmedTotal, m.duplicateTotal)

	return m, registry
}

func (m *workerMetrics) claimed(outcome claimOutcome) {
	if m == nil {
		return
	}

	m.claims.WithLabelValues(outcomeName(outcome)).Inc()
}

func (m *workerMetrics) ran(result string) {
	if m == nil {
		return
	}

	m.runs.WithLabelValues(result).Inc()
}

func (m *workerMetrics) ackConfirmed(took time.Duration) {
	if m == nil {
		return
	}

	m.ackConfirmSeconds.Observe(took.Seconds())
}

func (m *workerMetrics) ackRetried() {
	if m == nil {
		return
	}

	m.ackRetriesTotal.Inc()
}

func (m *workerMetrics) ackUnconfirmed() {
	if m == nil {
		return
	}

	m.ackUnconfirmedTotal.Inc()
}

func (m *workerMetrics) duplicateDelivery() {
	if m == nil {
		return
	}

	m.duplicateTotal.Inc()
}

// serveMetrics exposes the registry until the worker's context is cancelled.
//
// Opt-in, and off by default, because of where this process runs: a worker sits
// inside the network segment it is probing, often one whose whole point is that
// it has a small attack surface. Opening a listener nobody asked for is not a
// decision this binary gets to make on an operator's behalf.
//
// A failure to serve is logged and survived rather than fatal. A worker that
// cannot export metrics is still a worker that can run probes, and taking the
// fleet down because a port was already bound would be a worse outcome than
// losing observability of it.
func serveMetrics(ctx context.Context, address string, registry *prometheus.Registry) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		ErrorHandling: promhttp.ContinueOnError,
	}))

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("metrics endpoint did not shut down cleanly: %v", err)
		}
	}()

	log.Printf("serving metrics on %s/metrics", address)

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("metrics endpoint stopped: %v", err)
	}
}
