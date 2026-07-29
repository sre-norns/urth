package urth_test

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/urth"
)

// What the control plane can say about work that has been committed and has not
// happened yet.
//
// This is the half of the pipeline JetStream cannot see: a dispatch written to
// the outbox and never published looks, from the broker's side, exactly like a
// dispatch nobody ever wanted. The outbox age is the number to alert on -- it
// sits near zero while the relay keeps up, whatever the throughput, and grows
// the moment publication stops.

// metricValue scrapes one unlabelled sample out of a registry.
func metricValue(t *testing.T, registry *prometheus.Registry, name string) float64 {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}

		require.Len(t, family.GetMetric(), 1, "metric %q should have one sample", name)

		sample := family.GetMetric()[0]
		if sample.GetCounter() != nil {
			return sample.GetCounter().GetValue()
		}

		return sample.GetGauge().GetValue()
	}

	t.Fatalf("metric %q was not reported", name)

	return 0
}

// dispatchMetrics registers the collector under test against a fresh registry.
func dispatchMetrics(t *testing.T, srv urth.Service) (*prometheus.Registry, urth.Service) {
	t.Helper()

	return prometheus.NewPedanticRegistry(), srv
}

func TestDispatchMetricsReportTheOutboxBacklog(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)

	registry, _ := dispatchMetrics(t, srv)
	require.NoError(t, registry.Register(urth.NewDispatchCollector(db, urth.NewDispatchOutbox(db))))

	// Nothing committed yet.
	require.Zero(t, metricValue(t, registry, "urth_dispatch_outbox_pending"),
		"an idle control plane has no backlog")

	// Creating a run commits the Result and its dispatch together.
	_, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)

	require.EqualValues(t, 1, metricValue(t, registry, "urth_dispatch_outbox_pending"))
	require.Zero(t, metricValue(t, registry, "urth_dispatch_outbox_failing"),
		"an entry that has not been attempted has not failed")
	require.Zero(t, metricValue(t, registry, "urth_dispatch_outbox_max_attempts"))
}

// A dead letter is a failure somebody has to look at, so the figure is the
// unresolved working set rather than the history: a number that only ever grows
// is one that gets ignored.
func TestDispatchMetricsCountUnresolvedDeadLetters(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")

	registry, _ := dispatchMetrics(t, srv)
	require.NoError(t, registry.Register(urth.NewDispatchCollector(db, urth.NewDispatchOutbox(db))))

	require.Zero(t, metricValue(t, registry, "urth_dispatch_dead_letters_unresolved"))

	failure, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
		Reason:        urth.ReasonPolicyRefused,
		EventUID:      urth.DispatchEventUID(run.UID, run.Version),
		DispatchID:    urth.DispatchEventUID(run.UID, run.Version),
		ResultUID:     run.UID,
		ResultVersion: run.Version,
		Detail:        "the API refused this claim permanently",
	})
	require.NoError(t, err)

	require.EqualValues(t, 1, metricValue(t, registry, "urth_dispatch_dead_letters_unresolved"))

	// Resolving takes it out of the working set without deleting the record.
	_, err = srv.DispatchFailures().Resolve(ctx, failure.Name)
	require.NoError(t, err)

	require.Zero(t, metricValue(t, registry, "urth_dispatch_dead_letters_unresolved"),
		"a resolved failure is a decision already made")
}

// A database that cannot be read reports the failure rather than reporting
// zeroes: a collector that silently returns nothing is indistinguishable from a
// control plane with nothing to do.
func TestDispatchMetricsReportScrapeFailures(t *testing.T) {
	_, db, _ := newTestService(t, &stubScheduler{})

	registry, _ := dispatchMetrics(t, nil)
	require.NoError(t, registry.Register(urth.NewDispatchCollector(db, urth.NewDispatchOutbox(db))))

	require.NoError(t, db.Migrator().DropTable(&urth.DispatchOutboxEntry{}))

	require.EqualValues(t, 1, metricValue(t, registry, "urth_dispatch_scrape_failures_total"))
	require.EqualValues(t, 2, metricValue(t, registry, "urth_dispatch_scrape_failures_total"),
		"each failed scrape counts")
}
