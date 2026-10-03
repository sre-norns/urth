//go:build urth_native

package runner

import (
	"testing"

	"github.com/sre-norns/urth/pkg/prob"
)

func TestNativeWorkerProbeRegistry(t *testing.T) {
	want := []prob.Kind{"dns", "grpc", "har", "http", "icmp", "rest", "tcp"}
	registered := prob.ListProbs()
	if len(registered) != len(want) {
		t.Fatalf("native registry has %d probers; want %d", len(registered), len(want))
	}
	cfg := NewDefaultConfig()
	labels := cfg.GetEffectiveLabels()
	for _, kind := range want {
		if _, ok := registered[kind]; !ok {
			t.Errorf("native prober %q is absent", kind)
		}
		if _, ok := labels[kindAsLabel(kind)]; !ok {
			t.Errorf("native capability %q is absent", kind)
		}
	}
	for _, kind := range []prob.Kind{"puppeteer", "pypuppeteer"} {
		if _, ok := registered[kind]; ok {
			t.Errorf("external-runtime prober %q is registered", kind)
		}
		if _, ok := labels[kindAsLabel(kind)]; ok {
			t.Errorf("external-runtime capability %q is advertised", kind)
		}
	}
}
