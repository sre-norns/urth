package urth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/urth"
)

// fakeOutbox is an in-memory DispatchOutbox that records what the relay did to
// each entry. It exists to drive the relay through failure orderings a real
// store cannot be asked for on demand -- notably "the publish succeeded and the
// relay died before marking it".
type fakeOutbox struct {
	mu sync.Mutex

	entries []urth.DispatchOutboxEntry

	published map[uint]time.Time
	receipts  map[uint]urth.DispatchReceipt
	failures  map[uint]error
	notBefore map[uint]time.Time

	// markPublishedErr simulates the database becoming unreachable in the
	// window between a successful publication and the row being marked.
	markPublishedErr error

	claims int
}

func newFakeOutbox(entries ...urth.DispatchOutboxEntry) *fakeOutbox {
	return &fakeOutbox{
		entries:   entries,
		published: map[uint]time.Time{},
		receipts:  map[uint]urth.DispatchReceipt{},
		failures:  map[uint]error{},
		notBefore: map[uint]time.Time{},
	}
}

func (f *fakeOutbox) Claim(_ context.Context, _ string, limit int, _ time.Duration) ([]urth.DispatchOutboxEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.claims++

	var due []urth.DispatchOutboxEntry
	for i := range f.entries {
		if len(due) == limit {
			break
		}
		if _, done := f.published[f.entries[i].ID]; done {
			continue
		}
		f.entries[i].Attempts++
		due = append(due, f.entries[i])
	}

	return due, nil
}

func (f *fakeOutbox) MarkPublished(_ context.Context, id uint, at time.Time, receipt urth.DispatchReceipt) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.markPublishedErr != nil {
		return f.markPublishedErr
	}
	f.published[id] = at
	f.receipts[id] = receipt

	return nil
}

func (f *fakeOutbox) MarkFailed(_ context.Context, id uint, cause error, notBefore time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failures[id] = cause
	f.notBefore[id] = notBefore

	return nil
}

func (f *fakeOutbox) Stats(context.Context, time.Time) (urth.DispatchOutboxStats, error) {
	return urth.DispatchOutboxStats{}, nil
}

// recordingPublisher captures every event UID handed to the transport.
type recordingPublisher struct {
	mu sync.Mutex

	seen []string
	err  error

	// sequence stands in for a transport that addresses its messages, so the
	// relay is seen to carry the receipt through to the row rather than dropping
	// it on the floor.
	sequence uint64
}

func (p *recordingPublisher) PublishDispatch(_ context.Context, entry urth.DispatchOutboxEntry) (urth.DispatchReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.err != nil {
		return urth.DispatchReceipt{}, p.err
	}
	p.seen = append(p.seen, entry.EventUID)

	return urth.DispatchReceipt{Sequence: p.sequence}, nil
}

func (p *recordingPublisher) uids() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.seen...)
}

func testEntry(id uint, eventUID string) urth.DispatchOutboxEntry {
	return urth.DispatchOutboxEntry{
		ID:            id,
		SchemaVersion: urth.DispatchOutboxEntryVersion,
		EventUID:      eventUID,
		ResultUID:     "result-1",
		ResultVersion: 1,
		ScenarioName:  "test-scenario",
		RunnerUID:     "runner-1",
	}
}

// A relay that dies after the broker accepted the message but before the row is
// marked must republish under the *same* event UID.
//
// This is the crash point the outbox cannot avoid -- there is no way to make a
// broker publication and a database update atomic -- so the design's answer is
// that publication is at-least-once and the message ID is stable. If the ID were
// regenerated on retry, deduplication would have nothing to match and a scenario
// would run twice.
func TestRelayRetryAfterCrashReusesEventUID(t *testing.T) {
	outbox := newFakeOutbox(testEntry(1, "result-1.1"))
	outbox.markPublishedErr = errors.New("database connection lost")
	publisher := &recordingPublisher{}

	relay := urth.NewDispatchRelay(outbox, publisher)

	// First pass: the transport accepts the message, marking it fails.
	_, err := relay.RunOnce(context.Background())
	require.Error(t, err)
	require.Equal(t, []string{"result-1.1"}, publisher.uids())

	// The relay comes back and finds the entry still unpublished.
	outbox.markPublishedErr = nil
	published, err := relay.RunOnce(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, published)

	// Two publications, one event UID: the broker's duplicate window collapses
	// them into a single delivered dispatch.
	require.Equal(t, []string{"result-1.1", "result-1.1"}, publisher.uids())
}

// A transport outage must not consume the entry. The relay records the failure,
// schedules a retry, and leaves the row for the next pass.
func TestRelayRetriesAfterTransportFailure(t *testing.T) {
	outbox := newFakeOutbox(testEntry(1, "result-1.1"))
	publisher := &recordingPublisher{err: errors.New("no responders available")}

	relay := urth.NewDispatchRelay(outbox, publisher, urth.WithRelayBackoff(time.Second, time.Minute))

	published, err := relay.RunOnce(context.Background())
	require.Error(t, err)
	require.Zero(t, published)

	outbox.mu.Lock()
	defer outbox.mu.Unlock()

	require.Contains(t, outbox.failures[1].Error(), "no responders available")
	require.Empty(t, outbox.published, "a failed publication must not mark the entry published")
	require.WithinDuration(t, time.Now().Add(time.Second), outbox.notBefore[1], 500*time.Millisecond)
}

// A dispatch that can never be routed is held back for a long time rather than
// retried on the fast loop, so it cannot crowd out live work while it waits for
// the dead-letter path (task 012) to retire it.
func TestRelayBacksOffPermanentFailureFarLonger(t *testing.T) {
	outbox := newFakeOutbox(testEntry(1, "result-1.1"))
	publisher := &recordingPublisher{
		err: errors.Join(urth.ErrPermanentDispatch, errors.New("result has no runner assigned")),
	}

	relay := urth.NewDispatchRelay(outbox, publisher, urth.WithRelayBackoff(time.Second, time.Minute))

	_, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)

	outbox.mu.Lock()
	defer outbox.mu.Unlock()

	require.WithinDuration(t, time.Now().Add(urth.PermanentDispatchBackoff), outbox.notBefore[1], time.Minute)
}

// One unroutable entry must not stop the rest of the batch. Runners are
// independent; a scenario that cannot be placed is no reason for every other
// runner's work to stay queued.
func TestRelayContinuesBatchAfterOneFailure(t *testing.T) {
	broken := testEntry(1, "result-1.1")
	// A row written by a newer API server than this relay understands.
	broken.SchemaVersion = urth.DispatchOutboxEntryVersion + 1

	outbox := newFakeOutbox(broken, testEntry(2, "result-2.1"))
	publisher := &recordingPublisher{}

	relay := urth.NewDispatchRelay(outbox, publisher)

	published, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)
	require.Equal(t, 1, published)
	require.Equal(t, []string{"result-2.1"}, publisher.uids(),
		"the unreadable entry must not have been handed to the transport")
}

// stubScheduler stands in for the legacy asynq transport.
type stubScheduler struct {
	scheduled []urth.Result
	err       error
}

func (s *stubScheduler) Close() error { return nil }

func (s *stubScheduler) Schedule(_ context.Context, result urth.Result, _ urth.Scenario) (urth.RunID, error) {
	if s.err != nil {
		return urth.InvalidRunID, s.err
	}
	s.scheduled = append(s.scheduled, result)

	return urth.RunID("task-1"), nil
}

type stubLoader struct {
	result   urth.Result
	scenario urth.Scenario
	err      error
}

func (l *stubLoader) LoadForDispatch(context.Context, urth.DispatchOutboxEntry) (urth.Result, urth.Scenario, error) {
	return l.result, l.scenario, l.err
}

// The legacy transport is reached through the same outbox, so that both
// transports share one durability story until task 015 removes asynq.
func TestSchedulerPublisherDispatchesThroughLegacyScheduler(t *testing.T) {
	var result urth.Result
	result.UID = "result-1"
	result.Version = 1

	scheduler := &stubScheduler{}
	publisher := urth.NewSchedulerDispatchPublisher(scheduler, &stubLoader{result: result})

	_, err := publisher.PublishDispatch(context.Background(), testEntry(1, "result-1.1"))
	require.NoError(t, err)
	require.Len(t, scheduler.scheduled, 1)
}

// An entry written for an older version of a Result is not replayed against the
// current one: the entry records what was true at commit time, and dispatching
// it now would start a run that current state does not ask for.
func TestSchedulerPublisherRejectsStaleResultVersion(t *testing.T) {
	var result urth.Result
	result.UID = "result-1"
	result.Version = 4

	scheduler := &stubScheduler{}
	publisher := urth.NewSchedulerDispatchPublisher(scheduler, &stubLoader{result: result})

	_, err := publisher.PublishDispatch(context.Background(), testEntry(1, "result-1.1"))
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)
	require.Empty(t, scheduler.scheduled)
}
