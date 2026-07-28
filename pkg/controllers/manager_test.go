package controllers_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/controllers"
	"github.com/stretchr/testify/require"
)

// fastManager supervises with backoffs short enough for a test to observe
// several restarts without waiting out the production defaults.
func fastManager() *controllers.Manager {
	return controllers.NewManager(
		controllers.WithBackoff(time.Millisecond, 5*time.Millisecond),
		controllers.WithHealthyRunTime(time.Hour),
	)
}

// blockUntilDone is the shape every healthy control loop has: it runs until its
// context is cancelled and reports that as the reason it stopped.
func blockUntilDone(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestManagerRestartsALoopThatPanics(t *testing.T) {
	// Against the previous composition -- `go func() { _ = loop.Run(ctx) }()` --
	// this test does not fail, it takes the test binary down with it, which is
	// exactly what it did to the api-server.
	var starts atomic.Int32
	restarted := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	manager.Add("panics-once", controllers.LoopFunc(func(ctx context.Context) error {
		if starts.Add(1) == 1 {
			panic("a repair pass dereferenced something nil")
		}

		close(restarted)

		return blockUntilDone(ctx)
	}))

	manager.Start(ctx)

	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("a loop that panicked was never restarted")
	}

	cancel()
	require.NoError(t, manager.Wait(5*time.Second))
	require.EqualValues(t, 2, starts.Load())
}

func TestManagerKeepsOtherLoopsRunningWhenOnePanics(t *testing.T) {
	// The reason supervision is per loop rather than per process: a defective
	// reconciler must not stop the relay from publishing, or a bug in repair
	// becomes a bug in dispatch.
	var panicked atomic.Int32

	healthy := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	manager.Add("always-panics", controllers.LoopFunc(func(context.Context) error {
		panicked.Add(1)
		panic("still broken")
	}))
	manager.Add("healthy", controllers.LoopFunc(func(ctx context.Context) error {
		close(healthy)
		return blockUntilDone(ctx)
	}))

	manager.Start(ctx)

	select {
	case <-healthy:
	case <-time.After(5 * time.Second):
		t.Fatal("a healthy loop did not start beside a panicking one")
	}

	// Let the broken loop fail a few times, then confirm it is still being
	// retried rather than having quietly given up.
	require.Eventually(t, func() bool { return panicked.Load() >= 3 },
		5*time.Second, time.Millisecond,
		"a loop that keeps panicking must keep being restarted, not abandoned")

	cancel()
	require.NoError(t, manager.Wait(5*time.Second))
}

func TestManagerRestartsALoopThatReturnsEarly(t *testing.T) {
	// A control loop's Run is an infinite loop, so returning at all while its
	// context is live means something went wrong -- a nil error is no more
	// trustworthy here than a panic.
	var starts atomic.Int32
	restarted := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	manager.Add("returns-early", controllers.LoopFunc(func(ctx context.Context) error {
		if starts.Add(1) == 1 {
			return nil
		}

		close(restarted)

		return blockUntilDone(ctx)
	}))

	manager.Start(ctx)

	select {
	case <-restarted:
	case <-time.After(5 * time.Second):
		t.Fatal("a loop that returned while its context was live was not restarted")
	}

	cancel()
	require.NoError(t, manager.Wait(5*time.Second))
}

func TestManagerStopsLoopsOnCancellation(t *testing.T) {
	// Loops participating in shutdown is what stops a rolling restart from
	// hard-killing a scan mid-transaction.
	var running atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	for _, name := range []string{"first", "second"} {
		manager.Add(name, controllers.LoopFunc(func(ctx context.Context) error {
			running.Add(1)
			defer running.Add(-1)

			return blockUntilDone(ctx)
		}))
	}

	manager.Start(ctx)
	require.Eventually(t, func() bool { return running.Load() == 2 },
		5*time.Second, time.Millisecond)

	cancel()

	require.NoError(t, manager.Wait(5*time.Second))
	require.EqualValues(t, 0, running.Load(), "Wait returned while a loop was still running")
}

func TestManagerDoesNotRestartAfterCancellation(t *testing.T) {
	// A loop that returns because it was cancelled has not failed. Restarting it
	// would spin for as long as the process took to exit.
	var starts atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	manager.Add("well-behaved", controllers.LoopFunc(func(ctx context.Context) error {
		starts.Add(1)
		return blockUntilDone(ctx)
	}))

	manager.Start(ctx)
	require.Eventually(t, func() bool { return starts.Load() == 1 },
		5*time.Second, time.Millisecond)

	cancel()
	require.NoError(t, manager.Wait(5*time.Second))

	// Long enough that a restart would have happened at a 1ms backoff.
	time.Sleep(50 * time.Millisecond)
	require.EqualValues(t, 1, starts.Load(), "a cancelled loop was restarted")
}

func TestManagerWaitReportsALoopThatWillNotStop(t *testing.T) {
	// Reported rather than waited out: the alternative is a process that never
	// exits, and an operator who cannot see which stage of shutdown ran long.
	stuck := make(chan struct{})
	defer close(stuck)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	manager.Add("ignores-cancellation", controllers.LoopFunc(func(context.Context) error {
		<-stuck
		return nil
	}))

	manager.Start(ctx)
	cancel()

	require.ErrorIs(t, manager.Wait(50*time.Millisecond), controllers.ErrShutdownTimeout)
}

func TestManagerReportsRegisteredLoops(t *testing.T) {
	manager := fastManager()
	require.Equal(t, 0, manager.Len())

	require.NoError(t, manager.Add("dispatch-relay", controllers.LoopFunc(blockUntilDone)))
	require.NoError(t, manager.Add("dispatch-reconciler", controllers.LoopFunc(blockUntilDone)))

	require.Equal(t, 2, manager.Len())
	require.Equal(t, []string{"dispatch-relay", "dispatch-reconciler"}, manager.Names())
}

func TestManagerRefusesALoopAddedAfterStart(t *testing.T) {
	// It would never be started, and a control loop that silently does not exist
	// is the failure this whole arrangement is trying to avoid.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	manager.Start(ctx)

	err := manager.Add("too-late", controllers.LoopFunc(blockUntilDone))
	require.ErrorIs(t, err, controllers.ErrManagerStarted)
	require.Equal(t, 0, manager.Len(), "a refused loop must not be registered")

	cancel()
	require.NoError(t, manager.Wait(5*time.Second))
}

// TestMustAddPanicsOnlyWhenAddWouldFail keeps the two entry points in step: the
// panicking one is a wrapper over the same check, not a second rule.
func TestMustAddPanicsOnlyWhenAddWouldFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := fastManager()
	require.NotPanics(t, func() {
		manager.MustAdd("in-time", controllers.LoopFunc(blockUntilDone))
	})
	require.Equal(t, 1, manager.Len())

	manager.Start(ctx)

	require.Panics(t, func() {
		manager.MustAdd("too-late", controllers.LoopFunc(blockUntilDone))
	})

	cancel()
	require.NoError(t, manager.Wait(5*time.Second))
}

func TestLoopFuncReportsItsError(t *testing.T) {
	sentinel := errors.New("boom")
	require.ErrorIs(t, controllers.LoopFunc(func(context.Context) error {
		return sentinel
	}).Run(context.Background()), sentinel)
}
