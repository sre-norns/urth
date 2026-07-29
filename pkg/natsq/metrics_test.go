package natsq_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// What an operator can see about the queue.
//
// Before this, nothing: the publication counters lived in a struct field nobody
// read, and everything else -- how much work is queued, how old it is, how much
// of it is being redelivered -- was answerable only through the NATS monitoring
// port, which is a different system from the one Urth's own alerts run on.

// gather scrapes the collector and returns the metric families by name.
func gather(t *testing.T, collector prometheus.Collector) map[string]*dto.MetricFamily {
	t.Helper()

	registry := prometheus.NewPedanticRegistry()
	if err := registry.Register(collector); err != nil {
		t.Fatalf("failed to register collector: %v", err)
	}

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

// value returns the single unlabelled sample of a metric.
func value(t *testing.T, families map[string]*dto.MetricFamily, name string) float64 {
	t.Helper()

	family, ok := families[name]
	if !ok {
		t.Fatalf("metric %q was not reported", name)
	}
	if len(family.GetMetric()) != 1 {
		t.Fatalf("metric %q has %d samples, want 1", name, len(family.GetMetric()))
	}

	return sampleValue(family.GetMetric()[0])
}

func sampleValue(m *dto.Metric) float64 {
	if m.GetGauge() != nil {
		return m.GetGauge().GetValue()
	}

	return m.GetCounter().GetValue()
}

// labelled returns a metric's samples keyed by the value of its `runner` label.
func labelled(t *testing.T, families map[string]*dto.MetricFamily, name string) map[string]float64 {
	t.Helper()

	samples := map[string]float64{}
	family, ok := families[name]
	if !ok {
		return samples
	}

	for _, m := range family.GetMetric() {
		for _, label := range m.GetLabel() {
			if label.GetName() == "runner" {
				samples[label.GetValue()] = sampleValue(m)
			}
		}
	}

	return samples
}

// stubCounters stands in for a transport's publication tally.
type stubCounters struct {
	published uint64
	failed    uint64
}

func (s stubCounters) PublishStats() (uint64, uint64) { return s.published, s.failed }

func TestMetricsReportStreamCapacityAndBacklog(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := testConfig()
	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	collector := natsq.NewJetStreamCollector(js)

	// An empty stream still reports its capacity: an alert on "how full is it"
	// needs the denominator before anything has been queued.
	families := gather(t, collector)
	if got := value(t, families, "urth_jetstream_stream_messages"); got != 0 {
		t.Errorf("empty stream reports %v messages, want 0", got)
	}
	if got := value(t, families, "urth_jetstream_stream_max_messages"); got != float64(cfg.MaxJobs) {
		t.Errorf("stream capacity is %v, want %d", got, cfg.MaxJobs)
	}
	if got := value(t, families, "urth_jetstream_stream_max_bytes"); got != float64(cfg.MaxBytes) {
		t.Errorf("stream byte capacity is %v, want %d", got, cfg.MaxBytes)
	}
	if got := value(t, families, "urth_jetstream_stream_oldest_message_age_seconds"); got != 0 {
		t.Errorf("empty stream reports an oldest message age of %v, want 0", got)
	}

	const runnerUID = manifest.ResourceID("runner-1")
	if _, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID); err != nil {
		t.Fatalf("failed to provision consumer: %v", err)
	}
	for i := range 3 {
		publishJob(t, js, runnerUID, manifest.ResourceID("result-"+itoa(i)), 1)
	}

	families = gather(t, collector)
	if got := value(t, families, "urth_jetstream_stream_messages"); got != 3 {
		t.Errorf("stream reports %v messages, want 3", got)
	}
	if got := value(t, families, "urth_jetstream_stream_bytes"); got <= 0 {
		t.Errorf("stream reports %v bytes for 3 queued jobs, want a positive size", got)
	}
	if got := value(t, families, "urth_jetstream_pending_messages"); got != 3 {
		t.Errorf("fleet pending is %v, want 3", got)
	}
	if got := labelled(t, families, "urth_jetstream_runner_pending_messages")[string(runnerUID)]; got != 3 {
		t.Errorf("runner pending is %v, want 3", got)
	}
	if got := value(t, families, "urth_jetstream_stream_consumers"); got != 1 {
		t.Errorf("stream reports %v consumers, want 1", got)
	}
}

// A job delivered and not yet acknowledged is a claim handshake in flight, and
// one that keeps being redelivered is a worker that cannot claim. Both are the
// numbers an operator reaches for when runs stop happening.
func TestMetricsReportAckPendingAndRedelivery(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := testConfig()
	cfg.AckWait = 300 * time.Millisecond

	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	const runnerUID = manifest.ResourceID("runner-1")
	consumer, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, runnerUID)
	if err != nil {
		t.Fatalf("failed to provision consumer: %v", err)
	}

	publishJob(t, js, runnerUID, "result-1", 1)

	// Delivered, deliberately not acknowledged: the worker that would claim it
	// has gone away mid-handshake.
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("failed to fetch: %v", err)
	}
	for range batch.Messages() {
	}

	collector := natsq.NewJetStreamCollector(js)

	families := gather(t, collector)
	if got := value(t, families, "urth_jetstream_ack_pending_messages"); got != 1 {
		t.Errorf("ack pending is %v, want 1 while the handshake is outstanding", got)
	}

	// Past AckWait the job becomes deliverable again, and the next worker to pull
	// gets it a second time. That redelivery is what the metric counts -- a
	// message merely eligible for redelivery has not been redelivered yet.
	time.Sleep(600 * time.Millisecond)

	batch, err = consumer.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("failed to fetch the redelivery: %v", err)
	}
	redelivered := 0
	for range batch.Messages() {
		redelivered++
	}
	if redelivered != 1 {
		t.Fatalf("the unacknowledged job should have come back, got %d messages", redelivered)
	}

	families = gather(t, collector)
	if got := value(t, families, "urth_jetstream_redelivered_messages"); got < 1 {
		t.Errorf("redelivered is %v, want at least 1 after the job was handed out again", got)
	}
}

// Per-runner series are capped, and the fleet totals are not.
//
// Runner UID is an unbounded label -- a deployment creating a runner per tenant
// or per branch writes a series per runner into Prometheus forever -- so beyond
// the cap only the busiest runners get their own series. "Is anything queued"
// must still be answerable exactly, which is why the totals are computed before
// the cap is applied.
func TestMetricsCapPerRunnerCardinality(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := testConfig()
	if _, err := natsq.EnsureJobStream(ctx, js, cfg); err != nil {
		t.Fatalf("failed to provision stream: %v", err)
	}

	// Three runners with different amounts of queued work.
	for i, jobs := range map[manifest.ResourceID]int{"runner-a": 1, "runner-b": 3, "runner-c": 2} {
		if _, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, i); err != nil {
			t.Fatalf("failed to provision consumer: %v", err)
		}
		for j := range jobs {
			publishJob(t, js, i, manifest.ResourceID(string(i)+"-result-"+itoa(j)), 1)
		}
	}

	collector := natsq.NewJetStreamCollector(js, natsq.WithMaxRunnerSeries(1))
	families := gather(t, collector)

	if got := value(t, families, "urth_jetstream_pending_messages"); got != 6 {
		t.Errorf("fleet pending is %v, want 6 -- totals are exact whatever the cap", got)
	}

	perRunner := labelled(t, families, "urth_jetstream_runner_pending_messages")
	if len(perRunner) != 1 {
		t.Fatalf("reported %d runner series, want 1 under the cap: %v", len(perRunner), perRunner)
	}
	// The busiest runner is the one worth a series.
	if got, ok := perRunner["runner-b"]; !ok || got != 3 {
		t.Errorf("reported %v, want the busiest runner (runner-b, 3 pending)", perRunner)
	}

	if got := value(t, families, "urth_jetstream_runners_unreported"); got != 2 {
		t.Errorf("unreported runners is %v, want 2", got)
	}
}

// The publication tally is this process's own, so it survives a broker that
// cannot be read -- which is exactly when an operator needs to tell "NATS is
// unreachable" apart from "nothing is being published".
func TestMetricsReportPublishCountersWhenTheBrokerIsUnreachable(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)

	// No stream provisioned: reading its state fails the way an unreachable
	// broker would, without the flakiness of actually stopping a server.
	collector := natsq.NewJetStreamCollector(js,
		natsq.WithPublishCounters(stubCounters{published: 7, failed: 2}),
		natsq.WithMetricsTimeout(2*time.Second),
	)

	families := gather(t, collector)

	if got := value(t, families, "urth_jetstream_published_total"); got != 7 {
		t.Errorf("published total is %v, want 7", got)
	}
	if got := value(t, families, "urth_jetstream_publish_failures_total"); got != 2 {
		t.Errorf("publish failures is %v, want 2", got)
	}
	if got := value(t, families, "urth_jetstream_scrape_failures_total"); got != 1 {
		t.Errorf("scrape failures is %v, want 1: a collector that reports nothing must say so", got)
	}
}
