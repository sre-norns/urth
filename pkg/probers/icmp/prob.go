package icmp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"

	bxconfig "github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/blackbox_exporter/prober"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	Kind           = prob.Kind("icmp")
	ScriptMimeType = "application/yaml"
)

type Spec struct {
	Target string             `json:"target,omitempty" yaml:"target,omitempty"`
	ICMP   bxconfig.ICMPProbe `json:"icmp,omitempty" yaml:"icmp,omitempty"`
}

func init() {
	moduleVersion := prob.BuildVersion()

	// Ignore double registration error
	_ = prob.RegisterProbKind(
		Kind,
		&Spec{},
		prob.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
			Privileges:  privileges,
		},
	)
}

func RunScript(ctx context.Context, probSpec any, config prob.RunOptions, registry *prometheus.Registry, logger *slog.Logger) (prob.RunStatus, []prob.Artifact, error) {
	spec, ok := probSpec.(*Spec)
	if !ok {
		return prob.RunFinishedError, nil, fmt.Errorf("%w: got %q, expected %q", manifest.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}

	if spec.Target == "" {
		return prob.RunFinishedError, nil, prob.ErrNoTarget
	}

	if success := prober.ProbeICMP(ctx, spec.Target, bxconfig.Module{ICMP: spec.ICMP}, registry, logger); !success {
		return prob.RunFinishedFailed, nil, nil
	}

	return prob.RunFinishedSuccess, nil, nil
}

// privileges reports raw sockets for don't-fragment probes only. Setting an IP
// header option is impossible on an unprivileged ping socket, so the blackbox
// prober opens a raw socket for those; every other ICMP probe tries a ping
// socket first.
func privileges(spec any) ([]string, error) {
	var typed Spec
	switch s := spec.(type) {
	case *Spec:
		if s == nil {
			return nil, nil
		}
		typed = *s
	case Spec:
		typed = s
	default:
		data, err := json.Marshal(spec)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &typed); err != nil {
			return nil, fmt.Errorf("cannot classify icmp spec: %w", err)
		}
	}
	if typed.ICMP.DontFragment {
		return []string{prob.PrivilegeRawSockets}, nil
	}
	return nil, nil
}
