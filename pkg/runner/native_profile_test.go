//go:build urth_native

package runner

import (
	"context"
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
	caps := cfg.discoverCapabilities(context.Background(), func(context.Context, string, string, ...string) ([]byte, error) {
		return []byte(`{"puppeteer":"24.0.0","path":"/browser"}`), nil
	}, func() (bool, bool) { return true, true })
	for _, kind := range want {
		if _, ok := registered[kind]; !ok {
			t.Errorf("native prober %q is absent", kind)
		}
		if _, ok := labels[kindAsLabel(kind)]; !ok {
			t.Errorf("native capability %q is absent", kind)
		}
	}
	for _, kind := range []prob.Kind{"puppeteer", "pypuppeteer"} {
		if _, ok := caps.ProbeVersions[string(kind)]; ok {
			t.Errorf("native typed capability %q is advertised", kind)
		}
		if _, ok := registered[kind]; ok {
			t.Errorf("external-runtime prober %q is registered", kind)
		}
		if _, ok := labels[kindAsLabel(kind)]; ok {
			t.Errorf("external-runtime capability %q is advertised", kind)
		}
	}
}
