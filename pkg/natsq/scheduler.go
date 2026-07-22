package natsq

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrNoRunner reports that a Result reached dispatch without a runner assigned.
//
// The runner UID is the job's subject, so there is nowhere to publish a Result
// that has not been placed. This is a scheduling bug rather than a transport
// failure, and is worth a distinct error so it reads as one.
var ErrNoRunner = fmt.Errorf("result has no runner assigned")

type scheduler struct {
	conn *nats.Conn
	js   jetstream.JetStream
	cfg  Config

	totalErrors    uint64
	totalScheduled uint64
}

// NewScheduler connects to NATS and provisions the shared jobs stream.
//
// Stream provisioning happens here, at startup, rather than lazily on first
// dispatch: a misconfigured JetStream should stop an API server from coming up,
// not surface later as the first scenario run of the day failing.
func NewScheduler(ctx context.Context, cfg Config) (urth.Scheduler, error) {
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
// The Result must already carry the runner it was placed on: placement is a
// scheduling decision, and this function only routes.
func (s *scheduler) Schedule(ctx context.Context, result urth.Result, scenario urth.Scenario) (urth.RunID, error) {
	runnerUID := result.Status.Executor.RunnerID
	if runnerUID == "" {
		atomic.AddUint64(&s.totalErrors, 1)
		return urth.InvalidRunID, fmt.Errorf("can't schedule job for %q: %w", result.Name, ErrNoRunner)
	}

	// The dispatch ID is derived from the Result's identity and version rather
	// than randomly generated. That makes a republication of the same Result
	// state produce the same ID, so JetStream's duplicate window collapses it
	// and a worker retrying a claim presents a key the API already knows.
	dispatchID := DispatchIDFor(result.UID, result.Version)

	envelope := DispatchEnvelope{
		SchemaVersion: DispatchEnvelopeVersion,
		ResultUID:     result.UID,
		ResultVersion: result.Version,
		ScenarioName:  scenario.Name,
		RunnerUID:     runnerUID,
		DispatchID:    dispatchID,
	}

	data, err := MarshalEnvelope(envelope)
	if err != nil {
		atomic.AddUint64(&s.totalErrors, 1)
		return urth.InvalidRunID, fmt.Errorf("failed to encode dispatch for %q: %w", result.Name, err)
	}

	// Publish synchronously and wait for the storage acknowledgement. Returning
	// before JetStream has persisted the message would let the caller mark the
	// Result scheduled when it may never be delivered.
	if _, err = s.js.Publish(ctx, JobSubject(runnerUID), data, jetstream.WithMsgID(dispatchID)); err != nil {
		atomic.AddUint64(&s.totalErrors, 1)
		return urth.InvalidRunID, fmt.Errorf("failed to publish dispatch for %q: %w", result.Name, err)
	}

	atomic.AddUint64(&s.totalScheduled, 1)
	log.Printf("dispatched %q to runner %q as %v", result.Name, runnerUID, dispatchID)

	return urth.RunID(dispatchID), nil
}

// DispatchIDFor derives the stable dispatch identifier for a Result version.
func DispatchIDFor(uid manifest.ResourceID, version manifest.Version) string {
	return fmt.Sprintf("%v.%v", uid, version)
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
