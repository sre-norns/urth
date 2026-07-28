package urth

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"
)

// DefaultMetricsTimeout bounds one scrape's database work.
//
// A scrape that waits on a slow query holds a Prometheus connection and delays
// the next scrape; failing fast and saying so is more useful than a metric that
// eventually arrives.
const DefaultMetricsTimeout = 5 * time.Second

// DispatchCollector reports what the control plane knows about work that has
// not happened yet.
//
// It is the other half of the JetStream metrics: the broker can say what is
// queued, and only Postgres can say what was committed and never got there. A
// backlog that is growing in the outbox while the stream is empty is the relay;
// the same backlog with a full stream is the fleet. Read one without the other
// and the two look identical.
type DispatchCollector struct {
	db     *gorm.DB
	outbox DispatchOutbox

	timeout time.Duration

	pending      *prometheus.Desc
	failing      *prometheus.Desc
	oldestAge    *prometheus.Desc
	maxAttempts  *prometheus.Desc
	deadLetters  *prometheus.Desc
	scrapeErrors *prometheus.Desc

	failures atomic.Uint64
}

// NewDispatchCollector builds the collector over an outbox and its database.
func NewDispatchCollector(db *gorm.DB, outbox DispatchOutbox) *DispatchCollector {
	const namespace = "urth_dispatch"

	return &DispatchCollector{
		db:      db,
		outbox:  outbox,
		timeout: DefaultMetricsTimeout,

		pending: prometheus.NewDesc(namespace+"_outbox_pending",
			"Committed dispatches not yet published to the transport.", nil, nil),
		failing: prometheus.NewDesc(namespace+"_outbox_failing",
			"Unpublished dispatches that have already failed at least once.", nil, nil),
		oldestAge: prometheus.NewDesc(namespace+"_outbox_oldest_age_seconds",
			"Age of the oldest unpublished dispatch. This is the number to alert on: it stays near zero while the relay keeps up, whatever the throughput.", nil, nil),
		maxAttempts: prometheus.NewDesc(namespace+"_outbox_max_attempts",
			"Highest publication attempt count among unpublished dispatches.", nil, nil),
		deadLetters: prometheus.NewDesc(namespace+"_dead_letters_unresolved",
			"Dispatch failures an operator has not yet resolved.", nil, nil),
		scrapeErrors: prometheus.NewDesc(namespace+"_scrape_failures_total",
			"Scrapes that could not read dispatch state from the database.", nil, nil),
	}
}

// Describe implements prometheus.Collector.
func (c *DispatchCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.pending, c.failing, c.oldestAge, c.maxAttempts, c.deadLetters, c.scrapeErrors,
	} {
		ch <- desc
	}
}

// Collect implements prometheus.Collector.
func (c *DispatchCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	var failed bool

	if stats, err := c.outbox.Stats(ctx, time.Now()); err != nil {
		log.Printf("metrics: failed to read the dispatch outbox: %v", err)
		failed = true
	} else {
		ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(stats.Pending))
		ch <- prometheus.MustNewConstMetric(c.failing, prometheus.GaugeValue, float64(stats.Failing))
		ch <- prometheus.MustNewConstMetric(c.oldestAge, prometheus.GaugeValue, stats.OldestAge.Seconds())
		ch <- prometheus.MustNewConstMetric(c.maxAttempts, prometheus.GaugeValue, float64(stats.MaxAttempts))
	}

	if unresolved, err := c.unresolvedDeadLetters(ctx); err != nil {
		log.Printf("metrics: failed to count unresolved dispatch failures: %v", err)
		failed = true
	} else {
		ch <- prometheus.MustNewConstMetric(c.deadLetters, prometheus.GaugeValue, float64(unresolved))
	}

	if failed {
		c.failures.Add(1)
	}

	ch <- prometheus.MustNewConstMetric(c.scrapeErrors, prometheus.CounterValue, float64(c.failures.Load()))
}

// unresolvedDeadLetters counts the dead-letter records still waiting for someone.
//
// The working set, not the history: a resolved failure is a decision already
// made, and counting it would make the figure only ever grow, which is the
// quickest way to have an alert ignored.
func (c *DispatchCollector) unresolvedDeadLetters(ctx context.Context) (int64, error) {
	var total int64
	err := c.db.WithContext(ctx).
		Model(&DispatchFailure{}).
		Where("status_resolved = ?", false).
		Where("deleted_at IS NULL").
		Count(&total).Error

	return total, err
}
