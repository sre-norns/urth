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
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/pages"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"

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

// Config is everything an operator can set about an API server.
//
// The kong tags are the command's flag definitions and are load-bearing: kong
// parses an imported struct exactly as it parsed the one that used to live in
// cmd/api-server, so moving this here does not rename a single flag.
type Config struct {
	// Identity replaces the whole identity configuration for an embedded host or
	// a test. When set, the identity flags below are ignored.
	Identity       *identity.Config `kong:"-"`
	dbstore.Config `help:"Persistent storage URL" embed:"" prefix:"store."`

	// IdentityOptions are the operator's identity settings: the public issuer,
	// sign-in providers and account mail. Environment variables are URTH_ plus
	// the option's own name -- URTH_ISSUER, URTH_MAIL_PROVIDER -- because kong's
	// prefix renames flags only and envprefix alone names the variables. Left
	// zero (a Config built in code), the branded defaults apply unchanged.
	IdentityOptions identity.Options `embed:"" prefix:"identity." envprefix:"URTH_"`
	// WebRedirectURIs are where the authorization server may return the web
	// client. They must match exactly, so a development SPA on another port
	// needs its own entry.
	WebRedirectURIs []string `name:"identity.web-redirect-uri" env:"URTH_WEB_REDIRECT_URI" default:"http://localhost:8080/oauth/callback" help:"OAuth redirect URI of the urth-web client; repeat for several"`
	// PrivacyURL is the privacy notice the sign-in pages link to. Empty hides
	// the link: Urth serves no privacy page of its own.
	PrivacyURL     string   `name:"identity.privacy-url" env:"URTH_PRIVACY_URL" help:"Privacy notice the sign-in pages link to: an https URL or a path that starts with /"`
	TrustedProxies []string `name:"http.trusted-proxy" env:"URTH_TRUSTED_PROXIES" help:"Trusted reverse proxy IP address or CIDR; repeat for several. Empty uses the direct peer address"`

	Bootstrap BootstrapConfig `embed:"" prefix:"bootstrap." envprefix:"URTH_BOOTSTRAP_"`

	// Named rather than embedded: both of these are called Config, and
	// embedding a second one collides with dbstore's.
	Signing urth.SigningKeysConfig `embed:"" prefix:"signing."`
	NATS    natsq.Config           `embed:"" prefix:"nats."`

	// Transport is accepted only so that command lines written while asynq was
	// an alternative keep working. NATS is the only transport.
	Transport string `help:"Job transport. Only nats remains" enum:"nats" default:"nats" hidden:""`

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

// BootstrapConfig provisions a first user, so a new installation can be signed
// into before anyone can invite anyone.
type BootstrapConfig struct {
	Email    string `env:"EMAIL" help:"Create this local user, and an account it owns, if the user does not exist"`
	Password string `env:"PASSWORD" help:"Password of the bootstrap user: 12 to 72 bytes"`
	// SystemAdmin is opt-in because the two are exclusive in identity: a system
	// administrator is provisioned with system authority and no account, and
	// Urth has no system console to land on.
	SystemAdmin bool `name:"system-admin" env:"SYSTEM_ADMIN" help:"Provision a system administrator instead: system authority and no account"`
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
	Service  urth.Service
	Identity *identity.Service

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

	// natsConn carries run-log streaming, presence, and advisories.
	natsConn *nats.Conn

	cfg Config
}

// New composes an API server over an already-open database.
//
// The database is the caller's because it is the one dependency whose lifetime
// is not this server's: a command opens it from flags, a test opens one scoped
// to a private schema, and neither wants the other's connection settings.
func New(ctx context.Context, db *gorm.DB, cfg Config, options ...Option) (*Server, error) {
	if err := gin.New().SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, fmt.Errorf("invalid trusted proxy configuration: %w", err)
	}
	var opts settings
	for _, option := range options {
		option(&opts)
	}

	if err := urth.RegisterIdentity(db); err != nil {
		return nil, err
	}
	if err := identity.Migrate(db); err != nil {
		return nil, err
	}
	identityService := identity.NewService(db)
	identityConfig, err := IdentityConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("invalid identity configuration: %w", err)
	}
	if err := identityService.Configure(identityConfig); err != nil {
		return nil, err
	}
	if err := pages.ValidPrivacyURL(cfg.PrivacyURL); err != nil {
		return nil, err
	}
	if cfg.Bootstrap.Email != "" {
		// Does nothing when the user exists, so every restart may run it; it
		// does not repair an account deleted since.
		if err := identityService.ProvisionUser(ctx, cfg.Bootstrap.Email, cfg.Bootstrap.Password, cfg.Bootstrap.SystemAdmin); err != nil {
			return nil, fmt.Errorf("failed to provision the bootstrap user: %w", err)
		}
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
		urth.WithIdentity(db, identityService),
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
		Store:    store,
		Identity: identityService,
		DB:       db,
		cfg:      cfg,
	}

	natsScheduler, err := natsq.NewScheduler(ctx, cfg.NATS, func(ctx context.Context, uid manifest.ResourceID) (urth.Runner, error) {
		var runner urth.Runner
		err := db.WithContext(ctx).Where("uid = ?", uid).First(&runner).Error
		return runner, err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}
	server.scheduler = natsScheduler

	// publisher is what the relay hands committed outbox entries to: the
	// scheduler publishes a dispatch envelope straight from the entry.
	var publisher urth.DispatchPublisher = natsScheduler

	// channels is the transport's half of reconciliation: restoring a runner's
	// queue and withdrawing a dispatch nothing will claim.
	var channels urth.RunnerChannelReconciler = natsScheduler

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
	conn, err := cfg.NATS.Connect("urth-api-server-logs")
	if err != nil {
		_ = server.scheduler.Close()
		return nil, fmt.Errorf("failed to connect to NATS for run log streaming: %w", err)
	}
	server.natsConn = conn

	// Worker presence shares that connection. It is a handful of empty
	// messages a minute per worker, and the traffic it competes with is a
	// browser tailing a run.
	presenceWatcher := natsq.NewPresenceWatcher(conn, presence)

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
	if err := server.Loops.Add("worker-presence", presenceWatcher); err != nil {
		_ = server.Close()
		return nil, fmt.Errorf("failed to register the worker presence watcher: %w", err)
	}

	// Identity mail: invitations and project-access notices, each delivered by a
	// retrying worker from rows committed with the change. Registered only when a
	// mail provider is configured -- without one both workers return at once,
	// and the manager would restart them forever.
	if identityService.IdentityMailAvailable() {
		for name, run := range map[string]func(context.Context){
			"identity-invitation-mail":     identityService.RunInvitationMailWorker,
			"identity-project-access-mail": identityService.RunProjectAccessMailWorker,
		} {
			if err := server.Loops.Add(name, mailLoop(run)); err != nil {
				_ = server.Close()
				return nil, fmt.Errorf("failed to register the %s worker: %w", name, err)
			}
		}
	}

	server.Service = urth.NewService(store, server.scheduler, serviceOptions...)
	server.Metrics = metricsRegistry(db, server.scheduler, placementMetrics)
	server.Router = Routes(server.Service, server.natsConn, server.Metrics, IdentityRoutes{
		Service: identityService,
		Pages:   SignInPages(cfg.PrivacyURL),
	})
	// Validated before composition, so this cannot fail after resources open.
	_ = server.Router.SetTrustedProxies(cfg.TrustedProxies)

	return server, nil
}

// Start runs the control loops until ctx is cancelled. It does not serve HTTP:
// the listener belongs to the caller, which is what lets a test drive the router
// through httptest and a command through http.Server.
func (s *Server) Start(ctx context.Context) {
	// Control loops operate across tenants; an administrator session never receives this authority.
	s.Loops.Start(identity.WithServicePrincipal(ctx))

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

	// Stream metrics come from the transport when it offers them. A stand-in
	// that has no stream registers nothing, rather than empty gauges that would
	// read as a queue that is always empty instead of one nobody is measuring.
	if source, ok := scheduler.(natsq.MetricsSource); ok {
		registry.MustRegister(source.Collector())
	}

	return registry
}
