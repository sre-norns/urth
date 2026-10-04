package worker

import (
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExecutionUsesFullProbeBudget(t *testing.T) {
	w := Worker{config: &Config{RunnerConfig: runner.RunnerConfig{Timeout: time.Minute}}}
	auth := urth.AuthJobResponse{Prob: prob.Manifest{Timeout: time.Minute}, Deadline: time.Now().Add(time.Minute)}
	require.Greater(t, w.runTimeout(auth), 59*time.Second, "reporting grace must not remove 15 seconds from execution")
}
