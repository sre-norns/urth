package natsq_test

import (
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/natsq"
)

// Configuration is checked before a broker is contacted.
//
// "Is my configuration valid" and "is my broker up" are different questions, and
// until this existed they shared one error: a duplicate window longer than the
// job age surfaced as a 500 from the stream provisioner, phrased in JetStream's
// own field names, after a connection had been made. Nothing in it named the
// flag to change.
//
// None of these tests start a NATS server. That is the point of them.

// validConfig is a configuration with every constraint satisfied, so each test
// below changes exactly the field it is about.
func validConfig() natsq.Config {
	return natsq.Config{
		Replicas:         1,
		MaxJobs:          10_000,
		MaxBytes:         1 << 30,
		MaxJobsPerRunner: 1024,
		MaxMsgSize:       8 << 10,
		DuplicateWindow:  30 * time.Minute,
		MaxJobAge:        time.Hour,
		AckWait:          30 * time.Second,
		MaxDeliver:       5,
		MaxAckPending:    64,
	}
}

func TestDefaultConfigIsValid(t *testing.T) {
	// The defaults ship as an operator's starting point; a set of them that does
	// not pass its own validation would be a poor one.
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("the reference configuration must validate, got: %v", err)
	}
}

// The known reachable case: JetStream refuses a duplicate window longer than the
// stream's maximum age, and an operator lowering --nats.max-job-age has no way
// to know that from the flags.
func TestDuplicateWindowMayNotExceedMaxJobAge(t *testing.T) {
	cfg := validConfig()
	cfg.MaxJobAge = 5 * time.Second

	err := cfg.Validate()
	if err == nil {
		t.Fatal("a duplicate window longer than the job age must be rejected")
	}

	message := err.Error()
	for _, want := range []string{"--nats.duplicate-window", "30m", "--nats.max-job-age", "5s"} {
		if !strings.Contains(message, want) {
			t.Errorf("validation error should name %q, got: %v", want, message)
		}
	}
}

// An operator tuning several related settings should not rediscover the next
// constraint on the next start.
func TestValidateReportsEveryViolationAtOnce(t *testing.T) {
	cfg := validConfig()
	cfg.MaxJobAge = time.Second   // shorter than the duplicate window
	cfg.Replicas = 0              // not a replica count JetStream accepts
	cfg.MaxJobsPerRunner = 20_000 // larger than the global limit
	cfg.MaxAckPending = -1        // nonsense
	cfg.MaxDeliver = 0            // unlimited redelivery of a job nobody can claim

	err := cfg.Validate()
	if err == nil {
		t.Fatal("an invalid configuration must be rejected")
	}

	message := err.Error()
	for _, want := range []string{
		"--nats.duplicate-window",
		"--nats.replicas",
		"--nats.max-jobs-per-runner",
		"--nats.max-ack-pending",
		"--nats.max-deliver",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("validation should report %q in the same pass, got: %v", want, message)
		}
	}
}

// Unlimited is a decision, not a default to fall into. JetStream reads 0 as "no
// limit" on every one of these, which is exactly the state the stream bounds
// exist to prevent.
func TestValidateRejectsUnlimitedValues(t *testing.T) {
	cases := map[string]struct {
		mutate func(*natsq.Config)
		flag   string
	}{
		"global messages":   {func(c *natsq.Config) { c.MaxJobs = 0 }, "--nats.max-jobs"},
		"global bytes":      {func(c *natsq.Config) { c.MaxBytes = 0 }, "--nats.max-bytes"},
		"per-runner jobs":   {func(c *natsq.Config) { c.MaxJobsPerRunner = 0 }, "--nats.max-jobs-per-runner"},
		"message size":      {func(c *natsq.Config) { c.MaxMsgSize = 0 }, "--nats.max-msg-size"},
		"job age":           {func(c *natsq.Config) { c.MaxJobAge = 0 }, "--nats.max-job-age"},
		"duplicate window":  {func(c *natsq.Config) { c.DuplicateWindow = 0 }, "--nats.duplicate-window"},
		"ack wait":          {func(c *natsq.Config) { c.AckWait = 0 }, "--nats.ack-wait"},
		"delivery attempts": {func(c *natsq.Config) { c.MaxDeliver = 0 }, "--nats.max-deliver"},
		"ack pending":       {func(c *natsq.Config) { c.MaxAckPending = 0 }, "--nats.max-ack-pending"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := validConfig()
			tc.mutate(&cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatalf("an unlimited %s must be rejected", name)
			}
			if !strings.Contains(err.Error(), tc.flag) {
				t.Errorf("error should name %q, got: %v", tc.flag, err)
			}
		})
	}
}

// A per-runner limit above the global one is not reachable: the stream stops
// accepting long before one runner gets there, so the per-runner limit an
// operator set is not the limit they get.
func TestPerRunnerLimitMayNotExceedTheGlobalOne(t *testing.T) {
	cfg := validConfig()
	cfg.MaxJobs = 100
	cfg.MaxJobsPerRunner = 101

	err := cfg.Validate()
	if err == nil {
		t.Fatal("a per-runner limit above the global limit must be rejected")
	}
	if !strings.Contains(err.Error(), "--nats.max-jobs") {
		t.Errorf("error should name the global flag, got: %v", err)
	}
}

// JetStream accepts 1..5 replicas. Anything else is refused by the server, and
// an even count has no quorum benefit worth the write cost.
func TestValidateRejectsUnsupportedReplicaCounts(t *testing.T) {
	for _, replicas := range []int{-1, 0, 6, 100} {
		cfg := validConfig()
		cfg.Replicas = replicas

		if err := cfg.Validate(); err == nil {
			t.Errorf("replica count %d must be rejected", replicas)
		}
	}

	for _, replicas := range []int{1, 3, 5} {
		cfg := validConfig()
		cfg.Replicas = replicas

		if err := cfg.Validate(); err != nil {
			t.Errorf("replica count %d must be accepted, got: %v", replicas, err)
		}
	}
}

// The CLI struct every command hosting this transport writes: the config is
// embedded, so kong walks into it looking for a Validate method.
type testCLI struct {
	NATS natsq.Config `embed:"" prefix:"nats."`
}

// The validation has to be reachable the way an operator reaches it, or it is a
// method nobody calls. This parses a command line rather than constructing a
// Config, which also pins the flag names the messages above promise.
func TestKongValidatesTheEmbeddedConfig(t *testing.T) {
	parse := func(args ...string) error {
		var cli testCLI
		parser, err := kong.New(&cli, kong.Exit(func(int) {}))
		if err != nil {
			t.Fatalf("failed to build the parser: %v", err)
		}

		_, err = parser.Parse(args)

		return err
	}

	// The shipped defaults, with nothing overridden.
	if err := parse(); err != nil {
		t.Fatalf("the default flags must validate, got: %v", err)
	}

	err := parse("--nats.max-job-age=5s")
	if err == nil {
		t.Fatal("kong must refuse the combination during parsing")
	}
	if !strings.Contains(err.Error(), flagDuplicateWindowText) {
		t.Errorf("parse error should name the flag to change, got: %v", err)
	}
}

// The flag name as it appears in --help, kept next to the test that asserts it
// rather than imported from the package under test, so a rename has to be made
// deliberately in both places.
const flagDuplicateWindowText = "--nats.duplicate-window"

// The claim handshake has to fit inside the window the job is allowed to live
// for, or a job expires mid-handshake and the worker's claim races its own
// message expiry.
func TestAckWaitMayNotExceedMaxJobAge(t *testing.T) {
	cfg := validConfig()
	cfg.AckWait = 2 * time.Hour

	err := cfg.Validate()
	if err == nil {
		t.Fatal("an ack wait longer than the job age must be rejected")
	}
	if !strings.Contains(err.Error(), "--nats.ack-wait") {
		t.Errorf("error should name the ack-wait flag, got: %v", err)
	}
}
