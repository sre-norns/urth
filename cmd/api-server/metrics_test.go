package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sre-norns/urth/pkg/urth"
)

// The scrape endpoint is not a resource API.
//
// It is registered outside the `/api/v1` group on purpose: bark's content
// negotiation lives on that group and answers 406 to any Accept header it does
// not recognise, which is precisely how the live run log stream became
// unreachable from a browser (task 019). Prometheus asks for a text exposition
// format, so the same middleware would make this endpoint unscrapeable -- and,
// like the log stream, it would fail quietly, as a monitoring target that is
// simply always down.
func TestMetricsEndpointIsServedOutsideTheResourceAPI(t *testing.T) {
	registry := prometheus.NewRegistry()
	registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "urth_test_metric",
		Help: "A metric that exists only to be scraped by this test.",
	}, func() float64 { return 42 }))

	router := apiRoutes(nil, nil, registry)

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	// What Prometheus actually sends.
	request.Header.Set("Accept", "application/openmetrics-text;version=1.0.0,text/plain;version=0.0.4;q=0.5,*/*;q=0.1")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("scraping /metrics returned %d, want 200", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "urth_test_metric 42") {
		t.Errorf("scrape did not include the registered metric, got:\n%s", body)
	}
}

// A deployment that exports no metrics still serves its API: the endpoint is
// added only when there is a registry to serve, rather than answering with an
// empty page that reads as a fleet doing nothing.
func TestMetricsEndpointIsAbsentWithoutARegistry(t *testing.T) {
	router := apiRoutes(nil, nil, nil)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusNotFound {
		t.Errorf("/metrics returned %d with no registry, want 404", recorder.Code)
	}
}

// The API's own routes keep their content negotiation, so moving the scrape
// endpoint out of the group did not move anything else with it.
func TestResourceAPIStillNegotiatesContent(t *testing.T) {
	router := apiRoutes(nil, nil, prometheus.NewRegistry())

	request := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	request.Header.Set("Accept", "application/x-yaml")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("a YAML request to the resource API returned %d, want 200", recorder.Code)
	}
}

// Guards the composition rather than the collectors, which have their own tests:
// a registry that refuses a duplicate registration would panic at startup, and
// this is the cheapest place to find that out.
func TestMetricsRegistryComposes(t *testing.T) {
	registry := metricsRegistry(nil, nil, urth.NewPlacementMetrics())
	if registry == nil {
		t.Fatal("metricsRegistry returned nothing")
	}

	// A second identical registration must not be attempted internally.
	if err := registry.Register(urth.NewDispatchCollector(nil, nil)); err == nil {
		t.Error("the dispatch collector should already be registered")
	}
}
