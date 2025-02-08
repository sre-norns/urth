package http

import (
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/go-kit/log"
	bxconfig "github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/blackbox_exporter/prober"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	Kind           = prob.Kind("http")
	ScriptMimeType = "application/yaml"
)

type Spec struct {
	Target string             `json:"target,omitempty" yaml:"target,omitempty"`
	HTTP   bxconfig.HTTPProbe `json:"http" yaml:"http"`
}

func init() {
	moduleVersion := "devel"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = prob.RegisterProbKind(
		Kind,
		&Spec{},
		prob.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		},
	)
}

func RunScript(ctx context.Context, probSpec any, config prob.RunOptions, registry *prometheus.Registry, logger log.Logger) (prob.RunStatus, []prob.Artifact, error) {
	spec, ok := probSpec.(*Spec)
	if !ok {
		return prob.RunFinishedError, nil, fmt.Errorf("%w: got %q, expected %q", manifest.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}

	if spec.Target == "" {
		return prob.RunFinishedError, nil, prob.ErrNoTarget
	}

	if success := prober.ProbeHTTP(ctx, spec.Target, bxconfig.Module{HTTP: spec.HTTP}, registry, logger); !success {
		return prob.RunFinishedFailed, nil, nil
	}

	return prob.RunFinishedSuccess, nil, nil
}
