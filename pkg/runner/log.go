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

// LogPublisher is the shared destination for a single script run.
// It accumulates everything written so the run can be published as an artifact,
// while teeing to the process log so an operator watching a worker sees the run
// unfold live.
//
// A run may be written to concurrently: probers commonly attach a subprocess'
// stdout and stderr in addition to logging from their own goroutines, so access
// to the buffer is guarded.
// LogPublisher receives run log output as it is written, so a run in progress
// can be watched from elsewhere.
//
// PublishLine returns nothing on purpose. Streaming is best-effort: a run must
// not fail, stall, or spam its own log because whatever was watching went away.
type LogPublisher interface {
	PublishLine(line []byte)
}

type runLogSink struct {
	mu sync.Mutex

	content bytes.Buffer

	// publisher tees output to a live subscriber, if any.
	publisher LogPublisher

	// rawWrites records whether anything reached this log other than through a
	// slog record. Probers redact credentials as they build the records they
	// log, but a prober that attaches a subprocess' stdout -- puppeteer pipes
	// node's output straight through -- contributes bytes nobody inspected.
	rawWrites bool
}

// writeRaw records output that passed through no redaction on its way here.
func (s *runLogSink) writeRaw(p []byte) (n int, err error) {
	s.mu.Lock()
	s.rawWrites = true
	s.mu.Unlock()

	return s.Write(p)
}

func (s *runLogSink) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, err = s.content.Write(p)
	log.Writer().Write(p)

	if s.publisher != nil {
		// Published under the lock, so subscribers see lines in the same order
		// the artifact records them. A publisher that blocked here would stall
		// the run, which is why LogPublisher is documented as best-effort and
		// the NATS implementation never blocks.
		//
		// The slice is cloned because the caller may reuse its buffer -- slog's
		// handler does -- and a publisher that queues the bytes would otherwise
		// send whatever the buffer held later.
		s.publisher.PublishLine(bytes.Clone(p))
	}

	return
}

// dataClass reports what the accumulated log may expose. Records logged by a
// prober have had credentials removed as they were assembled, so a log built
// only from those is redacted; any raw passthrough leaves the content
// unaudited, and unknown is the honest answer rather than a guess.
func (s *runLogSink) dataClass() prob.DataClass {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.rawWrites {
		return prob.DataClassUnknown
	}

	return prob.DataClassRedacted
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

// NewRunLogger returns a logger capturing one run's output.
//
// The publisher may be nil, in which case the log is only captured as an
// artifact.
func NewRunLogger(publisher LogPublisher) *RunLogger {
	sink := &runLogSink{publisher: publisher}

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
//
// Output arriving this way -- typically a probed subprocess' stdout or stderr --
// has passed through no redaction, so it downgrades the log's data class. Prefer
// logging a record when the content is the prober's own.
func (rl *RunLogger) Write(p []byte) (n int, err error) {
	return rl.sink.writeRaw(p)
}

// ToArtifact captures the run log accumulated so far as a run artifact.
func (rl *RunLogger) ToArtifact() urth.ArtifactSpec {
	return urth.ArtifactSpec{
		Artifact: prob.Artifact{
			Rel:       LogRelType,
			MimeType:  "text/plain",
			DataClass: rl.sink.dataClass(),
			Content:   rl.sink.snapshot(),
		},
	}
}
