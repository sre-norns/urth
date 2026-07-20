package runner

import (
	"bytes"
	stdlog "log"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// Run logs are also echoed to the worker's own log, so that an operator
// watching a worker can follow a run as it happens rather than having to wait
// for the artifact to be uploaded.
func TestRunLoggerEchoesRecordsToProcessLog(t *testing.T) {
	var processLog bytes.Buffer
	restore := stdlog.Writer()
	stdlog.SetOutput(&processLog)
	t.Cleanup(func() { stdlog.SetOutput(restore) })

	logger := NewRunLogger()
	slog.New(logger).Info("probe finished", "kind", "tcp")

	require.Contains(t, processLog.String(), "probe finished")
}

// Records logged through the handler must land in the run artifact: this is the
// only way a run's log reaches the API server.
func TestRunLoggerCapturesRecords(t *testing.T) {
	logger := NewRunLogger()
	slog.New(logger).Info("probe finished", "kind", "tcp")

	require.Contains(t, string(logger.ToArtifact().Content), "probe finished")
	require.Contains(t, string(logger.ToArtifact().Content), "kind=tcp")
}

// Probers attach subprocess output directly, so the logger must also capture
// raw writes.
func TestRunLoggerCapturesRawWrites(t *testing.T) {
	logger := NewRunLogger()

	_, err := logger.Write([]byte("raw subprocess output\n"))
	require.NoError(t, err)

	require.Contains(t, string(logger.ToArtifact().Content), "raw subprocess output")
}

// Derived loggers must write into the same run log, rather than a detached one.
func TestRunLoggerDerivedSharesRunLog(t *testing.T) {
	logger := NewRunLogger()

	slog.New(logger).With("scenario", "checkout").WithGroup("http").Info("request sent")

	content := string(logger.ToArtifact().Content)
	require.Contains(t, content, "request sent")
	require.Contains(t, content, "scenario=checkout")
}

// A prober may log from several goroutines while a subprocess writes to the
// same run log.
func TestRunLoggerConcurrentWrites(t *testing.T) {
	logger := NewRunLogger()
	log := slog.New(logger)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); log.Info("logged record") }()
		go func() { defer wg.Done(); logger.Write([]byte("raw write\n")) }()
	}
	wg.Wait()

	require.NotEmpty(t, logger.ToArtifact().Content)
}
