package runner

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/prob"
)

type stubSpec struct {
	Target string `json:"target"`
}

// registerStubProb registers a prob kind that reports the given outcome, and
// removes it again when the test ends.
func registerStubProb(t *testing.T, kind prob.Kind, status prob.RunStatus, runErr error) {
	t.Helper()

	err := prob.RegisterProbKind(kind, &stubSpec{}, prob.ProbRegistration{
		RunFunc: func(context.Context, any, prob.RunOptions, *prometheus.Registry, *slog.Logger) (prob.RunStatus, []prob.Artifact, error) {
			return status, nil, runErr
		},
	})
	require.NoError(t, err)

	t.Cleanup(func() { prob.UnregisterProbKind(kind) })
}

// A probe that fails must report its own error to the caller. Play also collects
// a metrics artifact after running the probe, and that step must not be allowed
// to overwrite the probe's error.
func TestPlayReturnsProbeError(t *testing.T) {
	probeErr := errors.New("connection refused")
	registerStubProb(t, "stub-failing", prob.RunFinishedError, probeErr)

	result, artifacts, err := Play(context.Background(), prob.Manifest{
		Kind: "stub-failing",
		Spec: &stubSpec{Target: "localhost:1"},
	}, prob.RunOptions{})

	require.ErrorIs(t, err, probeErr)
	require.Equal(t, prob.RunFinishedError, result.Result)
	require.NotEmpty(t, artifacts, "a failed run should still produce its log artifact")
}

func TestPlayReturnsNoErrorOnSuccess(t *testing.T) {
	registerStubProb(t, "stub-ok", prob.RunFinishedSuccess, nil)

	result, artifacts, err := Play(context.Background(), prob.Manifest{
		Kind: "stub-ok",
		Spec: &stubSpec{Target: "localhost:1"},
	}, prob.RunOptions{})

	require.NoError(t, err)
	require.Equal(t, prob.RunFinishedSuccess, result.Result)
	require.NotEmpty(t, artifacts)
}

func TestPlayRejectsUnknownKind(t *testing.T) {
	_, _, err := Play(context.Background(), prob.Manifest{
		Kind: "no-such-prob-kind",
		Spec: &stubSpec{},
	}, prob.RunOptions{})

	require.Error(t, err)
}

func TestPlayRejectsNilSpec(t *testing.T) {
	_, _, err := Play(context.Background(), prob.Manifest{Kind: "stub-ok"}, prob.RunOptions{})

	require.Error(t, err)
}
