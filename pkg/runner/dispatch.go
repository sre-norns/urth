package runner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
	_ "github.com/sre-norns/urth/pkg/probers/rest"
	_ "github.com/sre-norns/urth/pkg/probers/tcp"
)

// PlayOption adjusts how a run is executed.
//
// Variadic options rather than another parameter, so that adding a way to
// observe a run does not disturb existing callers -- cmd/asynq-runner passes
// none of these and continues to work unchanged.
type PlayOption func(*playConfig)

type playConfig struct {
	logPublisher LogPublisher
}

// WithLogPublisher tees the run's log to a publisher as it is written, in
// addition to capturing it as an artifact.
//
// The artifact remains the authoritative record; this is for watching a run
// that is still going.
func WithLogPublisher(publisher LogPublisher) PlayOption {
	return func(c *playConfig) { c.logPublisher = publisher }
}

// Play executes a single scenario, returning its result along with the
// artifacts it produced.
func Play(ctx context.Context, probSpec prob.Manifest, options prob.RunOptions, playOptions ...PlayOption) (urth.ResultStatus, []urth.ArtifactSpec, error) {
	var config playConfig
	for _, option := range playOptions {
		option(&config)
	}

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

	logger := NewRunLogger(config.logPublisher)
	slLogger := slog.New(logger) // .Default() // TODO: Add a wrapper .New(logger)

	start := time.Now()
	registry := prometheus.NewRegistry()
	registry.MustRegister(probeSuccessGauge)
	registry.MustRegister(probeDurationGauge)

	slLogger.Info("Beginning probe", "kind", probSpec.Kind) //, "timeout_seconds", options.)
	result, sideEffects, err := probFunc(ctx, probSpec.Spec, options, registry, slLogger)

	duration := time.Since(start).Seconds()
	probeDurationGauge.Set(duration)
	if result == prob.RunFinishedSuccess {
		probeSuccessGauge.Set(1)
		slLogger.Info("Probe succeeded", "duration_seconds", duration)
	} else {
		slLogger.Info("Probe failed", "duration_seconds", duration)
	}

	artifacts := make([]urth.ArtifactSpec, 0, len(sideEffects)+1)
	for _, effect := range sideEffects {
		artifacts = append(artifacts, urth.ArtifactSpec{
			Artifact: effect,
		})
	}

	// Note: a failure to collect metrics must not mask the error reported by the
	// probe itself, which is what `err` carries and what the caller is told about.
	metricsArtifact, metricsErr := ToArtifact(registry, RegistryOptions{DisableCompression: true})
	if metricsErr != nil {
		slLogger.Error("NOTICE: Failed to collect metrics registry", "err", metricsErr)
	} else {
		artifacts = append(artifacts, metricsArtifact)
	}

	artifacts = append(artifacts, logger.ToArtifact())

	return urth.NewRunResults(result), artifacts, err
}
