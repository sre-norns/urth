// Package controllers composes and supervises Urth's control loops.
//
// A control loop, as [ADR 0006] defines it, runs periodically rather than in
// response to a request, drives observed state toward authoritative Postgres
// state, coordinates with its peers through a database lease rather than elected
// leadership, and sits on no request's latency path. The dispatch relay and the
// dispatch/execution reconciler are control loops; the scheduler, artifact
// retention, and dead-letter processing will be.
//
// Composition lives here rather than in a command so that where a loop runs is a
// property of a deployment rather than of the loop. Every loop runs in every
// api-server replica by default, and moving them to a separate process is meant
// to stay a configuration change -- which it only does if there is one definition
// of how a loop is built and supervised for both commands to share.
//
// [ADR 0006]: ../../docs/adr/0006-control-loop-placement.md
package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"time"
)

// Supervision defaults.
const (
	// DefaultMinBackoff is how long a loop waits before its first restart.
	DefaultMinBackoff = 1 * time.Second

	// DefaultMaxBackoff caps the wait between restarts. A loop that cannot run
	// keeps trying at this interval rather than giving up: a control loop that
	// stops is the silent failure the in-process default exists to prevent, and
	// a stuck loop that retries is at least visible in the lease age.
	DefaultMaxBackoff = 1 * time.Minute

	// DefaultHealthyRunTime is how long a loop must stay up before its restart
	// backoff is forgotten. Without it, a loop that fails once an hour reaches
	// the backoff cap and stays there, so an occasional fault would be
	// indistinguishable from a permanent one.
	DefaultHealthyRunTime = 1 * time.Minute

	// DefaultShutdownTimeout bounds how long Wait blocks for loops to finish.
	DefaultShutdownTimeout = 15 * time.Second
)

// ErrShutdownTimeout reports that a loop was still running when Wait gave up.
var ErrShutdownTimeout = errors.New("controllers did not stop within the shutdown timeout")

// Loop is one control loop.
//
// Run blocks until ctx is cancelled and returns ctx.Err() when it stops for that
// reason. Both loops in Urth already have this shape; it is the whole interface
// a Manager needs.
type Loop interface {
	Run(ctx context.Context) error
}

// LoopFunc adapts a plain function to Loop.
type LoopFunc func(ctx context.Context) error

// Run implements Loop.
func (f LoopFunc) Run(ctx context.Context) error { return f(ctx) }

// Manager runs a set of control loops and keeps them from taking down their host.
//
// This is the obligation ADR 0006 attaches to running loops beside an HTTP
// server: a panic in a repair pass must not become an API outage, and a rolling
// restart must not hard-kill a scan mid-transaction. Both are the Manager's
// rather than each loop's, so that a loop added later inherits them instead of
// re-deciding them.
type Manager struct {
	minBackoff     time.Duration
	maxBackoff     time.Duration
	healthyRunTime time.Duration

	mu      sync.Mutex
	loops   []namedLoop
	started bool

	wg sync.WaitGroup
}

type namedLoop struct {
	name string
	loop Loop
}

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithBackoff sets the bounds on the wait between restarts.
func WithBackoff(minimum, maximum time.Duration) ManagerOption {
	return func(m *Manager) {
		m.minBackoff = minimum
		m.maxBackoff = maximum
	}
}

// WithHealthyRunTime sets how long a loop must run before its backoff resets.
func WithHealthyRunTime(value time.Duration) ManagerOption {
	return func(m *Manager) { m.healthyRunTime = value }
}

// NewManager builds a supervisor for control loops.
func NewManager(options ...ManagerOption) *Manager {
	manager := &Manager{
		minBackoff:     DefaultMinBackoff,
		maxBackoff:     DefaultMaxBackoff,
		healthyRunTime: DefaultHealthyRunTime,
	}

	for _, option := range options {
		option(manager)
	}

	return manager
}

// Add registers a loop under a name used in its log lines.
//
// Registering after Start is a programming error rather than a race to tolerate:
// a loop added to a running manager would never be started, and failing loudly
// beats a control loop that silently does not exist.
func (m *Manager) Add(name string, loop Loop) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		panic(fmt.Sprintf("controllers: loop %q added after the manager started", name))
	}

	m.loops = append(m.loops, namedLoop{name: name, loop: loop})
}

// Len reports how many loops are registered, so a command can say so at startup.
func (m *Manager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.loops)
}

// Names lists the registered loops in registration order.
func (m *Manager) Names() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	names := make([]string, 0, len(m.loops))
	for _, entry := range m.loops {
		names = append(names, entry.name)
	}

	return names
}

// Start launches every registered loop and returns immediately.
//
// Loops stop when ctx is cancelled; Wait blocks for them to finish.
func (m *Manager) Start(ctx context.Context) {
	m.mu.Lock()
	m.started = true
	loops := m.loops
	m.mu.Unlock()

	for _, entry := range loops {
		m.wg.Add(1)

		go func(entry namedLoop) {
			defer m.wg.Done()
			m.supervise(ctx, entry)
		}(entry)
	}
}

// Wait blocks until every loop has stopped, or the timeout elapses.
//
// A timeout is reported rather than waited out because the alternative is a
// process that will not exit: the orchestrator's own kill is the next step, and
// an operator should see which stage of shutdown ran long. Loops are expected to
// finish quickly -- a scan is milliseconds -- so this is a guard, not a schedule.
func (m *Manager) Wait(timeout time.Duration) error {
	done := make(chan struct{})

	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return ErrShutdownTimeout
	}
}

// supervise runs one loop, restarting it when it stops for any reason other than
// cancellation.
//
// A control loop's Run is an infinite loop; returning at all while its context is
// live means something went wrong. Whether it panicked or returned an error, the
// answer is the same -- log it and start it again -- because the failure a
// control loop must never have is stopping quietly.
func (m *Manager) supervise(ctx context.Context, entry namedLoop) {
	backoff := m.minBackoff

	for {
		startedAt := time.Now()
		err := m.runOnce(ctx, entry)

		if ctx.Err() != nil {
			return
		}

		if time.Since(startedAt) >= m.healthyRunTime {
			backoff = m.minBackoff
		}

		log.Printf("controllers: loop %q stopped unexpectedly (%v); restarting in %v",
			entry.name, err, backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		if backoff *= 2; backoff > m.maxBackoff {
			backoff = m.maxBackoff
		}
	}
}

// runOnce calls a loop's Run and converts a panic into an error.
//
// The stack is logged here rather than carried in the error because it is the
// only copy: the goroutine that panicked is the one being recovered, and a
// repair pass that dereferences something nil is undiagnosable from the message
// alone.
func (m *Manager) runOnce(ctx context.Context, entry namedLoop) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("controllers: loop %q panicked: %v\n%s",
				entry.name, recovered, debug.Stack())

			err = fmt.Errorf("panic: %v", recovered)
		}
	}()

	return entry.loop.Run(ctx)
}
