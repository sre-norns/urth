// Package apiserver composes the Urth control plane: the REST API over the
// resource store, the transport the dispatch loops publish through, and the
// loops themselves.
//
// It is a package rather than a `main` for one reason: nothing could test the
// whole dispatch path while the router, the service and the control loops were
// only reachable by starting a process. Every claim disposition this system
// depends on is expressed as an HTTP status produced by a real handler over a
// real store, and a test that stubs either half asserts the contract it assumed
// rather than the one that ships -- which is exactly how the acknowledgement bug
// task 010 fixed survived for months. See test/integration.
//
// cmd/api-server is the process: flags, a database connection, a listener, and
// a shutdown. Everything else lives here.
package apiserver

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/controllers"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"

	"github.com/gin-gonic/gin"

	// Prober packages are linked for their registration side effects only. The
	// server does not execute probs -- workers do -- but it owns the registry of
	// which kinds exist, and cannot answer that for kinds it has never seen.
	// Without these imports GET /probs reports an empty list rather than a wrong
	// one, which is a quieter failure than it looks.
	//
	// They are imported by this package rather than by the command, because the
	// registry is the package's: a test that builds a Server and a command that
	// builds one must decode a stored prob spec against the same set of types,
	// or the test is exercising the untyped-map fallback the server never sees.
	_ "github.com/sre-norns/urth/pkg/probers/dns"
	_ "github.com/sre-norns/urth/pkg/probers/grpc"
	_ "github.com/sre-norns/urth/pkg/probers/har"
	_ "github.com/sre-norns/urth/pkg/probers/http"
	_ "github.com/sre-norns/urth/pkg/probers/icmp"
	_ "github.com/sre-norns/urth/pkg/probers/puppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/pypuppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/rest"
	_ "github.com/sre-norns/urth/pkg/probers/tcp"
)

// Transport names a job transport this server can be composed with.
const (
	TransportNATS  = "nats"
	TransportAsynq = "asynq"
)

// Config is everything an operator can set about an API server.
//
// The kong tags are the command's flag definitions and are load-bearing: kong
// parses an imported struct exactly as it parsed the one that used to live in
// cmd/api-server, so moving this here does not rename a single flag.
type Config struct {
	dbstore.Config `help:"Persistent storage URL" embed:"" prefix:"store."`

	// Named rather than embedded: both of these are called Config, and
	// embedding a second one collides with dbstore's.
	Signing urth.SigningKeysConfig `embed:"" prefix:"signing."`
	NATS    natsq.Config           `embed:"" prefix:"nats."`

	// Transport selects the job queue. Both implementations are kept while the
	// migration in ADR 0004 proceeds, so an operator can cut over and back
	// without changing binaries.
	Transport string `help:"Job transport to use: nats or asynq" enum:"nats,asynq" default:"asynq"`

	MessageBrokerURL string `help:"Message broker address:port to connect to (asynq transport)" default:"localhost:6379"`

	SessionTTL     time.Duration `help:"How long an issued worker session remains valid" default:"1h"`
	MaxRunDuration time.Duration `help:"Maximum time a worker may hold a run capability" default:"30m"`

	// Worker liveness. The interval is what the server asks workers to report
	// at; the timeout is derived from it unless set, so the two cannot be
	// configured into contradicting each other.
	WorkerHeartbeatInterval time.Duration `name:"worker.heartbeat-interval" help:"How often a worker is asked to report that it is still there" default:"1m"`
	WorkerOfflineAfter      time.Duration `name:"worker.offline-after" help:"How long a liveness signal may go unheard before it counts as offline. Zero derives it from the heartbeat interval" default:"0"`
	WorkerRetention         time.Duration `name:"worker.retention" help:"How long a worker silent on every signal is kept before its registration is dropped" default:"24h"`

	// Control loops are configured by the package that composes them, so that a
	// command hosting them elsewhere offers the same flags rather than a second
	// set that drifted. See ADR 0006.
	Controllers controllers.Config `embed:""`
}

// Models are the tables an API server needs migrated before it can serve.
//
// Exported because migration is the command's step, not this package's: a
// deployment may migrate from a separate job, and a server that silently
// created tables would make that impossible to enforce. The control loops'
// tables are included so a host cannot start with the loops enabled and their
// tables missing.
func Models() []any {
	return append([]any{
		&urth.WorkerInstance{},
		&urth.Runner{},
		&urth.Scenario{},
		&urth.Result{},
		&urth.Artifact{},
		&urth.DispatchFailure{},
	}, controllers.Models()...)
}

// Option adjusts a composed server. Options exist for the seams a test needs and
// a deployment does not -- a decorated publisher is the only way to express
// "the broker accepted this and then the relay died", which is a row of ADR
// 0004's failure table with no production equivalent.
type Option func(*settings)

type settings struct {
	decoratePublisher func(urth.DispatchPublisher) urth.DispatchPublisher
}

// WithPublisherDecorator wraps whatever publisher the transport produced.
//
// The relay is handed the decorated one, so a decorator can publish for real and
// then fail -- the crash point between the broker accepting a message and the
// outbox row being marked. Nothing in production sets this.
func WithPublisherDecorator(fn func(urth.DispatchPublisher) urth.DispatchPublisher) Option {
	return func(s *settings) { s.decoratePublisher = fn }
}

// Server is a composed control plane: a router to serve, loops to run, and the
// connections both hold.
type Server struct {
	// Service is the domain service the router is built over. Exposed because a
	// test driving the API in-process has no reason to go through HTTP for the
	// setup it is not testing.
	Service urth.Service

	// Router serves the REST API. A caller owns the listener.
	Router *gin.Engine

	// Store and DB are the authoritative store, for a caller that needs to
	// assert against rows the API does not expose.
	Store *dbstore.DBStore
	DB    *gorm.DB

	// Publisher is what the relay hands committed outbox entries to, after any
	// decorator.
	Publisher urth.DispatchPublisher

	// Dispatch holds the composed loops. Relay and Reconciler are nil when
	// disabled in this process; when they are not, RunOnce drives a single pass
	// deterministically instead of racing the ticker Start runs.
	Dispatch controllers.Dispatch

	// Loops supervises everything Start runs.
	Loops *controllers.Manager

	// Metrics is the registry the /metrics route serves.
	Metrics *prometheus.Registry

	// Transport is the scheduler side of the chosen transport. Nil is not
	// possible: composition fails rather than producing a server that cannot
	// dispatch.
	scheduler urth.Scheduler

	// natsConn carries run-log streaming, presence, and advisories. Nil on the
	// asynq transport.
	natsConn *nats.Conn

	cfg Config
}

// New composes an API server over an already-open database.
//
// The database is the caller's because it is the one dependency whose lifetime
// is not this server's: a command opens it from flags, a test opens one scoped
// to a private schema, and neither wants the other's connection settings.
func New(ctx context.Context, db *gorm.DB, cfg Config, options ...Option) (*Server, error) {
	var opts settings
	for _, option := range options {
		option(&opts)
	}

	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	if err != nil {
		return nil, fmt.Errorf("failed to open the resource store: %w", err)
	}

	keys, err := cfg.Signing.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare token signing keys: %w", err)
	}

	// Worker liveness is written straight to its columns rather than through the
	// resource store, because recording it is not a resource edit: it happens on
	// a timer forever, and a resource Save would bump metadata.version every
	// interval. See urth.WorkerPresenceStore.
	presence := urth.NewWorkerPresenceStore(db)

	// Built before the service because both need it: placement increments it, and
	// the metrics registry exposes it.
	placementMetrics := urth.NewPlacementMetrics()

	serviceOptions := []urth.ServiceOption{
		urth.WithSigningKeys(keys),
		urth.WithSessionTTL(cfg.SessionTTL),
		urth.WithMaxRunDuration(cfg.MaxRunDuration),
		urth.WithWorkerPresence(presence),
		urth.WithWorkerHeartbeatInterval(cfg.WorkerHeartbeatInterval),
		urth.WithWorkerOfflineAfter(cfg.WorkerOfflineAfter),

		// Placement reads how much work each runner already holds straight from
		// the results table -- see urth.RunnerLoadStore for why this needs no
		// broker round trip.
		urth.WithRunnerLoad(urth.NewRunnerLoadStore(db)),
		urth.WithPlacementCounter(placementMetrics),
	}

	server := &Server{
		Store: store,
		DB:    db,
		cfg:   cfg,
	}

	// publisher is what the relay hands committed outbox entries to. The two
	// transports reach it differently: NATS publishes a dispatch envelope
	// straight from the entry, while asynq needs the whole job and so goes
	// through an adapter that reloads the Result. Both share one durability
	// story, which is the point -- retiring asynq in task 015 deletes the
	// adapter rather than a second way of dispatching.
	var publisher urth.DispatchPublisher

	// channels is the transport's half of reconciliation: restoring a runner's
	// queue and withdrawing a dispatch nothing will claim. It stays nil for the
	// legacy transport, which has neither notion -- that is the honest answer,
	// not a degraded mode, and the reconciler skips those passes rather than
	// pretending it repaired something.
	var channels urth.RunnerChannelReconciler

	// presenceWatcher records the NATS half of worker liveness. Nil for a
	// transport with no broker to be present on, in which case workers report
	// that signal as `unknown` -- the honest answer, and the one that keeps the
	// reconciler from evicting them.
	var presenceWatcher *natsq.PresenceWatcher

	switch cfg.Transport {
	case TransportNATS:
		natsScheduler, nerr := natsq.NewScheduler(ctx, cfg.NATS)
		if nerr != nil {
			return nil, fmt.Errorf("failed to connect to NATS: %w", nerr)
		}
		server.scheduler = natsScheduler
		publisher = natsScheduler
		channels = natsScheduler

		// The NATS scheduler doubles as the transport provider: it already owns
		// the JetStream handle and the naming, so having it answer "where does
		// this runner collect work" keeps one component responsible for the
		// topology.
		serviceOptions = append(serviceOptions,
			urth.WithWorkerTransport(natsScheduler),
			// The same handle answers "who is waiting at this runner's queue",
			// which is the fleet-level cross-check on per-worker presence.
			urth.WithRunnerChannelObserver(natsScheduler),
		)

		// A separate connection for log tailing, so a browser holding a slow
		// stream open cannot interfere with job publication.
		conn, cerr := cfg.NATS.Connect("urth-api-server-logs")
		if cerr != nil {
			_ = server.scheduler.Close()
			return nil, fmt.Errorf("failed to connect to NATS for run log streaming: %w", cerr)
		}
		server.natsConn = conn

		// Worker presence shares that connection. It is a handful of empty
		// messages a minute per worker, and the traffic it competes with is a
		// browser tailing a run.
		presenceWatcher = natsq.NewPresenceWatcher(conn, presence)
	default:
		scheduler, serr := redqueue.NewScheduler(ctx, cfg.MessageBrokerURL)
		if serr != nil {
			return nil, fmt.Errorf("failed to create a scheduler: %w", serr)
		}
		server.scheduler = scheduler
		publisher = urth.NewSchedulerDispatchPublisher(scheduler, urth.NewStoreResultLoader(store))
	}

	if opts.decoratePublisher != nil {
		publisher = opts.decoratePublisher(publisher)
	}
	server.Publisher = publisher

	// Control loops run beside the API by default. Supervising them through a
	// manager is what makes that acceptable: a panic in a repair pass is
	// recovered and the loop restarted rather than taking the whole API server
	// down with it, and both loops stop with the process instead of being
	// hard-killed mid-transaction. See ADR 0006.
	//
	// Loop failures never propagate to the caller. A server that stops accepting
	// Results because NATS is unwell is strictly worse than one that keeps
	// recording them for the relay to publish when NATS returns.
	server.Loops = controllers.NewManager()
	server.Dispatch, err = controllers.Register(server.Loops, cfg.Controllers, controllers.Dependencies{
		DB:        db,
		Store:     store,
		Publisher: publisher,
		Channels:  channels,
		MaxJobAge: cfg.NATS.MaxJobAge,

		WorkerRetention: cfg.WorkerRetention,
		// Reuses the log-streaming connection rather than opening a third: an
		// advisory subscription is idle almost all the time, and the traffic it
		// competes with is a browser tailing a run.
		Advisories: controllers.AdvisoryWatcherFor(server.natsConn, urth.NewAdvisoryRecorder(db, store)),
	})
	if err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("failed to compose the control loops: %w", err)
	}

	// Added directly rather than through controllers.Register: that package
	// composes the *dispatch* loops, and worker liveness is not one of them. It
	// still wants the manager's supervision, so a panic recording presence
	// restarts the watcher instead of taking the API server down.
	if presenceWatcher != nil {
		if err := server.Loops.Add("worker-presence", presenceWatcher); err != nil {
			_ = server.Close()
			return nil, fmt.Errorf("failed to register the worker presence watcher: %w", err)
		}
	}

	server.Service = urth.NewService(store, server.scheduler, serviceOptions...)
	server.Metrics = metricsRegistry(db, server.scheduler, placementMetrics)
	server.Router = Routes(server.Service, server.natsConn, server.Metrics)

	return server, nil
}

// Start runs the control loops until ctx is cancelled. It does not serve HTTP:
// the listener belongs to the caller, which is what lets a test drive the router
// through httptest and a command through http.Server.
func (s *Server) Start(ctx context.Context) {
	s.Loops.Start(ctx)

	if names := s.Loops.Names(); len(names) > 0 {
		log.Printf("control loops running in this process: %v", names)
	} else {
		// Worth saying out loud: every loop disabled on every replica is a
		// deployment where nothing repairs anything, and the symptom is runs that
		// simply never move.
		log.Print("no control loops are running in this process")
	}
}

// Wait blocks until the control loops have stopped, or the timeout elapses.
func (s *Server) Wait(timeout time.Duration) error {
	return s.Loops.Wait(timeout)
}

// Close releases the transport connections this server owns. The database is the
// caller's and is left alone.
func (s *Server) Close() error {
	if s.natsConn != nil {
		// Drain rather than Close: an advisory subscription may be mid-callback,
		// and dropping it loses a dead letter nobody else can report.
		_ = s.natsConn.Drain()
		s.natsConn = nil
	}

	if s.scheduler != nil {
		err := s.scheduler.Close()
		s.scheduler = nil

		return err
	}

	return nil
}

// metricsRegistry assembles what this process can tell an operator about the
// dispatch pipeline.
//
// Two collectors, because the pipeline has two halves that look identical from
// either side alone: the outbox knows what was committed and not yet published,
// and JetStream knows what was published and not yet claimed. A backlog in the
// first with an empty stream is the relay; the same backlog with a full stream
// is the fleet.
//
// A registry of its own rather than prometheus.DefaultRegisterer, so that what
// this endpoint exposes is a decision made here rather than whatever any
// imported package happened to register into the global.
func metricsRegistry(db *gorm.DB, scheduler urth.Scheduler, placement *urth.PlacementMetrics) *prometheus.Registry {
	registry := prometheus.NewRegistry()

	// Process and Go runtime metrics: the baseline any on-call runbook assumes is
	// there, and the thing that says whether the api-server itself is healthy
	// before its own numbers are worth reading.
	registry.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	registry.MustRegister(urth.NewDispatchCollector(db, urth.NewDispatchOutbox(db)))
	registry.MustRegister(placement)

	// Only the routing transport has a stream to report on. The legacy asynq path
	// has no equivalent, and inventing empty gauges for it would read as a queue
	// that is always empty rather than one nobody is measuring.
	if source, ok := scheduler.(natsq.MetricsSource); ok {
		registry.MustRegister(source.Collector())
	}

	return registry
}
