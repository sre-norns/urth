package urth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// Relay defaults. They are deliberately modest: the relay is a latency path for
// every manually triggered run, so the poll interval is what an operator feels
// when nothing else is happening, while the batch size bounds how much work one
// relay takes on behalf of the others.
const (
	// DefaultRelayPollInterval is how long a relay waits after finding nothing.
	DefaultRelayPollInterval = 250 * time.Millisecond

	// DefaultRelayBatchSize is how many entries one relay leases at a time.
	DefaultRelayBatchSize = 32

	// DefaultRelayLease is how long a claim survives its relay. It must exceed
	// the time a publication can take, or a second relay will take over an entry
	// that is still being published -- which is safe, because the event UID
	// deduplicates it, but is wasted work.
	DefaultRelayLease = 30 * time.Second

	// DefaultRelayPublishTimeout bounds one publication attempt.
	//
	// Without a bound, a broker that accepts the connection and then never
	// answers holds the relay for as long as its caller's context allows, and
	// every other runner's dispatch waits behind it.
	DefaultRelayPublishTimeout = 15 * time.Second

	// DefaultRelayBookkeepingTimeout bounds the writes that record an outcome.
	DefaultRelayBookkeepingTimeout = 10 * time.Second

	// DefaultRelayRetryBackoff is the first retry delay after a failure.
	DefaultRelayRetryBackoff = 1 * time.Second

	// DefaultRelayMaxBackoff caps exponential growth, so a broker that has been
	// down all night is still retried promptly once it returns.
	DefaultRelayMaxBackoff = 1 * time.Minute

	// PermanentDispatchBackoff is how long an unpublishable entry is held back.
	//
	// Long, but not infinite. The failure is usually "this run was never placed
	// on a runner", and a runner appearing later does not revive this Result --
	// task 012 owns the dead-letter path that will retire such entries properly.
	// Until then a long backoff keeps them from crowding out live work while
	// leaving them visible in the backlog rather than silently discarded.
	PermanentDispatchBackoff = 1 * time.Hour
)

// DispatchRelay carries committed outbox entries to a transport.
//
// It is the second half of the transactional outbox: the writing transaction
// makes the intent to dispatch durable, and the relay makes it happen. Both
// halves are needed, and neither is sufficient. The relay publishes at least
// once by design -- it may crash between the broker accepting a message and the
// row being marked -- so publication carries the entry's stable event UID and
// consumers stay idempotent.
type DispatchRelay struct {
	outbox    DispatchOutbox
	publisher DispatchPublisher

	relayID            string
	pollInterval       time.Duration
	batchSize          int
	lease              time.Duration
	retryBackoff       time.Duration
	maxBackoff         time.Duration
	publishTimeout     time.Duration
	bookkeepingTimeout time.Duration
}

// RelayOption configures a DispatchRelay.
type RelayOption func(*DispatchRelay)

// WithRelayID names this relay in the leases it takes. Operators read it when
// asking which process is sitting on an entry, so it should identify a process,
// not a deployment.
func WithRelayID(value string) RelayOption {
	return func(r *DispatchRelay) { r.relayID = value }
}

// WithRelayPollInterval sets the idle poll interval.
func WithRelayPollInterval(value time.Duration) RelayOption {
	return func(r *DispatchRelay) { r.pollInterval = value }
}

// WithRelayBatchSize sets how many entries are leased per poll.
func WithRelayBatchSize(value int) RelayOption {
	return func(r *DispatchRelay) { r.batchSize = value }
}

// WithRelayLease sets how long a claim survives the relay holding it.
func WithRelayLease(value time.Duration) RelayOption {
	return func(r *DispatchRelay) { r.lease = value }
}

// WithRelayBackoff sets the initial and maximum retry delays.
func WithRelayBackoff(initial, max time.Duration) RelayOption {
	return func(r *DispatchRelay) {
		r.retryBackoff = initial
		r.maxBackoff = max
	}
}

// WithRelayPublishTimeout bounds one publication attempt.
func WithRelayPublishTimeout(value time.Duration) RelayOption {
	return func(r *DispatchRelay) { r.publishTimeout = value }
}

// NewDispatchRelay builds a relay over an outbox and a transport publisher.
func NewDispatchRelay(outbox DispatchOutbox, publisher DispatchPublisher, options ...RelayOption) *DispatchRelay {
	relay := &DispatchRelay{
		outbox:             outbox,
		publisher:          publisher,
		relayID:            defaultRelayID(),
		pollInterval:       DefaultRelayPollInterval,
		batchSize:          DefaultRelayBatchSize,
		lease:              DefaultRelayLease,
		retryBackoff:       DefaultRelayRetryBackoff,
		maxBackoff:         DefaultRelayMaxBackoff,
		publishTimeout:     DefaultRelayPublishTimeout,
		bookkeepingTimeout: DefaultRelayBookkeepingTimeout,
	}

	for _, option := range options {
		option(relay)
	}

	return relay
}

// defaultRelayID produces a name unique to this process.
func defaultRelayID() string {
	return fmt.Sprintf("relay-%s", NewRandToken(8))
}

// RunOnce leases one batch and publishes it, returning how many were published.
//
// Exposed separately from Run so that tests can drive the relay deterministically
// rather than racing a ticker.
func (r *DispatchRelay) RunOnce(ctx context.Context) (published int, err error) {
	entries, err := r.outbox.Claim(ctx, r.relayID, r.batchSize, r.lease)
	if err != nil {
		return 0, err
	}

	for _, entry := range entries {
		if perr := r.publish(ctx, entry); perr != nil {
			// One entry's failure is recorded against that entry and the batch
			// continues: a single unroutable dispatch must not stop every other
			// runner's work from being published.
			err = errors.Join(err, perr)
			continue
		}
		published++
	}

	return published, err
}

// bookkeeping derives the context used to record an entry's outcome.
//
// Detached from the caller's deadline on purpose. The context handed to a
// publication is the one that just expired waiting for a broker that would not
// answer; reusing it to record that failure fails too, and the entry is left
// leased with no error text -- the one case where an operator most needs to see
// why. Cancellation is still honoured through the timeout, so shutdown does not
// hang on it.
func (r *DispatchRelay) bookkeeping(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.bookkeepingTimeout)
}

// publish sends one entry and records the outcome against its row.
func (r *DispatchRelay) publish(ctx context.Context, entry DispatchOutboxEntry) error {
	if entry.SchemaVersion != DispatchOutboxEntryVersion {
		// A row written by a newer API server than this relay. Guessing at its
		// meaning is worse than leaving it for a relay that understands it.
		return r.recordFailure(ctx, entry, fmt.Errorf("%w: outbox schema %d, want %d",
			ErrPermanentDispatch, entry.SchemaVersion, DispatchOutboxEntryVersion))
	}

	publishCtx, cancel := context.WithTimeout(ctx, r.publishTimeout)
	defer cancel()

	receipt, err := r.publisher.PublishDispatch(publishCtx, entry)
	if err != nil {
		return r.recordFailure(ctx, entry, err)
	}

	// The window between the broker's acknowledgement and this update is the
	// crash point the event UID exists for: the message is out and the row does
	// not know it. Recording it as a failure -- rather than returning and leaving
	// the lease to lapse -- means a relay that merely lost the database for a
	// moment retries in seconds instead of waiting out its own lease. Either way
	// the republication carries the same event UID and the broker discards it.
	markCtx, cancel := r.bookkeeping(ctx)
	defer cancel()

	if err := r.outbox.MarkPublished(markCtx, entry.ID, time.Now(), receipt); err != nil {
		return r.recordFailure(ctx, entry, err)
	}

	log.Printf("relayed dispatch %v for result %v to runner %q (attempt %d)",
		entry.EventUID, entry.ResultUID, entry.RunnerUID, entry.Attempts)

	return nil
}

// recordFailure schedules the entry's next attempt.
func (r *DispatchRelay) recordFailure(ctx context.Context, entry DispatchOutboxEntry, cause error) error {
	backoff := r.backoffFor(entry, cause)

	log.Printf("failed to relay dispatch %v for result %v (attempt %d, retry in %v): %v",
		entry.EventUID, entry.ResultUID, entry.Attempts, backoff, cause)

	failCtx, cancel := r.bookkeeping(ctx)
	defer cancel()

	if err := r.outbox.MarkFailed(failCtx, entry.ID, cause, time.Now().Add(backoff)); err != nil {
		return errors.Join(cause, err)
	}

	return cause
}

// backoffFor computes the delay before an entry is retried.
func (r *DispatchRelay) backoffFor(entry DispatchOutboxEntry, cause error) time.Duration {
	if errors.Is(cause, ErrPermanentDispatch) {
		return PermanentDispatchBackoff
	}

	// Exponential in the attempt count, which is already incremented by the
	// claim, so the first failure waits the base delay rather than double it.
	backoff := r.retryBackoff
	for attempt := 1; attempt < entry.Attempts && backoff < r.maxBackoff; attempt++ {
		backoff *= 2
	}

	if backoff > r.maxBackoff {
		return r.maxBackoff
	}

	return backoff
}

// Run drains the outbox until the context is cancelled.
//
// Errors are logged rather than returned: a relay that stopped on the first
// broker hiccup would leave the outbox unattended for exactly as long as the
// broker was unwell, which is the opposite of what it is for.
func (r *DispatchRelay) Run(ctx context.Context) error {
	log.Printf("dispatch relay %q started (batch=%d, poll=%v, lease=%v)",
		r.relayID, r.batchSize, r.pollInterval, r.lease)

	for {
		published, err := r.RunOnce(ctx)
		if ctx.Err() != nil {
			log.Printf("dispatch relay %q stopped", r.relayID)
			return ctx.Err()
		}
		if err != nil {
			log.Printf("dispatch relay %q: %v", r.relayID, err)
		}

		// A full batch means there is probably more waiting, so poll again
		// immediately instead of sleeping through a backlog.
		if published == r.batchSize {
			continue
		}

		select {
		case <-ctx.Done():
			log.Printf("dispatch relay %q stopped", r.relayID)
			return ctx.Err()
		case <-time.After(r.pollInterval):
		}
	}
}

// SchedulerDispatchPublisher adapts a legacy urth.Scheduler to the outbox.
//
// The asynq transport publishes the whole job -- prob kind, spec, and script --
// so it needs the Result itself, not just the dispatch identity. Rather than keep
// that transport on the pre-outbox code path until task 015 retires it, this
// adapter rehydrates the Result from the store at publication time. Both
// transports then share one durability story, and removing asynq later removes
// this type rather than a second way of dispatching.
type SchedulerDispatchPublisher struct {
	scheduler Scheduler
	results   ResultLoader
}

// ResultLoader reads the Result a dispatch is for.
//
// It loads the Result and stops there. The Scenario used to be loaded beside it,
// and a dispatch was refused when that scenario had been deleted -- which made a
// run already committed to happen depend on a resource it no longer needs. The
// Result's execution snapshot is the whole job.
type ResultLoader interface {
	LoadForDispatch(ctx context.Context, entry DispatchOutboxEntry) (Result, error)
}

// NewSchedulerDispatchPublisher wraps a Scheduler as a DispatchPublisher.
func NewSchedulerDispatchPublisher(scheduler Scheduler, results ResultLoader) *SchedulerDispatchPublisher {
	return &SchedulerDispatchPublisher{scheduler: scheduler, results: results}
}

// PublishDispatch implements DispatchPublisher.
//
// The receipt is always empty: asynq names its tasks, but nothing in this build
// can withdraw one, so claiming an address the reconciler cannot act on would be
// worse than admitting there is none. Retiring a stale asynq task belongs to
// task 015, which removes this path rather than extending it.
func (p *SchedulerDispatchPublisher) PublishDispatch(ctx context.Context, entry DispatchOutboxEntry) (DispatchReceipt, error) {
	result, err := p.results.LoadForDispatch(ctx, entry)
	if err != nil {
		return DispatchReceipt{}, err
	}

	// A Result that has moved on since the entry was written is not dispatched.
	// The entry is a record of what was true at commit time; replaying it against
	// newer state would start a run the current state does not ask for.
	if result.Version != entry.ResultVersion {
		return DispatchReceipt{}, fmt.Errorf("%w: result %v is at version %d, dispatch was written for %d",
			ErrPermanentDispatch, entry.ResultUID, result.Version, entry.ResultVersion)
	}

	if _, err := p.scheduler.Schedule(ctx, result); err != nil {
		return DispatchReceipt{}, err
	}

	return DispatchReceipt{}, nil
}
