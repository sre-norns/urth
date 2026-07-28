package controllers

import (
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"gorm.io/gorm"
)

// Config selects and tunes the dispatch control loops.
//
// It carries kong tags so that every command hosting these loops offers the same
// flags with the same defaults, which is the point of ADR 0006 §2: an operator
// who moves the loops out of the api-server should not find that the knobs
// changed names on the way.
type Config struct {
	// Relay settings. The relay runs inside every api-server replica by default:
	// ADR 0004 allows it as a separate process, but a deployment where nobody
	// remembered to start one is a deployment where every run sits pending, so
	// the safe default is that any process that can create a Result can also
	// dispatch it. Competing relays are expected and handled by the row lease.
	RelayEnabled      bool          `help:"Run the dispatch outbox relay in this process" default:"true" negatable:""`
	RelayPollInterval time.Duration `help:"How often the dispatch relay polls the outbox when idle" default:"250ms"`
	RelayBatchSize    int           `help:"How many outbox entries the relay leases per poll" default:"32"`
	RelayLease        time.Duration `help:"How long a relay's claim on an outbox entry survives" default:"30s"`

	// Reconciler settings. Runs in every replica for the same reason the relay
	// does -- a deployment where nobody started one is a deployment where an
	// abandoned run stays `running` forever -- and competing scans are settled by
	// a lease row plus the version guard on every transition.
	ReconcileEnabled   bool          `help:"Run the dispatch/execution reconciler in this process" default:"true" negatable:""`
	ReconcileInterval  time.Duration `help:"How often the reconciler scans for drift between Results and the transport" default:"1m"`
	ReconcileLease     time.Duration `help:"How long one reconciler holds the right to scan" default:"5m"`
	ReconcileBatchSize int           `help:"How many inconsistencies one reconciler scan repairs" default:"256"`

	// PendingDispatchGrace is added to the transport's own message expiry to
	// decide when a published dispatch is presumed lost. Expiring a pending run
	// while its message is still claimable would terminate live work, so the
	// margin is deliberately generous.
	PendingDispatchGrace time.Duration `help:"How long past the transport's job expiry a pending run waits before it is expired" default:"30m"`

	// Advisory watcher settings. The broker abandoning a message is the one
	// dead-letter category no worker can report -- by the time the transport
	// gives up, the workers that failed to claim it have long since moved on.
	AdvisoriesEnabled bool `help:"Record dispatches the broker has stopped redelivering" default:"true" negatable:""`

	// ShutdownTimeout bounds how long a command waits for its loops to stop.
	ShutdownTimeout time.Duration `help:"How long to wait for control loops to stop during shutdown" default:"15s"`
}

// Dependencies are what the dispatch loops need from their host's composition.
//
// The transport halves are interfaces owned by pkg/urth, so this package depends
// on no broker: a command wires the concrete transport it chose and passes the
// result through here.
type Dependencies struct {
	// DB and Store are the authoritative store. The reconciler needs both: whole
	// table questions ("which runs have outlived their lease") that only gorm can
	// express, and the resource store for the transitions it makes.
	DB    *gorm.DB
	Store *dbstore.DBStore

	// Publisher is what the relay hands committed outbox entries to.
	Publisher urth.DispatchPublisher

	// Channels is the transport's half of reconciliation. It may be nil for a
	// transport that has no notion of a runner queue or a withdrawable message;
	// the reconciler skips those passes rather than pretending it repaired
	// something.
	Channels urth.RunnerChannelReconciler

	// MaxJobAge is how long the transport itself will hold an unclaimed job. The
	// pending-dispatch timeout is derived from it rather than configured
	// independently, because a pending run must be given longer than the broker
	// will keep its message or the reconciler expires runs that are still queued
	// and claimable.
	MaxJobAge time.Duration

	// Advisories watches for dispatches the transport has abandoned. Nil for a
	// transport with no such notion, in which case that loop is not registered.
	Advisories Loop
}

// Dispatch is what Register built, for a host that needs to reach a loop after
// starting it.
type Dispatch struct {
	// Relay is nil when relaying is disabled in this process.
	Relay *urth.DispatchRelay

	// Reconciler is nil when reconciliation is disabled in this process.
	//
	// Its Status() is per-process and reports a skipped scan whenever another
	// replica held the lease, so it diagnoses one process rather than answering
	// "is anything reconciling". That question is answered from the lease row --
	// see urth.ReconcileStore and ADR 0006 §5.
	Reconciler *urth.Reconciler

	// Advisories reports whether the abandoned-dispatch watcher was registered.
	Advisories bool
}

// Register builds the enabled dispatch loops and adds them to a manager.
//
// Nothing is started here: a command registers every loop it wants, then starts
// the manager once, so that a failure while composing does not leave half a
// control plane running.
func Register(manager *Manager, cfg Config, deps Dependencies) (Dispatch, error) {
	var dispatch Dispatch

	if cfg.RelayEnabled {
		dispatch.Relay = urth.NewDispatchRelay(urth.NewDispatchOutbox(deps.DB), deps.Publisher,
			urth.WithRelayPollInterval(cfg.RelayPollInterval),
			urth.WithRelayBatchSize(cfg.RelayBatchSize),
			urth.WithRelayLease(cfg.RelayLease),
		)

		if err := manager.Add("dispatch-relay", dispatch.Relay); err != nil {
			return dispatch, err
		}
	}

	if cfg.ReconcileEnabled {
		dispatch.Reconciler = urth.NewReconciler(urth.NewReconcileStore(deps.DB, deps.Store),
			urth.WithReconcileInterval(cfg.ReconcileInterval),
			urth.WithReconcileLease(cfg.ReconcileLease),
			urth.WithReconcileBatchSize(cfg.ReconcileBatchSize),
			urth.WithPendingDispatchTimeout(deps.MaxJobAge+cfg.PendingDispatchGrace),
			urth.WithRunnerChannels(deps.Channels),
		)

		if err := manager.Add("dispatch-reconciler", dispatch.Reconciler); err != nil {
			return dispatch, err
		}
	}

	if cfg.AdvisoriesEnabled && deps.Advisories != nil {
		// Safe in every replica without a lease: recording a dead letter is
		// idempotent by dispatch and reason, so every replica that sees the same
		// advisory converges on one record. Which is just as well, because
		// advisories are at-most-once and a single designated listener would be
		// a single point at which they are missed.
		if err := manager.Add("dispatch-advisories", deps.Advisories); err != nil {
			return dispatch, err
		}

		dispatch.Advisories = true
	}

	return dispatch, nil
}

// Models are the tables the dispatch loops own, for a command's migration step.
//
// Listed here so that a command hosting these loops cannot start with the loops
// enabled and their tables missing -- the failure would otherwise appear at the
// first scan, as a query error against a table nobody noticed was absent.
func Models() []any {
	return []any{
		&urth.DispatchOutboxEntry{},
		&urth.ReconcileLease{},
	}
}

// AdvisoryWatcherFor builds the abandoned-dispatch watcher, when the transport
// and connection support one.
//
// Kept here rather than in the command so that the "which transport can do this"
// question has one answer, and a second command hosting these loops inherits it.
func AdvisoryWatcherFor(conn *nats.Conn, sink urth.DispatchAdvisorySink) Loop {
	if conn == nil || sink == nil {
		return nil
	}

	return natsq.NewAdvisoryWatcher(conn, sink)
}
