package prob

import (
	"context"
	"fmt"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrNilRunner = fmt.Errorf("prob run function is nil")
	ErrNoTarget  = fmt.Errorf("empty prob.target value")
)

type ScriptRunFn func(ctx context.Context, spec any, config RunOptions, registry *prometheus.Registry, logger log.Logger) (RunStatus, []Artifact, error)

type ProbRegistration struct {
	// Function to execute a script
	RunFunc ScriptRunFn

	// Sem-version of the prober module loaded
	Version string

	// Mime type of the script.
	ContentType string

	// Types of artifacts this prob is expected to produce
	Produce []string
}

// Registrar of Probing modules
var (
	kindRunnerMap = map[Kind]ProbRegistration{}
)

// Register new kind of prob
func RegisterProbKind(kind Kind, proto any, probInfo ProbRegistration) error {
	if probInfo.RunFunc == nil {
		return ErrNilRunner
	}

	if err := RegisterKind(kind, proto); err != nil {
		return err
	}

	// TODO: Should be return an error?
	kindRunnerMap[kind] = probInfo
	return nil
}

// Unregister given prober kind
func UnregisterProbKind(kind Kind) error {
	UnregisterKind(kind)
	delete(kindRunnerMap, kind)

	return nil
}

// List all registered probers
// Note: function makes a copy of the module list to avoid accidental modification of registration info
func ListProbs() map[Kind]ProbRegistration {
	result := make(map[Kind]ProbRegistration, len(kindRunnerMap))
	for kind, info := range kindRunnerMap {
		result[kind] = info
	}

	return result
}

func FindRunFunc(kind Kind) (ScriptRunFn, bool) {
	result, ok := kindRunnerMap[kind]
	return result.RunFunc, ok
}
