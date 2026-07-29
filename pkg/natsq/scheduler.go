package natsq

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrNoRunner reports that a Result reached dispatch without a runner assigned.
//
// The runner UID is the job's subject, so there is nowhere to publish a Result
// that has not been placed. This is a placement outcome rather than a transport
// failure, and is worth a distinct error so it reads as one.
//
// It is the domain's own sentinel rather than a private one: the relay decides
// what to do about a permanently undeliverable dispatch, and "nothing could take
// this run" is the case it must not treat as a fault. Kept under this name
// because the transport is where a caller meets it.
var ErrNoRunner = urth.ErrDispatchUnplaced

// Transport is everything the API server needs from the NATS backbone: it
// publishes relayed dispatches, tells a registering worker where to collect
// work, repairs the assets it owns, and still satisfies the legacy Scheduler the
// composition takes.
type Transport interface {
	urth.Scheduler
	urth.DispatchPublisher
	urth.WorkerTransportProvider
	urth.RunnerChannelReconciler
	urth.RunnerChannelObserver
}

type scheduler struct {
	conn *nats.Conn
	js   jetstream.JetStream
	cfg  Config

	totalErrors    atomic.Uint64
	totalScheduled atomic.Uint64
}

// NewScheduler connects to NATS and provisions the shared jobs stream.
//
// Stream provisioning happens here, at startup, rather than lazily on first
// dispatch: a misconfigured JetStream should stop an API server from coming up,
// not surface later as the first scenario run of the day failing.
func NewScheduler(ctx context.Context, cfg Config) (Transport, error) {
	conn, err := cfg.Connect("urth-api-server")
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to initialize JetStream: %w", err)
	}

	if _, err := EnsureJobStream(ctx, js, cfg); err != nil {
		conn.Close()
		return nil, err
	}

	return &scheduler{conn: conn, js: js, cfg: cfg}, nil
}

// PublishStats implements PublishCounters.
//
// The counters were previously incremented and never read, which made them a
// cost with no benefit: "how much has this process published, and how much of it
// failed" is exactly the question asked when a queue looks wrong, and it was
// answerable only with a debugger.
func (s *scheduler) PublishStats() (published, failed uint64) {
	return s.totalScheduled.Load(), s.totalErrors.Load()
}

func (s *scheduler) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}

	// Drain rather than Close: an in-flight publish has a Result already
	// committed behind it, and dropping it here would strand that Result until
	// the reconciler notices.
	return s.conn.Drain()
}

// Schedule publishes a dispatch envelope for a pending Result.
//
// It exists to satisfy urth.Scheduler, which the API server composition still
// takes. The durable path no longer runs through here: a Result commits its
// outbox entry in its own transaction and the relay calls PublishDispatch. This
// method is the same publication expressed against a Result the caller already
// holds, and is kept so that a direct dispatch -- a test, an operator tool --
// produces exactly the message the relay would.
func (s *scheduler) Schedule(ctx context.Context, result urth.Result) (urth.RunID, error) {
	entry := urth.NewDispatchOutboxEntry(result, time.Now())

	if _, err := s.PublishDispatch(ctx, entry); err != nil {
		return urth.InvalidRunID, fmt.Errorf("can't schedule job for %q: %w", result.Name, err)
	}

	log.Printf("dispatched %q to runner %q as %v", result.Name, entry.RunnerUID, entry.EventUID)

	return urth.RunID(entry.EventUID), nil
}

// DispatchIDFor derives the stable dispatch identifier for a Result version.
//
// Deprecated: the identifier is now minted once, when the outbox entry is
// written, and carried on the entry. Use urth.DispatchEventUID to derive it and
// urth.DispatchOutboxEntry.EventUID to read the one actually in use.
func DispatchIDFor(uid manifest.ResourceID, version manifest.Version) string {
	return urth.DispatchEventUID(uid, version)
}

// ConnectionInfoFor implements urth.WorkerTransportProvider.
//
// Provisioning the runner's consumer happens here rather than only when a
// runner resource is created, because the consumer is what makes a queue exist:
// a runner that predates this transport, or whose consumer an operator removed,
// would otherwise have jobs published to a subject nothing is bound to. Calling
// it on every registration is cheap and idempotent.
func (s *scheduler) ConnectionInfoFor(ctx context.Context, runnerUID manifest.ResourceID) (urth.NATSConnectionInfo, error) {
	if _, err := EnsureRunnerConsumer(ctx, s.js, s.cfg, runnerUID); err != nil {
		return urth.NATSConnectionInfo{}, err
	}

	credential := urth.NATSCredential{Type: urth.NATSCredentialNone}
	if s.cfg.CredsFile != "" {
		// The worker is told to use a credentials file it already has. Urth is
		// not yet an issuer of NATS identities -- ADR 0004 leaves the choice
		// between Auth Callout and minted NKey/JWT open -- so this is the
		// operator's provisioning, surfaced through the same field that a
		// minted credential will eventually use.
		credential = urth.NATSCredential{
			Type:  urth.NATSCredentialFile,
			Value: s.cfg.CredsFile,
		}
	}

	return urth.NATSConnectionInfo{
		SchemaVersion:    urth.NATSConnectionInfoVersion,
		URLs:             strings.Split(s.cfg.URL, ","),
		Stream:           JobsStreamName,
		Consumer:         RunnerConsumerName(runnerUID),
		Subject:          JobSubject(runnerUID),
		LogSubjectPrefix: RunnerLogSubjectPrefix(runnerUID),
		Credential:       credential,
	}, nil
}
