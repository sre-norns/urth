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
//
// Every limit here is set explicitly, including the ones whose JetStream default
// would be "unlimited". A stream that grows until the disk fills is not a limit
// an operator chose; it is one nobody wrote down. Validate refuses the unlimited
// values rather than passing them through, so the decision has to be made here.
type Config struct {
	ClientConfig `embed:""`

	// Replicas is a server-side concern; a worker never creates streams.
	Replicas int `help:"JetStream replica count for the jobs stream. Production deployments want 3" default:"1"`

	// MaxJobs bounds the whole stream. Per-runner limits alone do not: a fleet of
	// runners, each within its share, still adds up to whatever the disk holds.
	MaxJobs int64 `help:"Maximum queued jobs across all runners before publication is rejected" default:"100000"`

	// MaxBytes is the same bound in the unit the disk actually runs out of.
	// Messages are small and similar in size here, so the two rarely disagree --
	// but only one of them is the thing that fills a volume.
	MaxBytes int64 `help:"Maximum bytes the jobs stream may occupy before publication is rejected" default:"1073741824"`

	// MaxJobsPerRunner bounds one runner's share of the shared stream, so that a
	// runner whose workers are all offline cannot fill the stream and start
	// rejecting publications for every other runner.
	MaxJobsPerRunner int64 `help:"Maximum queued jobs per runner before publication is rejected" default:"1024"`

	// MaxMsgSize bounds one dispatch envelope. The envelope is deliberately
	// nearly empty -- identity, not the probe definition, which is disclosed only
	// in the claim response -- so a message anywhere near this size means
	// something is being carried on the queue that ADR 0004 says should not be.
	// Rejecting it visibly is better than storing it.
	MaxMsgSize int32 `help:"Maximum size of a single dispatch message" default:"8192"`

	// DuplicateWindow is how long JetStream remembers a published message ID.
	//
	// The outbox relay publishes at least once: it can die between the broker
	// accepting a message and the outbox row being marked published, and will
	// then republish the same event UID. Suppressing that duplicate is what this
	// window does, so it must comfortably exceed how long a relay can be down
	// and still retry -- otherwise the retry lands as a second job and a
	// scenario runs twice. The default is generous for that reason; it costs
	// only the memory to remember recent IDs.
	DuplicateWindow time.Duration `help:"How long JetStream suppresses a republished dispatch with the same message ID" default:"30m"`

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

	// MaxAckPending bounds how many jobs one runner's workers may hold
	// unacknowledged at once.
	//
	// It reserves *claim handshakes*, not probe executions: a worker acks as soon
	// as the claim commits and then runs the probe under the Result's own lease,
	// so this is the number of workers that can be mid-handshake, not the number
	// of probes that can be running. Set it too low and a pool of workers pulls
	// in lockstep; too high and a runner reserves work its workers cannot claim
	// promptly, which is what redelivery storms are made of.
	MaxAckPending int `help:"Maximum unacknowledged claim handshakes per runner" default:"64"`
}

// Flag names as an operator typed them, for validation messages.
//
// Spelled out rather than derived: kong owns the mapping from field to flag, and
// a message that guessed at it would drift silently the first time a field was
// renamed. These are the strings that have to appear in `--help`.
const (
	flagReplicas         = "--nats.replicas"
	flagMaxJobs          = "--nats.max-jobs"
	flagMaxBytes         = "--nats.max-bytes"
	flagMaxJobsPerRunner = "--nats.max-jobs-per-runner"
	flagMaxMsgSize       = "--nats.max-msg-size"
	flagDuplicateWindow  = "--nats.duplicate-window"
	flagMaxJobAge        = "--nats.max-job-age"
	flagAckWait          = "--nats.ack-wait"
	flagMaxDeliver       = "--nats.max-deliver"
	flagMaxAckPending    = "--nats.max-ack-pending"
)

// Validate checks every constraint that can be settled from the flags alone.
//
// kong calls this during parsing -- Config is embedded in each command's CLI
// struct, and kong walks embedded structs looking for a Validate method -- so
// the check belongs to the configuration rather than to whichever command
// happened to provision assets first. A second command hosting this transport
// inherits it.
//
// Reaching the broker to discover that a combination of local flags is invalid
// is the slow way round, and it produces the wrong error: JetStream answers in
// its own field names ("duplicates window can not be larger then max age"),
// which names nothing an operator can change and requires a running server to
// find out. "Is my configuration valid" and "is my broker up" are different
// questions and should not share an error.
//
// Every violation is reported, not just the first. An operator tuning several
// related durations should not rediscover the next constraint on the next start.
func (c Config) Validate() error {
	var problems []error

	// Unlimited is a decision, not a default to fall into: JetStream reads 0 as
	// "no limit" on all of these, which is the unbounded stream the limits exist
	// to prevent.
	if c.MaxJobs <= 0 {
		problems = append(problems, fmt.Errorf("%s must be a positive number of messages, got %d: an unbounded stream fills the disk", flagMaxJobs, c.MaxJobs))
	}
	if c.MaxBytes <= 0 {
		problems = append(problems, fmt.Errorf("%s must be a positive number of bytes, got %d: an unbounded stream fills the disk", flagMaxBytes, c.MaxBytes))
	}
	if c.MaxJobsPerRunner <= 0 {
		problems = append(problems, fmt.Errorf("%s must be a positive number of messages, got %d: without it one offline runner can consume the whole stream", flagMaxJobsPerRunner, c.MaxJobsPerRunner))
	}
	if c.MaxMsgSize <= 0 {
		problems = append(problems, fmt.Errorf("%s must be a positive size in bytes, got %d", flagMaxMsgSize, c.MaxMsgSize))
	}
	if c.MaxJobAge <= 0 {
		problems = append(problems, fmt.Errorf("%s must be positive, got %v: a job that never expires is one the reconciler can never settle", flagMaxJobAge, c.MaxJobAge))
	}
	if c.DuplicateWindow <= 0 {
		problems = append(problems, fmt.Errorf("%s must be positive, got %v: without it a republished dispatch runs the scenario twice", flagDuplicateWindow, c.DuplicateWindow))
	}
	if c.AckWait <= 0 {
		problems = append(problems, fmt.Errorf("%s must be positive, got %v", flagAckWait, c.AckWait))
	}
	if c.MaxDeliver <= 0 {
		problems = append(problems, fmt.Errorf("%s must be a positive number of attempts, got %d: unlimited redelivery of a job nobody can claim never reaches the dead-letter path", flagMaxDeliver, c.MaxDeliver))
	}
	if c.MaxAckPending <= 0 {
		problems = append(problems, fmt.Errorf("%s must be a positive number of handshakes, got %d", flagMaxAckPending, c.MaxAckPending))
	}

	// JetStream accepts 1 to 5. An even count buys no quorum for its write cost,
	// so it is refused here rather than merely discouraged in the help text.
	if c.Replicas < 1 || c.Replicas > 5 || c.Replicas%2 == 0 {
		problems = append(problems, fmt.Errorf("%s must be 1, 3 or 5, got %d: production wants 3", flagReplicas, c.Replicas))
	}

	// The constraint the broker enforces, checked here so it is reported in terms
	// of the two flags that produce it.
	if c.DuplicateWindow > 0 && c.MaxJobAge > 0 && c.DuplicateWindow > c.MaxJobAge {
		problems = append(problems, fmt.Errorf(
			"%s (%v) must not exceed %s (%v): JetStream refuses a duplicate window longer than the stream's maximum age",
			flagDuplicateWindow, c.DuplicateWindow, flagMaxJobAge, c.MaxJobAge))
	}

	// A per-runner limit above the global one is not the limit an operator gets:
	// the stream stops accepting long before any single runner reaches it.
	if c.MaxJobs > 0 && c.MaxJobsPerRunner > c.MaxJobs {
		problems = append(problems, fmt.Errorf(
			"%s (%d) must not exceed %s (%d): the stream stops accepting before one runner reaches its share",
			flagMaxJobsPerRunner, c.MaxJobsPerRunner, flagMaxJobs, c.MaxJobs))
	}

	// A handshake given longer than the job may live races the message's own
	// expiry: the worker is still entitled to claim a job the stream has dropped.
	if c.AckWait > 0 && c.MaxJobAge > 0 && c.AckWait > c.MaxJobAge {
		problems = append(problems, fmt.Errorf(
			"%s (%v) must not exceed %s (%v): a claim would outlive the job it is for",
			flagAckWait, c.AckWait, flagMaxJobAge, c.MaxJobAge))
	}

	return errors.Join(problems...)
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
