package natsq

import (
	"context"
	"log"
	"sort"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// Metric defaults.
const (
	// DefaultMetricsTimeout bounds one scrape's conversation with the broker. A
	// scrape that hangs holds a Prometheus connection and tells an operator
	// nothing; failing fast and reporting the failure as a metric does both
	// better.
	DefaultMetricsTimeout = 5 * time.Second

	// DefaultMaxRunnerSeries bounds per-runner cardinality.
	//
	// Runner UID is an unbounded label: a deployment that creates runners per
	// tenant, per branch, or per test run would otherwise write a time series per
	// runner into Prometheus forever, and the cost lands on the monitoring system
	// rather than here. Beyond this many runners, only the busiest are reported
	// individually -- the fleet totals are always exact.
	DefaultMaxRunnerSeries = 100
)

// MetricsSource is a transport that can report its own JetStream state.
//
// Offered as an optional interface rather than added to Transport: a command
// that does not export metrics should not have to satisfy it, and the JetStream
// handle stays private to this package either way.
type MetricsSource interface {
	Collector(options ...CollectorOption) *JetStreamCollector
}

// Collector implements MetricsSource.
//
// The transport builds its own collector because it already holds the JetStream
// connection and its own publication counters; handing both out so a caller
// could assemble one would widen this package's surface for no benefit.
func (s *scheduler) Collector(options ...CollectorOption) *JetStreamCollector {
	options = append([]CollectorOption{
		WithPublishCounters(s),
		WithMaxRunnerSeries(s.cfg.MaxRunnerSeries),
	}, options...)

	return NewJetStreamCollector(s.js, options...)
}

// PublishCounters is the publication tally a transport keeps.
//
// An interface rather than a direct dependency on the scheduler, so the
// collector can be built for a transport that is not publishing -- a control
// plane that only reads -- and so tests can supply their own numbers.
type PublishCounters interface {
	// PublishStats returns the total dispatches published and the total that
	// failed since this process started.
	PublishStats() (published, failed uint64)
}

// JetStreamCollector reports the state of the jobs stream to Prometheus.
//
// It queries the broker when scraped rather than keeping its own counters,
// because the numbers that matter -- how much work is queued, how old the oldest
// job is, how many deliveries are outstanding -- are JetStream's, and a copy
// held here would be another thing to keep in step. The exception is the
// publication tally, which is this process's own and has no broker equivalent.
//
// The stream's own gauges are exact regardless of fleet size. Per-runner series
// are capped; see DefaultMaxRunnerSeries.
type JetStreamCollector struct {
	js       jetstream.JetStream
	counters PublishCounters

	maxRunnerSeries int
	timeout         time.Duration

	streamMessages    *prometheus.Desc
	streamBytes       *prometheus.Desc
	streamMaxMessages *prometheus.Desc
	streamMaxBytes    *prometheus.Desc
	streamOldestAge   *prometheus.Desc
	streamConsumers   *prometheus.Desc

	runnerPending      *prometheus.Desc
	runnerAckPending   *prometheus.Desc
	runnerRedelivered  *prometheus.Desc
	fleetPending       *prometheus.Desc
	fleetAckPending    *prometheus.Desc
	fleetRedelivered   *prometheus.Desc
	runnersUnreported  *prometheus.Desc
	publishedTotal     *prometheus.Desc
	publishFailedTotal *prometheus.Desc
	scrapeFailures     *prometheus.Desc

	// failures counts scrapes that could not read the broker. Exported as a
	// metric because a collector that silently reports nothing is
	// indistinguishable from a fleet that is idle.
	failures atomic.Uint64
}

// CollectorOption configures a JetStreamCollector.
type CollectorOption func(*JetStreamCollector)

// WithPublishCounters attaches this process's publication tally.
func WithPublishCounters(counters PublishCounters) CollectorOption {
	return func(c *JetStreamCollector) { c.counters = counters }
}

// WithMaxRunnerSeries caps how many runners are reported individually.
func WithMaxRunnerSeries(limit int) CollectorOption {
	return func(c *JetStreamCollector) { c.maxRunnerSeries = limit }
}

// WithMetricsTimeout bounds one scrape.
func WithMetricsTimeout(timeout time.Duration) CollectorOption {
	return func(c *JetStreamCollector) { c.timeout = timeout }
}

// NewJetStreamCollector builds the collector for the jobs stream.
func NewJetStreamCollector(js jetstream.JetStream, options ...CollectorOption) *JetStreamCollector {
	const namespace = "urth_jetstream"

	runnerLabels := []string{"runner"}

	c := &JetStreamCollector{
		js:              js,
		maxRunnerSeries: DefaultMaxRunnerSeries,
		timeout:         DefaultMetricsTimeout,

		streamMessages: prometheus.NewDesc(namespace+"_stream_messages",
			"Jobs currently queued in the jobs stream.", nil, nil),
		streamBytes: prometheus.NewDesc(namespace+"_stream_bytes",
			"Bytes the jobs stream currently occupies.", nil, nil),
		streamMaxMessages: prometheus.NewDesc(namespace+"_stream_max_messages",
			"Configured message limit for the jobs stream; publication is refused at this point.", nil, nil),
		streamMaxBytes: prometheus.NewDesc(namespace+"_stream_max_bytes",
			"Configured byte limit for the jobs stream; publication is refused at this point.", nil, nil),
		streamOldestAge: prometheus.NewDesc(namespace+"_stream_oldest_message_age_seconds",
			"Age of the oldest queued job. Near the configured max age, jobs are about to expire unclaimed.", nil, nil),
		streamConsumers: prometheus.NewDesc(namespace+"_stream_consumers",
			"Runner queues bound to the jobs stream.", nil, nil),

		runnerPending: prometheus.NewDesc(namespace+"_runner_pending_messages",
			"Jobs queued for one runner and not yet delivered.", runnerLabels, nil),
		runnerAckPending: prometheus.NewDesc(namespace+"_runner_ack_pending_messages",
			"Claim handshakes outstanding for one runner.", runnerLabels, nil),
		runnerRedelivered: prometheus.NewDesc(namespace+"_runner_redelivered_messages",
			"Jobs currently awaiting redelivery for one runner.", runnerLabels, nil),

		fleetPending: prometheus.NewDesc(namespace+"_pending_messages",
			"Jobs queued across every runner. Exact regardless of fleet size.", nil, nil),
		fleetAckPending: prometheus.NewDesc(namespace+"_ack_pending_messages",
			"Claim handshakes outstanding across every runner.", nil, nil),
		fleetRedelivered: prometheus.NewDesc(namespace+"_redelivered_messages",
			"Jobs awaiting redelivery across every runner.", nil, nil),
		runnersUnreported: prometheus.NewDesc(namespace+"_runners_unreported",
			"Runners omitted from per-runner series because the cardinality cap was reached.", nil, nil),

		publishedTotal: prometheus.NewDesc(namespace+"_published_total",
			"Dispatches this process has published since start.", nil, nil),
		publishFailedTotal: prometheus.NewDesc(namespace+"_publish_failures_total",
			"Dispatch publications this process has failed since start.", nil, nil),
		scrapeFailures: prometheus.NewDesc(namespace+"_scrape_failures_total",
			"Scrapes that could not read JetStream state.", nil, nil),
	}

	for _, option := range options {
		option(c)
	}

	return c
}

// Describe implements prometheus.Collector.
func (c *JetStreamCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.streamMessages, c.streamBytes, c.streamMaxMessages, c.streamMaxBytes,
		c.streamOldestAge, c.streamConsumers,
		c.runnerPending, c.runnerAckPending, c.runnerRedelivered,
		c.fleetPending, c.fleetAckPending, c.fleetRedelivered, c.runnersUnreported,
		c.publishedTotal, c.publishFailedTotal, c.scrapeFailures,
	} {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
//
// A broker that cannot be read fails this scrape and nothing else: the
// publication counters are this process's own and are still reported, so an
// operator can tell "NATS is unreachable" from "nothing is being published".
func (c *JetStreamCollector) Collect(ch chan<- prometheus.Metric) {
	if c.counters != nil {
		published, failed := c.counters.PublishStats()
		ch <- prometheus.MustNewConstMetric(c.publishedTotal, prometheus.CounterValue, float64(published))
		ch <- prometheus.MustNewConstMetric(c.publishFailedTotal, prometheus.CounterValue, float64(failed))
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	if err := c.collectStream(ctx, ch); err != nil {
		log.Printf("metrics: failed to read JetStream state: %v", err)
		c.failures.Add(1)
	}

	ch <- prometheus.MustNewConstMetric(c.scrapeFailures, prometheus.CounterValue, float64(c.failures.Load()))
}

// runnerState is one runner queue's numbers, as read from its consumer.
type runnerState struct {
	runner      string
	pending     uint64
	ackPending  int
	redelivered int
}

func (c *JetStreamCollector) collectStream(ctx context.Context, ch chan<- prometheus.Metric) error {
	stream, err := c.js.Stream(ctx, JobsStreamName)
	if err != nil {
		return err
	}

	info, err := stream.Info(ctx)
	if err != nil {
		return err
	}

	ch <- prometheus.MustNewConstMetric(c.streamMessages, prometheus.GaugeValue, float64(info.State.Msgs))
	ch <- prometheus.MustNewConstMetric(c.streamBytes, prometheus.GaugeValue, float64(info.State.Bytes))
	ch <- prometheus.MustNewConstMetric(c.streamMaxMessages, prometheus.GaugeValue, float64(info.Config.MaxMsgs))
	ch <- prometheus.MustNewConstMetric(c.streamMaxBytes, prometheus.GaugeValue, float64(info.Config.MaxBytes))
	ch <- prometheus.MustNewConstMetric(c.streamConsumers, prometheus.GaugeValue, float64(info.State.Consumers))

	// Zero when the stream is empty, which is the honest answer: there is no
	// oldest message, and reporting the age of one that has been consumed would
	// read as a backlog that is not there.
	var oldest float64
	if info.State.Msgs > 0 && !info.State.FirstTime.IsZero() {
		oldest = time.Since(info.State.FirstTime).Seconds()
	}
	ch <- prometheus.MustNewConstMetric(c.streamOldestAge, prometheus.GaugeValue, oldest)

	states, err := c.collectConsumers(ctx, stream)
	if err != nil {
		return err
	}

	var fleet runnerState
	for _, state := range states {
		fleet.pending += state.pending
		fleet.ackPending += state.ackPending
		fleet.redelivered += state.redelivered
	}

	ch <- prometheus.MustNewConstMetric(c.fleetPending, prometheus.GaugeValue, float64(fleet.pending))
	ch <- prometheus.MustNewConstMetric(c.fleetAckPending, prometheus.GaugeValue, float64(fleet.ackPending))
	ch <- prometheus.MustNewConstMetric(c.fleetRedelivered, prometheus.GaugeValue, float64(fleet.redelivered))

	reported := c.reportable(states)
	ch <- prometheus.MustNewConstMetric(c.runnersUnreported, prometheus.GaugeValue, float64(len(states)-len(reported)))

	for _, state := range reported {
		ch <- prometheus.MustNewConstMetric(c.runnerPending, prometheus.GaugeValue, float64(state.pending), state.runner)
		ch <- prometheus.MustNewConstMetric(c.runnerAckPending, prometheus.GaugeValue, float64(state.ackPending), state.runner)
		ch <- prometheus.MustNewConstMetric(c.runnerRedelivered, prometheus.GaugeValue, float64(state.redelivered), state.runner)
	}

	return nil
}

// collectConsumers reads every runner queue's state.
func (c *JetStreamCollector) collectConsumers(ctx context.Context, stream jetstream.Stream) ([]runnerState, error) {
	var states []runnerState

	listing := stream.ListConsumers(ctx)
	for info := range listing.Info() {
		states = append(states, runnerState{
			runner:      runnerFromConsumerName(info.Name),
			pending:     info.NumPending,
			ackPending:  info.NumAckPending,
			redelivered: info.NumRedelivered,
		})
	}

	return states, listing.Err()
}

// reportable picks which runners get their own time series.
//
// Sorted by pending work, then by name so the choice is deterministic: under the
// cap every runner is reported, and over it the ones with a backlog are the ones
// worth a series. The fleet totals above are unaffected either way, so a capped
// scrape still answers "is anything queued" exactly -- it only stops answering
// "for which of these two hundred runners".
func (c *JetStreamCollector) reportable(states []runnerState) []runnerState {
	sort.Slice(states, func(i, j int) bool {
		if states[i].pending != states[j].pending {
			return states[i].pending > states[j].pending
		}

		return states[i].runner < states[j].runner
	})

	if c.maxRunnerSeries > 0 && len(states) > c.maxRunnerSeries {
		return states[:c.maxRunnerSeries]
	}

	return states
}

// runnerFromConsumerName recovers the runner UID from its durable name, so the
// label reads as the resource an operator knows rather than as JetStream's
// naming of it.
func runnerFromConsumerName(name string) string {
	const prefix = "runner-"
	if len(name) > len(prefix) && name[:len(prefix)] == prefix {
		return name[len(prefix):]
	}

	return name
}
