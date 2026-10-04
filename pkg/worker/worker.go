// Package worker executes Urth scenarios, taking its jobs from a NATS JetStream
// queue owned by one runner.
//
// It replaced the Redis/asynq prototype worker (ADR 0004). The important
// difference was not the broker. This worker authenticates: it exchanges an
// enrolment secret for a session credential, and every job it claims is
// authorised against that session. The prototype asserted its own identity in
// the request body, which the server had no way to check.
//
// It is a package rather than a `main` so that the claim handshake can be
// exercised against a real API server and a real broker at once -- the
// arrangement that would have caught the acknowledgement bug task 010 fixed.
// cmd/nats-worker is the process around it. See test/integration.
//
// Naming note: pkg/runner, which this package uses, holds *probe execution* and
// not the Runner resource. That is a pre-existing misnomer; the queue this
// worker pulls from belongs to a Runner, while the code that plays a prob lives
// in pkg/runner.
package worker

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/client"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Config is everything an operator can set about a worker.
//
// The kong tags are the command's flag definitions and are load-bearing: kong
// parses an imported struct exactly as it parsed the one that used to live in
// cmd/nats-worker, so moving this here does not rename a single flag.
type Config struct {
	client.APIClientConfig `embed:"" prefix:"client."`
	runner.RunnerConfig    `embed:""`

	NATS natsq.ClientConfig `embed:"" prefix:"nats."`

	// TokenFile reads the enrolment secret from disk rather than a flag.
	//
	// A secret passed as a command-line argument is visible in the process
	// table to every user on the host, and tends to end up in shell history.
	// The file, or the environment variable on APIClientConfig.Token, keeps it
	// out of both.
	TokenFile string `help:"Path to a file holding the runner enrolment token" type:"existingfile"`

	AllowInsecureAPI bool `help:"Allow HTTP enrollment only on loopback for local development" default:"false"`

	IdentityKeyFile string `help:"Persistent Ed25519 worker identity key file" env:"URTH_WORKER_IDENTITY_KEY_FILE"`

	Name manifest.ResourceName `help:"Custom name for this worker" env:"WORKER_NAME"`

	Concurrency int `help:"Maximum number of scenarios to execute at once"`

	APIRegistrationTimeout time.Duration `help:"Maximum time alloted for this worker to register with API server" default:"1m"`

	// HeartbeatInterval is only the starting cadence. The server answers every
	// heartbeat with the interval it wants, and that wins -- the timeout it
	// judges workers by is derived from the same number.
	HeartbeatInterval time.Duration `help:"How often to report that this worker is still alive, until the server says otherwise" default:"1m"`

	// StreamLogs publishes run output live. Worth turning off on a constrained
	// link: with nobody watching, the lines are discarded at the NATS server,
	// so the cost is the worker's own upstream bandwidth.
	StreamLogs bool `help:"Publish run logs live over NATS" default:"true" negatable:""`

	// MetricsAddress exports what this worker knows about its own claim
	// handshake, which is the part no other process can see.
	//
	// Empty by default, and that is the decision rather than an omission: this
	// process runs inside the network segment it is probing, often one chosen
	// for having very little listening on it. Opening a port nobody asked for is
	// not a call this binary makes on an operator's behalf.
	MetricsAddress string `help:"Address to serve Prometheus metrics on, e.g. :9101. Empty serves none" env:"METRICS_ADDRESS"`
}

// NewDefaultConfig returns a config carrying this build's capability labels.
func NewDefaultConfig() Config {
	return Config{
		RunnerConfig: runner.NewDefaultConfig(),
	}
}

// Normalize fills in the defaults that cannot be expressed as a flag default,
// because they depend on the machine rather than on a choice.
//
// Called by New, so a caller composing a Config by hand gets the same worker a
// command-line one does -- a concurrency of zero would otherwise produce a
// worker that fetches nothing and merely looks idle.
func (c *Config) Normalize() {
	if c.IdentityKeyFile == "" {
		dir, err := os.UserConfigDir()
		if err == nil {
			c.IdentityKeyFile = filepath.Join(dir, "urth", "worker.key")
		}
	}
	if c.Concurrency <= 0 {
		c.Concurrency = runtime.NumCPU()
	}
	if c.Name == "" {
		c.Name = runner.GenerateWorkerName()
	}
}

// EnrolmentToken resolves the enrolment secret from its file or the
// flag/environment, preferring the file when both are given.
func (c *Config) EnrolmentToken() (urth.APIToken, error) {
	if c.TokenFile == "" {
		return c.Token, nil
	}

	data, err := os.ReadFile(c.TokenFile)
	if err != nil {
		return "", fmt.Errorf("failed to read token file %q: %w", c.TokenFile, err)
	}

	// Trimmed because a token written with `>` or by an editor picks up a
	// trailing newline, and a bearer token with a newline in it fails in a way
	// that looks like a rejected credential rather than a malformed one.
	return urth.APIToken(strings.TrimSpace(string(data))), nil
}

// ProbeRunner stands in for probe execution.
//
// Everything the claim handshake asserts -- that the acknowledgement is
// confirmed before execution starts, that a duplicate delivery starts no second
// execution, that a worker dying mid-run leaves a lease for the reconciler --
// is a statement about whether and when a probe runs. Asserting that needs a
// stand-in; the alternative is proving the ordering against a real prob, which
// tests the prober rather than the handshake.
type ProbeRunner func(context.Context, natsq.DispatchEnvelope, urth.AuthJobResponse)

// Option adjusts a worker. Options exist for the seams a test needs and a
// deployment does not.
type Option func(*Worker)

// WithProbeRunner replaces probe execution. Nothing in production sets this.
func WithProbeRunner(fn ProbeRunner) Option {
	return func(w *Worker) { w.executeJob = fn }
}

// Worker holds the identity and connections established at startup.
type Worker struct {
	identityKey     ed25519.PrivateKey
	natsCredentials string
	config          *Config
	apiClient       urth.Service
	token           urth.APIToken

	// Guards the session, which the renewal goroutine replaces while job
	// handlers read it.
	mu           sync.RWMutex
	session      urth.APIToken
	sessionUntil time.Time

	runnerMeta manifest.ObjectMeta
	workerMeta manifest.ObjectMeta
	runnerUID  manifest.ResourceID

	conn               *nats.Conn
	consumer           jetstream.Consumer
	transportWorkerUID manifest.ResourceID

	// presence announces this worker on its runner's subject. Built once the
	// NATS connection and the worker's identity both exist.
	presence *natsq.PresencePublisher

	// The two halves of the claim handshake's budget, derived from the bound
	// consumer's AckWait. See handshakeBudget.
	claimBudget time.Duration
	ackBudget   time.Duration

	// inFlight is what stops a redelivered message becoming a second concurrent
	// execution of a run this process already holds.
	inFlight inFlightRuns

	// metrics is nil unless MetricsAddress was given. Every method on it is
	// nil-safe for that reason.
	metrics *workerMetrics

	// executeJob replaces probe execution in tests. Nil in production; see runJob.
	executeJob ProbeRunner
}

// New builds a worker over an API client and an enrolment token.
//
// The client is the caller's rather than built from the config, because a
// process and a test reach the same API differently -- one over a socket named
// by a URL, the other over an in-process listener -- and neither should have to
// fake the other's transport to say so.
func New(cfg *Config, client urth.Service, token urth.APIToken, options ...Option) *Worker {
	cfg.Normalize()

	w := &Worker{
		config:    cfg,
		apiClient: client,
		token:     token,
	}

	for _, option := range options {
		option(w)
	}

	return w
}

// RunnerUID reports the runner this worker registered against. Zero until Run
// has registered.
func (w *Worker) RunnerUID() manifest.ResourceID {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.runnerUID
}

// Meta reports this worker's own identity. Zero until Run has registered.
func (w *Worker) Meta() manifest.ObjectMeta {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.workerMeta
}

func (w *Worker) currentSession() urth.APIToken {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.session
}

// register exchanges the enrolment token for a session and connection details.
func (w *Worker) register(ctx context.Context) (urth.WorkerRegistrationResponse, error) {
	regoCtx, cancel := context.WithTimeout(ctx, w.config.APIRegistrationTimeout)
	defer cancel()

	if w.identityKey == nil {
		key, err := LoadInstallationKey(w.config.IdentityKeyFile)
		if err != nil {
			return urth.WorkerRegistrationResponse{}, err
		}
		w.identityKey = key
	}
	public := w.identityKey.Public().(ed25519.PublicKey)
	spec := urth.WorkerInstanceSpec{
		Proof:        &urth.WorkerProof{PublicKey: base64.RawURLEncoding.EncodeToString(public)},
		Capabilities: w.config.GetCapabilities(regoCtx),
	}
	entry := urth.WorkerInstance{
		ObjectMeta: manifest.ObjectMeta{Name: w.config.Name, Labels: w.config.GetEffectiveLabels()},
		Spec:       spec,
	}.ToManifest()
	// ToManifest omits request proof from resource reads; enrollment adds it explicitly.
	entry.Spec = &spec
	entry.Status = nil
	challenge, err := w.apiClient.Runners().ChallengeWorker(regoCtx, w.token, entry)
	if err != nil {
		return urth.WorkerRegistrationResponse{}, err
	}
	entry, err = urth.SignWorkerChallenge(entry, challenge, w.identityKey)
	if err != nil {
		return urth.WorkerRegistrationResponse{}, err
	}
	registration, err := w.apiClient.Runners().AuthWorker(regoCtx, w.token, entry)
	if err != nil {
		return registration, err
	}

	runnerResource, err := urth.NewRunner(registration.Runner)
	if err != nil {
		return registration, fmt.Errorf("registration returned an unexpected runner: %w", err)
	}

	workerResource, err := urth.NewWorkerInstance(registration.Worker)
	if err != nil {
		return registration, fmt.Errorf("registration returned an unexpected worker identity: %w", err)
	}

	w.mu.Lock()
	w.session = registration.Session
	w.sessionUntil = registration.SessionExpiresAt
	if registration.NATS.Credential.Type == urth.NATSCredentialJWT {
		credentials, err := natsq.WorkerCredentialBytes(registration.NATS.Credential)
		if err != nil {
			w.mu.Unlock()
			return registration, fmt.Errorf("invalid issued broker credential")
		}
		w.natsCredentials = string(credentials)
	}
	if expiry := registration.NATS.Credential.ExpiresAt; !expiry.IsZero() && expiry.Before(w.sessionUntil) {
		w.sessionUntil = expiry
	}
	w.runnerMeta = runnerResource.ObjectMeta
	w.workerMeta = workerResource.ObjectMeta
	w.runnerUID = runnerResource.UID
	w.mu.Unlock()

	return registration, nil
}

// Run registers, binds this runner's queue, and consumes jobs until ctx ends.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.config.ValidateAPITransport(); err != nil {
		return err
	}
	if w.config.MetricsAddress != "" {
		metrics, registry := newWorkerMetrics()
		w.metrics = metrics

		go serveMetrics(ctx, w.config.MetricsAddress, registry)
	}

	registration, err := w.register(ctx)
	if err != nil {
		return fmt.Errorf("failed to register with the API server: %w", err)
	}

	log.Printf("registered as worker %q (%v) of runner %q (%v); session valid until %v",
		w.workerMeta.Name, w.workerMeta.UID, w.runnerMeta.Name, w.runnerMeta.UID, registration.SessionExpiresAt)

	consumer, err := w.connect(ctx, registration.NATS)
	if err != nil {
		return err
	}
	loopCtx, cancelLoops := context.WithCancel(ctx)
	var loops sync.WaitGroup
	loops.Add(2)
	go func() { defer loops.Done(); w.renewSession(loopCtx) }()
	go func() { defer loops.Done(); w.reportPresence(loopCtx) }()
	defer func() {
		cancelLoops()
		loops.Wait()
		if conn := w.currentConnection(); conn != nil {
			_ = conn.Drain()
		}
	}()
	return w.consume(loopCtx, consumer)
}

// connect dials NATS and binds the runner's durable consumer.
func (w *Worker) connect(ctx context.Context, info urth.NATSConnectionInfo) (jetstream.Consumer, error) {
	if info.SchemaVersion != urth.NATSConnectionInfoVersion {
		// Refusing rather than guessing: the fields that matter here are the
		// stream and consumer to bind to, and binding to the wrong one either
		// fails loudly or, worse, drains a queue that is not ours.
		return nil, fmt.Errorf("server offered connection info version %d, this worker understands %d",
			info.SchemaVersion, urth.NATSConnectionInfoVersion)
	}

	cfg := w.config.NATS
	if info.Credential.Type == urth.NATSCredentialNone && !cfg.AllowInsecure {
		return nil, fmt.Errorf("unauthenticated worker broker requires --nats.allow-insecure")
	}
	if len(info.URLs) > 0 {
		// The server's answer wins over local configuration: it knows the
		// topology, and a worker pointed at the wrong cluster by a stale flag
		// would otherwise sit connected to a queue that never fills.
		cfg.URL = strings.Join(info.URLs, ",")
	}
	if info.Credential.Type == urth.NATSCredentialJWT {
		credentials, err := natsq.WorkerCredentialBytes(info.Credential)
		if err != nil {
			return nil, fmt.Errorf("invalid issued broker credential")
		}
		cfg.UserCredentials = string(credentials)
		cfg.CredsFile = ""
	}
	if info.Credential.Type == urth.NATSCredentialFile && info.Credential.Value != "" {
		cfg.CredsFile = info.Credential.Value
	}

	if info.Credential.Type == urth.NATSCredentialJWT {
		cfg.CredentialSource = func() string { w.mu.RLock(); defer w.mu.RUnlock(); return w.natsCredentials }
		cfg.InboxPrefix = info.InboxPrefix
	}
	meta := w.Meta()
	conn, err := cfg.Connect(fmt.Sprintf("urth-worker-%s", meta.Name))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to initialize JetStream: %w", err)
	}

	// Bind, never create. A worker that provisions its own consumer has stepped
	// outside the permission model ADR 0004 sets out, and on a work-queue
	// stream would likely collide with the real one.
	consumer, err := js.Consumer(ctx, info.Stream, info.Consumer)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if ctx.Err() != nil {
		conn.Close()
		return nil, ctx.Err()
	}
	w.mu.Lock()
	w.conn = conn
	w.consumer = consumer
	w.transportWorkerUID = meta.UID
	w.presence = natsq.NewPresencePublisher(conn, w.runnerUID, meta.UID)
	w.mu.Unlock()

	log.Printf("bound consumer %q on stream %q for subject %q",
		info.Consumer, info.Stream, info.Subject)

	return consumer, nil
}

// renewSession re-registers before the session expires.
//
// A session that lapses does not merely stop new work: it makes the worker
// invisible as channel capacity, so its runner looks emptier than it is.
func (w *Worker) renewSession(ctx context.Context) {
	failures := 0
	for {
		w.mu.RLock()
		expiry := w.sessionUntil
		w.mu.RUnlock()
		wait := renewalDelay(time.Until(expiry), failures)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		registration, err := w.register(ctx)
		if err != nil {
			failures++
			// Bound retries after expiry. Claimed runs keep their own capability.
			log.Print("failed to renew worker session; retrying with bounded backoff")
			continue
		}
		failures = 0
		w.mu.RLock()
		boundUID := w.transportWorkerUID
		old := w.conn
		w.mu.RUnlock()
		if boundUID != registration.Worker.Metadata.UID {
			if _, err := w.connect(ctx, registration.NATS); err != nil {
				failures++
				log.Print("worker broker rebind failed after new registration")
				continue
			}
			// Existing runs report over HTTPS with their original capabilities. Broker
			// subscriptions and presence now use the replacement registration's inbox.
			if old != nil {
				old.Close()
			}
			w.announce()
		} else if old != nil {
			if err := old.ForceReconnect(); err != nil {
				log.Print("worker broker reconnect failed after renewal")
			}
		}
		log.Print("worker session renewed")
	}
}
func renewalDelay(remaining time.Duration, failures int) time.Duration {
	if failures == 0 && remaining > 0 {
		delay := remaining * 2 / 3
		if delay < 100*time.Millisecond {
			return 100 * time.Millisecond
		}
		return delay
	}
	delay := time.Second
	for i := 0; i < failures && delay < time.Minute; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		delay = time.Minute
	}
	if remaining > 0 && delay > remaining/3 {
		delay = remaining / 3
	}
	if delay < 100*time.Millisecond {
		delay = 100 * time.Millisecond
	}
	return delay
}

// reportPresence tells the control plane this worker is still here, over both
// paths it has.
//
// The two reports are made independently and neither is conditional on the
// other, which is the entire point. A worker reaches Urth over HTTPS to the API
// server and over NATS to its queue; either can fail alone, and which one failed
// is the diagnosis. Skipping the announcement when the heartbeat failed -- the
// obvious economy -- would make a worker that has lost only its API route
// indistinguishable from one that has gone away, which is exactly the case this
// exists to tell apart.
//
// Failures are logged and survived, like session renewal: a worker that cannot
// report is still a worker that can run probes, and the control plane draws its
// own conclusions from the silence.
func (w *Worker) reportPresence(ctx context.Context) {
	interval := w.config.HeartbeatInterval
	if interval < urth.MinWorkerHeartbeatInterval {
		interval = urth.MinWorkerHeartbeatInterval
	}

	// Announce once immediately so a freshly started worker shows as present
	// without waiting out an interval first.
	w.announce()
	interval = w.heartbeat(ctx, interval, false)

	for {
		select {
		case <-ctx.Done():
			w.leave()
			return
		case <-time.After(interval):
		}

		w.announce()
		interval = w.heartbeat(ctx, interval, false)
	}
}

// announce publishes this worker's presence on its runner's subject.
func (w *Worker) announce() {
	w.mu.RLock()
	presence := w.presence
	w.mu.RUnlock()
	if presence == nil {
		return
	}
	if err := presence.Announce(); err != nil {
		log.Printf("failed to announce presence over NATS: %v", err)
	}
}

// heartbeat reports to the API server and returns the interval to wait next.
//
// The server owns the cadence: it is the same number the offline timeout is
// derived from, so a worker choosing its own could be declared dead while
// reporting exactly as often as it intended. The value is floored so that a
// misconfigured server cannot turn this into a busy loop.
func (w *Worker) heartbeat(ctx context.Context, current time.Duration, leaving bool) time.Duration {
	response, err := w.apiClient.Workers().Heartbeat(ctx, w.currentSession(), urth.WorkerHeartbeatRequest{Leaving: leaving})
	if err != nil {
		log.Printf("failed to report worker heartbeat: %v", err)
		return current
	}

	if response.Paused {
		// Said out loud because the symptom otherwise is claims that are refused
		// for no visible reason. The server enforces the pause regardless; this
		// only makes the worker's own log explain itself.
		log.Print("this worker is paused by an operator and will not be given work")
	}

	if response.Interval >= urth.MinWorkerHeartbeatInterval {
		return response.Interval
	}

	return current
}

// leave makes a final report so the fleet view updates at once.
//
// A courtesy, never a guarantee -- a worker that is killed outright, panics, or
// loses its link says nothing, and the offline timeout is what covers those. It
// is worth doing because a clean stop is the common case, and waiting out a
// timeout to reflect one makes the fleet view look broken.
func (w *Worker) leave() {
	// Rooted at Background rather than the worker's context, which is already
	// cancelled: this request exists precisely because the process is going away.
	leaveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	w.heartbeat(leaveCtx, 0, true)
}

// ValidateAPITransport protects machine credentials and proof/session exchanges.
func (c Config) ValidateAPITransport() error {
	parsed, err := url.Parse(c.APIServerAddress)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("configure a valid worker API URL without embedded credentials")
	}
	if parsed.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(parsed.Hostname())
	if c.AllowInsecureAPI && parsed.Scheme == "http" && (parsed.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return fmt.Errorf("worker enrollment requires HTTPS; HTTP loopback development requires --allow-insecure-api")
}

func (w *Worker) currentConnection() *nats.Conn { w.mu.RLock(); defer w.mu.RUnlock(); return w.conn }
func (w *Worker) executionMetadata() (manifest.ObjectMeta, manifest.ObjectMeta) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.runnerMeta, w.workerMeta
}
