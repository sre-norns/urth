package natsq

import (
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

var (
	// ErrInvalidEnvelope reports a dispatch message that could not be decoded or
	// is missing required identity. It is not retryable: redelivering a message
	// nobody can parse only repeats the failure.
	ErrInvalidEnvelope = errors.New("invalid dispatch envelope")

	// ErrUnsupportedSchema reports an envelope from a newer publisher than this
	// build understands.
	ErrUnsupportedSchema = errors.New("unsupported dispatch schema version")

	// ErrNoConsumer reports that a runner's durable consumer does not exist.
	//
	// For a worker this is fatal rather than a cue to create one. ADR 0004
	// reserves stream and consumer administration to the control plane, so a
	// worker that creates its own consumer has quietly stepped outside the
	// permission model -- and, because a work-queue stream refuses overlapping
	// consumers, would likely be creating one that conflicts with the real one.
	ErrNoConsumer = errors.New("runner consumer does not exist")
)

const (
	// SubjectPrefix namespaces every subject this package uses, and carries the
	// transport's major version so an incompatible future layout can coexist.
	SubjectPrefix = "urth.v1"

	// JobsStreamName is the single work-queue stream carrying all runners' jobs.
	//
	// One stream with disjoint per-runner subjects, rather than a stream per
	// runner: a stream is the unit of persistence and replication, and creating
	// one per runner multiplies that cost for isolation that subjects and
	// filtered consumers already provide.
	JobsStreamName = "URTH_JOBS"

	// JobsSubjectWildcard matches every runner's job subject.
	JobsSubjectWildcard = SubjectPrefix + ".jobs.*"
)

// JobSubject returns the subject carrying jobs for one runner.
//
// The subject is keyed on the runner's immutable UID, never its name. Deleting
// a runner and recreating it with the same name must not attach the new
// runner's workers to the old one's queued messages.
func JobSubject(runnerUID manifest.ResourceID) string {
	return fmt.Sprintf("%s.jobs.%s", SubjectPrefix, runnerUID)
}

// RunnerConsumerName returns the durable consumer name for a runner.
func RunnerConsumerName(runnerUID manifest.ResourceID) string {
	return fmt.Sprintf("runner-%s", runnerUID)
}

// ClientConfig is what any NATS participant needs in order to connect.
//
// Split from Config so a worker's --help shows only the knobs it honours.
// Stream replication and retention are the control plane's business; a worker
// that appeared to offer them would be advertising authority it does not have,
// and ADR 0004 is explicit that workers never administer JetStream assets.
type ClientConfig struct {
	URL string `help:"NATS server URL(s) to connect to" default:"nats://localhost:4222"`

	CredsFile string `help:"Path to a NATS credentials file, when the server requires one" type:"existingfile"`
}

// Config adds the stream and consumer settings only the control plane applies.
type Config struct {
	ClientConfig `embed:""`

	// Replicas is a server-side concern; a worker never creates streams.
	Replicas int `help:"JetStream replica count for the jobs stream. Production deployments want 3" default:"1"`

	// MaxJobsPerRunner bounds one runner's share of the shared stream, so that a
	// runner whose workers are all offline cannot fill the stream and start
	// rejecting publications for every other runner.
	MaxJobsPerRunner int64 `help:"Maximum queued jobs per runner before publication is rejected" default:"1024"`

	// MaxJobAge is an upper bound on how long a job may sit unclaimed. It should
	// track the point past which running a scenario is no longer useful; the
	// reconciler is responsible for marking Results whose messages aged out.
	MaxJobAge time.Duration `help:"Maximum time a job may remain queued before it expires" default:"1h"`

	// AckWait covers the claim handshake only -- pull, call the API, ack -- and
	// not probe execution, which may run for minutes. ADR 0004 is explicit that
	// a worker must not hold the JetStream ack across a probe: delivery is
	// NATS' concern until the claim commits, and the Result's lease owns
	// execution after it.
	AckWait time.Duration `help:"How long a worker has to claim a delivered job before it is redelivered" default:"30s"`

	// MaxDeliver bounds redelivery of a message no worker can make progress on.
	// Reaching it is an operational signal, not a silent discard.
	MaxDeliver int `help:"Maximum delivery attempts for a job before it is dead-lettered" default:"5"`
}

// Connect dials NATS using the configured credentials.
func (c ClientConfig) Connect(name string) (*nats.Conn, error) {
	opts := []nats.Option{
		nats.Name(name),
		// Reconnect indefinitely. A worker sits in someone else's network and
		// may well outlast a NATS restart or a network partition; exiting on
		// disconnect would turn a blip into an operator callout.
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2 * time.Second),
	}

	if c.CredsFile != "" {
		opts = append(opts, nats.UserCredentials(c.CredsFile))
	}

	return nats.Connect(c.URL, opts...)
}
