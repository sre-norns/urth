package runner

import (
	"bytes"
	stdlog "log"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/prob"
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

// A log built only from records logged by a prober carries the redaction those
// probers applied as they assembled them.
func TestRunLoggerClassifiesRecordsAsRedacted(t *testing.T) {
	logger := NewRunLogger()
	slog.New(logger).Info("probe finished", "kind", "rest")

	require.Equal(t, prob.DataClassRedacted, logger.ToArtifact().DataClass)
	require.False(t, logger.ToArtifact().DataClass.MayContainSecrets())
}

// Raw output attached by a prober -- puppeteer pipes node's stdout straight
// through -- passed through no redaction, so the log can no longer claim to be
// redacted.
func TestRunLoggerRawWritesDowngradeDataClass(t *testing.T) {
	logger := NewRunLogger()
	slog.New(logger).Info("probe started")

	require.Equal(t, prob.DataClassRedacted, logger.ToArtifact().DataClass)

	_, err := logger.Write([]byte("token=leaked-by-a-subprocess\n"))
	require.NoError(t, err)

	require.Equal(t, prob.DataClassUnknown, logger.ToArtifact().DataClass)
	require.True(t, logger.ToArtifact().DataClass.MayContainSecrets(),
		"unaudited output must not be reported as safe")
}

// The downgrade must survive derived loggers, which share the same run log.
func TestRunLoggerDerivedRawWriteDowngradesSharedLog(t *testing.T) {
	logger := NewRunLogger()
	derived, ok := logger.WithAttrs([]slog.Attr{slog.String("scenario", "checkout")}).(*RunLogger)
	require.True(t, ok)

	_, err := derived.Write([]byte("raw output\n"))
	require.NoError(t, err)

	require.Equal(t, prob.DataClassUnknown, logger.ToArtifact().DataClass)
}
