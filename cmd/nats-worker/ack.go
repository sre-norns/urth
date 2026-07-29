package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// The claim handshake's time budget.
//
// ADR 0004 §4 gives the handshake -- pull, claim, acknowledge -- exactly one
// window: the consumer's AckWait. Past it the broker redelivers, and an
// acknowledgement arriving afterwards is confirming a message that is already
// being offered to somebody else. So the two halves are budgeted from that one
// number rather than chosen independently, which is what the prototype did: its
// claim timeout was 30s and the default AckWait is also 30s, so a slow claim
// could spend the entire window and leave the ack no time that still counted.
const (
	// ackConfirmReserve is how much of the handshake is held back for
	// confirming the acknowledgement, in the same spirit as uploadReserve
	// holding back part of a run's lease for reporting it.
	ackConfirmReserve = 5 * time.Second

	// ackConfirmAttempt bounds one DoubleAck round trip. Short: this is a
	// request to a broker the worker is already connected to, and an attempt
	// that has not answered in this long is not going to.
	ackConfirmAttempt = 1500 * time.Millisecond

	// ackConfirmRetryPause spaces out attempts. Without it a failure that
	// returns instantly -- a closed connection -- would spin through the whole
	// reserve in microseconds and report as though the budget had been given a
	// fair chance.
	ackConfirmRetryPause = 250 * time.Millisecond

	// defaultHandshakeBudget is used when the consumer cannot tell us its
	// AckWait. It matches the shipped --nats.ack-wait default; guessing the
	// documented default is better than an unbounded claim.
	defaultHandshakeBudget = 30 * time.Second
)

// ackConfirmer confirms with the broker that an acknowledgement was recorded.
//
// A function type so the disposition logic can be exercised without a NATS
// server: what a test needs to vary is whether confirmation succeeds, fails, or
// takes forever, and none of those need a broker to express.
type ackConfirmer func(context.Context, jetstream.Msg) error

// handshakeBudget splits a consumer's AckWait between claiming and confirming.
//
// One invariant, and it holds for every input: claim + ack ≤ AckWait. There is
// deliberately no floor under either half. An AckWait too small to fit a claim
// is a misconfiguration, and the honest expression of it is claims that abandon
// and messages that are redelivered until the dead-letter path reports them --
// not a worker that quietly grants itself more of the window than the operator
// allowed, and then acknowledges messages that have already been offered to
// somebody else. The split is logged at startup so the number is diagnosable.
func handshakeBudget(ackWait time.Duration) (claim, ack time.Duration) {
	if ackWait <= 0 {
		ackWait = defaultHandshakeBudget
	}

	// A short AckWait is split evenly rather than handing the whole thing to the
	// acknowledgement: a confirmed ack for a claim that never had time to happen
	// is not worth anything.
	ack = min(ackConfirmReserve, ackWait/2)

	return ackWait - ack, ack
}

// confirmAck acknowledges a claimed message and waits for the server to say so.
//
// jetstream.Msg.Ack only publishes the acknowledgement and returns; nothing
// waits for it to be recorded. A connection lost between that publish and the
// server processing it redelivers a message whose run this worker is already
// executing -- and because the claim is idempotent for the same worker and
// dispatch, the redelivery would be authorised rather than refused. DoubleAck
// is the request/reply form that closes that window.
//
// Retried within the reserve, because the failure this exists to survive is a
// momentary one; a reconnect in progress answers on the second attempt.
//
// One known false negative, and it is the acceptable direction: if the
// acknowledgement reached the server but its reply was lost, the message is
// gone and there is nothing left to answer the retry, so it times out and this
// reports "unconfirmed" for a message that was in fact acknowledged. The cost
// is a counted metric and a log line. The opposite mistake -- reporting
// confirmation that never happened -- would be silent.
func confirmAck(ctx context.Context, msg jetstream.Msg, budget time.Duration, metrics *workerMetrics) error {
	started := time.Now()
	deadline := started.Add(budget)

	var lastErr error
	for attempt := 0; ; attempt++ {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		if attempt > 0 {
			metrics.ackRetried()
		}

		attemptCtx, cancel := context.WithTimeout(ctx, min(remaining, ackConfirmAttempt))
		err := msg.DoubleAck(attemptCtx)
		cancel()

		switch {
		case err == nil:
			metrics.ackConfirmed(time.Since(started))
			return nil

		case errors.Is(err, jetstream.ErrMsgAlreadyAckd):
			// Someone already acknowledged this message and the client knows it.
			// Retrying would spend the whole reserve re-learning that, so this
			// counts as confirmed: the message is not coming back.
			metrics.ackConfirmed(time.Since(started))
			return nil

		case errors.Is(err, jetstream.ErrMsgNoReply), errors.Is(err, jetstream.ErrMsgNotBound):
			// Not a transient failure: this message has no way to be
			// acknowledged at all, and no number of attempts changes that.
			metrics.ackUnconfirmed()
			return fmt.Errorf("message cannot be acknowledged: %w", err)
		}

		lastErr = err

		if ctx.Err() != nil {
			break
		}

		select {
		case <-ctx.Done():
		case <-time.After(min(time.Until(deadline), ackConfirmRetryPause)):
		}
	}

	metrics.ackUnconfirmed()

	if lastErr == nil {
		lastErr = context.DeadlineExceeded
	}

	return fmt.Errorf("acknowledgement unconfirmed after %v: %w", time.Since(started).Round(time.Millisecond), lastErr)
}
