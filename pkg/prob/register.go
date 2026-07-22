package prob

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	ErrNilRunner = fmt.Errorf("prob run function is nil")
	ErrNoTarget  = fmt.Errorf("empty prob.target value")
)

type ScriptRunFn func(ctx context.Context, spec any, config RunOptions, registry *prometheus.Registry, logger *slog.Logger) (RunStatus, []Artifact, error)

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

// RegisterProbKind registers a new kind of prob.
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

// UnregisterProbKind removes the given prober kind from the registry.
func UnregisterProbKind(kind Kind) error {
	UnregisterKind(kind)
	delete(kindRunnerMap, kind)

	return nil
}

// ListProbs lists all registered probers.
// Note: the function makes a copy of the module list to avoid accidental
// modification of registration info.
func ListProbs() map[Kind]ProbRegistration {
	result := make(map[Kind]ProbRegistration, len(kindRunnerMap))
	maps.Copy(result, kindRunnerMap)

	return result
}

func FindRunFunc(kind Kind) (ScriptRunFn, bool) {
	result, ok := kindRunnerMap[kind]
	return result.RunFunc, ok
}
