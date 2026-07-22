// Command nats-worker executes Urth scenarios, taking its jobs from a NATS
// JetStream queue owned by one runner.
//
// It exists alongside cmd/asynq-runner rather than replacing it: ADR 0004
// treats the Redis/asynq transport as the prototype and this as the target, and
// both need to run during the migration.
//
// The important difference is not the broker. This worker authenticates: it
// exchanges an enrolment secret for a session credential, and every job it
// claims is authorised against that session. The asynq worker asserts its own
// identity in the request body, which the server has no way to check.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alecthomas/kong"
	_ "github.com/joho/godotenv/autoload"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type workerConfig struct {
	urth.APIClientConfig `embed:"" prefix:"client."`
	runner.RunnerConfig  `embed:""`

	NATS natsq.ClientConfig `embed:"" prefix:"nats."`

	// TokenFile reads the enrolment secret from disk rather than a flag.
	//
	// A secret passed as a command-line argument is visible in the process
	// table to every user on the host, and tends to end up in shell history.
	// The file, or the environment variable on APIClientConfig.Token, keeps it
	// out of both.
	TokenFile string `help:"Path to a file holding the runner enrolment token" type:"existingfile"`

	Name manifest.ResourceName `help:"Custom name for this worker" env:"WORKER_NAME"`

	Concurrency int `help:"Maximum number of scenarios to execute at once"`

	APIRegistrationTimeout time.Duration `help:"Maximum time alloted for this worker to register with API server" default:"1m"`

	// StreamLogs publishes run output live. Worth turning off on a constrained
	// link: with nobody watching, the lines are discarded at the NATS server,
	// so the cost is the worker's own upstream bandwidth.
	StreamLogs bool `help:"Publish run logs live over NATS" default:"true" negatable:""`
}

// enrolmentToken resolves the enrolment secret from its file or the
// flag/environment, preferring the file when both are given.
func (c *workerConfig) enrolmentToken() (urth.APIToken, error) {
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

var appConfig = workerConfig{
	RunnerConfig: runner.NewDefaultConfig(),
}

func main() {
	log.SetFlags(0)

	kong.Parse(&appConfig,
		kong.Name("nats-worker"),
		kong.Description("Urth worker: claims scenarios from its runner's queue and executes them"),
	)

	if appConfig.Concurrency <= 0 {
		appConfig.Concurrency = runtime.NumCPU()
	}
	if appConfig.Name == "" {
		appConfig.Name = runner.GenerateWorkerName()
	}

	token, err := appConfig.enrolmentToken()
	grace.SuccessRequired(err, "failed to read enrolment token")
	if token == "" {
		grace.SuccessRequired(errors.New("no enrolment token provided"),
			"an enrolment token is required: pass --token, --token-file, or set the token in the environment")
	}

	apiClient, err := appConfig.NewClient()
	grace.SuccessRequired(err, "failed to initialize API client")

	// Signal-aware context, so a shutdown request drains in-flight runs rather
	// than abandoning results the server is waiting for.
	ctx := grace.NewSignalHandlingContext()

	w := &worker{
		config:    &appConfig,
		apiClient: apiClient,
		token:     token,
	}

	grace.SuccessRequired(w.run(ctx), "worker terminated")
}

// worker holds the identity and connections established at startup.
type worker struct {
	config    *workerConfig
	apiClient urth.Service
	token     urth.APIToken

	// Guards the session, which the renewal goroutine replaces while job
	// handlers read it.
	mu           sync.RWMutex
	session      urth.APIToken
	sessionUntil time.Time

	runnerMeta manifest.ObjectMeta
	workerMeta manifest.ObjectMeta
	runnerUID  manifest.ResourceID

	conn *nats.Conn
}

func (w *worker) currentSession() urth.APIToken {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.session
}

// register exchanges the enrolment token for a session and connection details.
func (w *worker) register(ctx context.Context) (urth.WorkerRegistrationResponse, error) {
	regoCtx, cancel := context.WithTimeout(ctx, w.config.APIRegistrationTimeout)
	defer cancel()

	registration, err := w.apiClient.Runners().AuthWorker(regoCtx, w.token,
		urth.WorkerInstance{
			ObjectMeta: manifest.ObjectMeta{
				Name: w.config.Name,
				// The worker's capabilities: which probs it can run, what OS and
				// architecture it is on. The server stores this snapshot and
				// admits or refuses the worker against the runner's
				// requirements -- it is not re-read from later requests.
				Labels: w.config.GetEffectiveLabels(),
			},
		}.ToManifest())
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
	w.runnerMeta = runnerResource.ObjectMeta
	w.workerMeta = workerResource.ObjectMeta
	w.runnerUID = runnerResource.UID
	w.mu.Unlock()

	return registration, nil
}

func (w *worker) run(ctx context.Context) error {
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
	defer w.conn.Drain()

	go w.renewSession(ctx)

	return w.consume(ctx, consumer)
}

// connect dials NATS and binds the runner's durable consumer.
func (w *worker) connect(ctx context.Context, info urth.NATSConnectionInfo) (jetstream.Consumer, error) {
	if info.SchemaVersion != urth.NATSConnectionInfoVersion {
		// Refusing rather than guessing: the fields that matter here are the
		// stream and consumer to bind to, and binding to the wrong one either
		// fails loudly or, worse, drains a queue that is not ours.
		return nil, fmt.Errorf("server offered connection info version %d, this worker understands %d",
			info.SchemaVersion, urth.NATSConnectionInfoVersion)
	}

	cfg := w.config.NATS
	if len(info.URLs) > 0 {
		// The server's answer wins over local configuration: it knows the
		// topology, and a worker pointed at the wrong cluster by a stale flag
		// would otherwise sit connected to a queue that never fills.
		cfg.URL = strings.Join(info.URLs, ",")
	}
	if info.Credential.Type == urth.NATSCredentialFile && info.Credential.Value != "" {
		cfg.CredsFile = info.Credential.Value
	}

	conn, err := cfg.Connect(fmt.Sprintf("urth-worker-%s", w.workerMeta.Name))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}
	w.conn = conn

	js, err := jetstream.New(conn)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize JetStream: %w", err)
	}

	// Bind, never create. A worker that provisions its own consumer has stepped
	// outside the permission model ADR 0004 sets out, and on a work-queue
	// stream would likely collide with the real one.
	consumer, err := natsq.BindRunnerConsumer(ctx, js, w.runnerUID)
	if err != nil {
		return nil, err
	}

	log.Printf("bound consumer %q on stream %q for subject %q",
		info.Consumer, info.Stream, info.Subject)

	return consumer, nil
}

// renewSession re-registers before the session expires.
//
// A session that lapses does not merely stop new work: it makes the worker
// invisible as channel capacity, so its runner looks emptier than it is.
func (w *worker) renewSession(ctx context.Context) {
	for {
		w.mu.RLock()
		expiry := w.sessionUntil
		w.mu.RUnlock()

		// Renew at two thirds of the remaining life, leaving room for a couple
		// of failed attempts before the credential actually lapses.
		wait := time.Until(expiry) * 2 / 3
		if wait < time.Minute {
			wait = time.Minute
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		if _, err := w.register(ctx); err != nil {
			// Not fatal. The existing session may still be valid, and the API
			// server may simply be restarting; the next attempt is a minute
			// away and jobs already claimed carry their own capability.
			log.Printf("failed to renew worker session: %v", err)
			continue
		}

		log.Print("worker session renewed")
	}
}
