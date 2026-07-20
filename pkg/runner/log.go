package runner

import (
	"bytes"
	"context"
	"log"
	"log/slog"
	"sync"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
)

const LogRelType = "log"

// runLogSink is the shared destination for a single script run.
// It accumulates everything written so the run can be published as an artifact,
// while teeing to the process log so an operator watching a worker sees the run
// unfold live.
//
// A run may be written to concurrently: probers commonly attach a subprocess'
// stdout and stderr in addition to logging from their own goroutines, so access
// to the buffer is guarded.
type runLogSink struct {
	mu      sync.Mutex
	content bytes.Buffer
}

func (s *runLogSink) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, err = s.content.Write(p)
	log.Writer().Write(p)

	return
}

func (s *runLogSink) snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return bytes.Clone(s.content.Bytes())
}

// RunLogger captures the log of a single script run, so that it can be
// attached to the run results as an artifact.
//
// It implements slog.Handler, and doubles as an io.Writer for probers that
// need to pipe raw subprocess output into the same log.
type RunLogger struct {
	sink    *runLogSink
	handler slog.Handler
}

func NewRunLogger() *RunLogger {
	sink := &runLogSink{}

	return &RunLogger{
		sink:    sink,
		handler: slog.NewTextHandler(sink, nil),
	}
}

func (rl *RunLogger) Enabled(ctx context.Context, level slog.Level) bool {
	return rl.handler.Enabled(ctx, level)
}

func (rl *RunLogger) Handle(ctx context.Context, r slog.Record) error {
	return rl.handler.Handle(ctx, r)
}

// WithAttrs implements slog.Handler, returning a logger that shares the same
// underlying run log.
func (rl *RunLogger) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &RunLogger{
		sink:    rl.sink,
		handler: rl.handler.WithAttrs(attrs),
	}
}

// WithGroup implements slog.Handler, returning a logger that shares the same
// underlying run log.
func (rl *RunLogger) WithGroup(name string) slog.Handler {
	return &RunLogger{
		sink:    rl.sink,
		handler: rl.handler.WithGroup(name),
	}
}

// Write implements io.Writer, appending raw output to the run log.
func (rl *RunLogger) Write(p []byte) (n int, err error) {
	return rl.sink.Write(p)
}

// ToArtifact captures the run log accumulated so far as a run artifact.
func (rl *RunLogger) ToArtifact() urth.ArtifactSpec {
	return urth.ArtifactSpec{
		Artifact: prob.Artifact{
			Rel:      LogRelType,
			MimeType: "text/plain",
			Content:  rl.sink.snapshot(),
		},
	}
}
