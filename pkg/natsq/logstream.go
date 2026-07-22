package natsq

import (
	"fmt"
	"strings"

	"github.com/nats-io/nats.go"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Run logs travel on Core NATS, not JetStream.
//
// A log tail is the textbook transient case: it is worth having while someone
// is watching and worth nothing once they have stopped. Persisting it in
// JetStream would duplicate the log artifact that the worker uploads at the end
// of the run anyway, and would add a durable stream whose only consumer is a
// browser tab that may never open.
//
// The consequence, stated plainly: with no subscriber the server drops these
// messages, so a worker publishing a log nobody is reading wastes its own
// upstream bandwidth. That is the trade for not maintaining a second durable
// stream, and it is why workers can turn streaming off.

// LogSubject returns the subject a worker publishes one run's log lines on.
//
// The runner UID is in the subject, ahead of the result UID, so that a worker's
// publish permission can be scoped to its own runner's prefix. Without it, any
// worker able to publish could inject lines into any other runner's run log,
// and a log an operator cannot trust is worse than no log.
func LogSubject(runnerUID, resultUID manifest.ResourceID) string {
	return fmt.Sprintf("%s.logs.%s.%s", SubjectPrefix, runnerUID, resultUID)
}

// LogSubjectForResult returns the subject pattern matching one run's log lines
// from any runner. The server subscribes with this and checks the publishing
// runner against the Result's recorded executor, because a wildcard subscriber
// cannot rely on the subject alone to tell it who published.
func LogSubjectForResult(resultUID manifest.ResourceID) string {
	return fmt.Sprintf("%s.logs.*.%s", SubjectPrefix, resultUID)
}

// RunnerLogSubjectPrefix returns the subject prefix a runner's workers may
// publish logs on. It is the permission grant that scopes LogSubject.
func RunnerLogSubjectPrefix(runnerUID manifest.ResourceID) string {
	return fmt.Sprintf("%s.logs.%s.*", SubjectPrefix, runnerUID)
}

// RunnerUIDFromLogSubject recovers the publishing runner from a log subject, so
// a subscriber can verify it against the Result's executor.
func RunnerUIDFromLogSubject(subject string) (manifest.ResourceID, bool) {
	// urth.v1.logs.<runner-uid>.<result-uid> -- four fixed tokens then two IDs.
	// Splitting on the separator rather than pattern-matching keeps the token
	// count explicit, so a subject with a missing or extra element is rejected
	// instead of silently yielding a partial UID.
	const (
		runnerTokenIndex = 3
		tokenCount       = 5
	)

	tokens := strings.Split(subject, ".")
	if len(tokens) != tokenCount || tokens[0] != "urth" || tokens[1] != "v1" || tokens[2] != "logs" {
		return "", false
	}

	if tokens[runnerTokenIndex] == "" {
		return "", false
	}

	return manifest.ResourceID(tokens[runnerTokenIndex]), true
}

// LogSubscriber delivers a run's log lines as they are published.
type LogSubscriber struct {
	sub   *nats.Subscription
	lines chan []byte
}

// SubscribeRunLog starts receiving log lines for one run.
//
// runnerUID is the runner the Result records as its executor. Lines published
// by anything else are dropped: the subscription is a wildcard over runners, so
// without this check any worker able to publish could inject lines into any
// run's log, and an operator would have no way to tell.
//
// buffer bounds how far behind a slow reader may fall before lines are dropped.
// Dropping is the right failure here -- a viewer that cannot keep up should
// miss output, not stall the run producing it.
func SubscribeRunLog(conn *nats.Conn, runnerUID, resultUID manifest.ResourceID, buffer int) (*LogSubscriber, error) {
	if buffer <= 0 {
		buffer = 256
	}

	subscriber := &LogSubscriber{lines: make(chan []byte, buffer)}

	sub, err := conn.Subscribe(LogSubjectForResult(resultUID), func(msg *nats.Msg) {
		// The subscription is a wildcard over runners, so the subject alone does
		// not say who published. Check it against the Result's executor.
		if publisher, ok := RunnerUIDFromLogSubject(msg.Subject); !ok || publisher != runnerUID {
			return
		}

		select {
		case subscriber.lines <- msg.Data:
		default:
			// Reader is behind. Drop rather than block: this callback runs on
			// the NATS client's dispatch goroutine, and blocking it would stall
			// every other subscription on the connection.
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to run log: %w", err)
	}

	subscriber.sub = sub

	return subscriber, nil
}

// Lines yields log output as it arrives.
func (s *LogSubscriber) Lines() <-chan []byte {
	return s.lines
}

// Close stops the subscription.
func (s *LogSubscriber) Close() error {
	if s == nil || s.sub == nil {
		return nil
	}

	return s.sub.Unsubscribe()
}

// LogPublisher publishes run log lines for one run.
type LogPublisher struct {
	conn    *nats.Conn
	subject string
}

// NewLogPublisher returns a publisher for one run's log.
func NewLogPublisher(conn *nats.Conn, runnerUID, resultUID manifest.ResourceID) *LogPublisher {
	return &LogPublisher{
		conn:    conn,
		subject: LogSubject(runnerUID, resultUID),
	}
}

// PublishLine sends one chunk of run log output.
//
// Errors are dropped on purpose. This is a best-effort tail running alongside a
// probe; failing a scenario run, or spamming the very log being streamed,
// because nobody was listening would be a poor trade. The authoritative record
// is the log artifact uploaded when the run finishes.
func (p *LogPublisher) PublishLine(line []byte) {
	if p == nil || p.conn == nil {
		return
	}

	_ = p.conn.Publish(p.subject, line)
}
