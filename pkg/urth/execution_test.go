package urth_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/probers/dns"
	"github.com/sre-norns/urth/pkg/probers/grpc"
	"github.com/sre-norns/urth/pkg/probers/har"
	httpprob "github.com/sre-norns/urth/pkg/probers/http"
	"github.com/sre-norns/urth/pkg/probers/icmp"
	"github.com/sre-norns/urth/pkg/probers/puppeteer"
	"github.com/sre-norns/urth/pkg/probers/pypuppeteer"
	"github.com/sre-norns/urth/pkg/probers/rest"
	"github.com/sre-norns/urth/pkg/probers/tcp"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The snapshot is persisted with gorm's `serializer:json`, so storing and
// reloading it is exactly a JSON round trip. These tests assert on that round
// trip directly: the failure they exist to catch -- a prob kind that comes back
// as an untyped map because the registry did not recognise it, so the Worker
// receives a job it cannot cast -- is a property of the encoding, not of the
// database.

// storeAndReload puts a snapshot through the same encoding the column does.
func storeAndReload(t *testing.T, snapshot urth.ExecutionSnapshot) urth.ExecutionSnapshot {
	t.Helper()

	stored, err := json.Marshal(snapshot)
	require.NoError(t, err)

	var reloaded urth.ExecutionSnapshot
	require.NoError(t, json.Unmarshal(stored, &reloaded))

	return reloaded
}

// requireStableUnderStorage asserts that storing a reloaded snapshot again
// changes nothing.
//
// Stability rather than identity with the original, because a couple of prob
// specs do not encode symmetrically on the first pass: the http kind embeds
// prometheus/common's HTTPClientConfig, whose zero ProxyURL marshals to "" and
// unmarshals to a non-nil empty url.URL. That normalisation is semantically
// inert and predates the snapshot -- Scenario.Spec.Prob is stored through the
// same serializer -- but it means "equal to what went in" is the wrong
// assertion. What must hold is that a stored snapshot never drifts again, so a
// run claimed twice is the same run both times.
func requireStableUnderStorage(t *testing.T, reloaded urth.ExecutionSnapshot) {
	t.Helper()

	require.Equal(t, reloaded, storeAndReload(t, reloaded))
}

// Every prob kind this build knows about must survive storage as the typed value
// it went in as. A kind that decodes back to map[string]any still looks like a
// job right up to the point a Worker tries to run it.
func TestExecutionSnapshotRoundTripsEveryRegisteredProbKind(t *testing.T) {
	registered := prob.ListProbs()
	require.NotEmpty(t, registered, "the prober packages should have registered themselves")

	for kind := range registered {
		t.Run(string(kind), func(t *testing.T) {
			instance, err := prob.InstanceOf(kind)
			require.NoError(t, err, "a registered kind must be instantiable")

			given := urth.ExecutionSnapshot{
				ScenarioUID:     "scenario-uid",
				ScenarioName:    "a-scenario",
				ScenarioVersion: 7,
				Prob: prob.Manifest{
					Kind:    kind,
					Timeout: 42 * time.Second,
					Spec:    instance.Spec,
				},
			}

			got := storeAndReload(t, given)

			require.Equal(t, given.ScenarioUID, got.ScenarioUID)
			require.Equal(t, given.ScenarioName, got.ScenarioName)
			require.Equal(t, given.ScenarioVersion, got.ScenarioVersion,
				"the run must keep naming the scenario revision it was created from")
			require.Equal(t, given.Prob.Kind, got.Prob.Kind)
			require.Equal(t, given.Prob.Timeout, got.Prob.Timeout,
				"the effective execution budget travels with the snapshot")
			require.IsType(t, given.Prob.Spec, got.Prob.Spec,
				"the spec must come back as the registered type, not an untyped map")

			requireStableUnderStorage(t, got)
		})
	}
}

// The specs that carry the actual test -- a script, a target, a request template
// -- must come back byte for byte. A snapshot that keeps the kind but loses the
// script would dispatch a run that does nothing and reports success.
func TestExecutionSnapshotPreservesProbContent(t *testing.T) {
	testCases := map[string]struct {
		given  prob.Manifest
		expect func(t *testing.T, spec any)
	}{
		"http": {
			given: prob.Manifest{
				Kind: httpprob.Kind,
				Spec: &httpprob.Spec{Target: "https://example.com/health"},
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, "https://example.com/health", spec.(*httpprob.Spec).Target)
			},
		},
		"tcp": {
			given: prob.Manifest{
				Kind: tcp.Kind,
				Spec: &tcp.Spec{Target: "example.com:5432"},
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, "example.com:5432", spec.(*tcp.Spec).Target)
			},
		},
		"dns": {
			given: prob.Manifest{
				Kind: dns.Kind,
				Spec: &dns.Spec{Target: "example.com"},
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, "example.com", spec.(*dns.Spec).Target)
			},
		},
		"icmp": {
			given: prob.Manifest{
				Kind: icmp.Kind,
				Spec: &icmp.Spec{Target: "example.com"},
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, "example.com", spec.(*icmp.Spec).Target)
			},
		},
		"grpc": {
			given: prob.Manifest{
				Kind: grpc.Kind,
				Spec: &grpc.Spec{Target: "example.com:9090"},
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, "example.com:9090", spec.(*grpc.Spec).Target)
			},
		},
		"har": {
			given: prob.Manifest{
				Kind: har.Kind,
				Spec: &har.Spec{Script: `{"log":{"version":"1.2","entries":[]}}`},
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, `{"log":{"version":"1.2","entries":[]}}`, spec.(*har.Spec).Script)
			},
		},
		"rest": {
			given: prob.Manifest{
				Kind: rest.Kind,
				Spec: &rest.Spec{
					FollowRedirects: true,
					Script:          "GET https://example.com/api\nAuthorization: Bearer hunter2\n",
				},
			},
			expect: func(t *testing.T, spec any) {
				got := spec.(*rest.Spec)
				require.True(t, got.FollowRedirects)
				require.Equal(t, "GET https://example.com/api\nAuthorization: Bearer hunter2\n", got.Script)
			},
		},
		"puppeteer": {
			given: prob.Manifest{
				Kind: puppeteer.Kind,
				Spec: &puppeteer.Spec{Port: 8080, Script: "await page.goto('https://example.com')"},
			},
			expect: func(t *testing.T, spec any) {
				got := spec.(*puppeteer.Spec)
				require.Equal(t, 8080, got.Port)
				require.Equal(t, "await page.goto('https://example.com')", got.Script)
			},
		},
		"pypuppeteer": {
			given: prob.Manifest{
				Kind: pypuppeteer.Kind,
				// This kind registers a bare string as its spec prototype, so
				// the whole script is the spec.
				Spec: ptr("await page.goto('https://example.com')"),
			},
			expect: func(t *testing.T, spec any) {
				require.Equal(t, "await page.goto('https://example.com')", *spec.(*string))
			},
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			given := urth.ExecutionSnapshot{
				ScenarioUID:  "scenario-uid",
				ScenarioName: manifest.ResourceName(name),
				Prob:         test.given,
			}

			got := storeAndReload(t, given)
			require.Equal(t, given.Prob.Kind, got.Prob.Kind)
			test.expect(t, got.Prob.Spec)

			requireStableUnderStorage(t, got)
		})
	}
}

func ptr[T any](value T) *T {
	return &value
}

// A Result that carries no snapshot has to be distinguishable from one that
// carries an empty-looking snapshot, because the two get opposite treatment:
// the first is refused, the second is run.
func TestExecutionSnapshotIsZero(t *testing.T) {
	require.True(t, urth.ExecutionSnapshot{}.IsZero(),
		"a NULL column reads back as the zero value and means 'no snapshot'")

	require.False(t, urth.ExecutionSnapshot{
		ScenarioUID: "scenario-uid",
	}.IsZero())

	require.False(t, urth.ExecutionSnapshot{
		Prob: prob.Manifest{Kind: tcp.Kind},
	}.IsZero())
}

// Validation happens before the Result is written, so a stored pending run is
// always executable. Each of these would otherwise be found out only after a
// dispatch, a claim and an execution lease had been spent on it.
func TestExecutionSnapshotValidate(t *testing.T) {
	complete := urth.ExecutionSnapshot{
		ScenarioUID: "scenario-uid",
		Prob: prob.Manifest{
			Kind: tcp.Kind,
			Spec: &tcp.Spec{Target: "example.com:5432"},
		},
	}
	require.NoError(t, complete.Validate())

	noScenario := complete
	noScenario.ScenarioUID = ""
	require.Error(t, noScenario.Validate())

	noKind := complete
	noKind.Prob.Kind = ""
	require.Error(t, noKind.Validate())

	noSpec := complete
	noSpec.Prob.Spec = nil
	require.Error(t, noSpec.Validate())

	// A kind this process was not built with is still schedulable: the API server
	// does not execute probes, and refusing here would let its link-time prober
	// set means the deployment could not run a Worker that knows more kinds.
	unknownKind := complete
	unknownKind.Prob.Kind = "a-kind-this-build-has-never-heard-of"
	require.NoError(t, unknownKind.Validate())
}
