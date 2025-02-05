package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"

	_ "github.com/sre-norns/urth/pkg/probers/dns"
	_ "github.com/sre-norns/urth/pkg/probers/grpc"
	_ "github.com/sre-norns/urth/pkg/probers/har"
	_ "github.com/sre-norns/urth/pkg/probers/http"
	_ "github.com/sre-norns/urth/pkg/probers/icmp"
	_ "github.com/sre-norns/urth/pkg/probers/puppeteer"

	// _ "github.com/sre-norns/urth/pkg/probers/pypuppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/tcp"
)

// Execute a single scenario
func Play(ctx context.Context, probSpec prob.Manifest, options prob.RunOptions) (urth.ResultStatus, []urth.ArtifactSpec, error) {
	if probSpec.Spec == nil {
		return urth.NewRunResults(prob.RunFinishedError, urth.WithStatus(urth.JobErrored)), nil, fmt.Errorf("no prob spec")
	}

	probFunc, ok := prob.FindRunFunc(probSpec.Kind)
	if !ok {
		return urth.NewRunResults(prob.RunFinishedError, urth.WithStatus(urth.JobErrored)), nil, fmt.Errorf("unsupported script kind: %q", probSpec.Kind)
	}

	probeSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "Displays whether or not the probe was a success",
	})
	probeDurationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "Returns how long the probe took to complete in seconds",
	})

	var logger RunLog
	slLogger := log.NewLogfmtLogger(&logger) // .Default() // TODO: Add a wrapper .New(logger)

	start := time.Now()
	registry := prometheus.NewRegistry()
	registry.MustRegister(probeSuccessGauge)
	registry.MustRegister(probeDurationGauge)

	slLogger.Log("Beginning probe", "kind", probSpec.Kind) //, "timeout_seconds", options.)
	result, sideEffects, err := probFunc(ctx, probSpec.Spec, options, registry, slLogger)

	duration := time.Since(start).Seconds()
	probeDurationGauge.Set(duration)
	if result == prob.RunFinishedSuccess {
		probeSuccessGauge.Set(1)
		slLogger.Log("Probe succeeded", "duration_seconds", duration)
	} else {
		slLogger.Log("Probe failed", "duration_seconds", duration)
	}

	artifacts := make([]urth.ArtifactSpec, 0, len(sideEffects)+1)
	for _, effect := range sideEffects {
		artifacts = append(artifacts, urth.ArtifactSpec{
			Artifact: effect,
		})
	}

	artifacts = append(artifacts, logger.ToArtifact())

	return urth.NewRunResults(result), artifacts, err
}
