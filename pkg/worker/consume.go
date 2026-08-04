package worker

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// claimOutcome is what the worker decided to do about a delivered message.
//
// It exists because "the claim failed" is not one situation. A job the API
// server was briefly unable to answer about should come back; a job another
// worker already legitimately holds should not; a job this worker is not
// permitted to run should stop being redelivered to it forever. ADR 0004 sets
// out these dispositions and getting them wrong is either a lost run or an
// infinite redelivery loop.
type claimOutcome int

const (
	// claimAccepted: the API granted the claim. Acknowledge and execute.
	claimAccepted claimOutcome = iota

	// claimRetry: a transient failure the API reported as 5xx. The run may still
	// be pending, so leave the message for redelivery (NAK with a delay).
	claimRetry

	// claimStale: the run is already terminal or validly held elsewhere (409).
	// The message describes work that no longer needs doing, so acknowledge and
	// drop it.
	claimStale

	// claimTerminal: a policy decision that redelivery will not change (401/403),
	// or a malformed message. Terminate it so it stops being redelivered here and
	// enters the operational dead-letter path.
	claimTerminal

	// claimAbandon: the claim was interrupted by worker shutdown before the API
	// answered. That is not a verdict on the run, so leave the message
	// unacknowledged and let the broker redeliver it after AckWait.
	claimAbandon
)

// consume pulls jobs and executes them, up to the configured concurrency.
func (w *Worker) consume(ctx context.Context, consumer jetstream.Consumer) error {
	// The whole claim handshake has to fit inside the consumer's AckWait, and the
	// consumer is the only place a worker can learn what that is -- it has the
	// client half of the NATS configuration (URL and credentials) and never the
	// stream settings, which are the control plane's to set.
	w.budgetHandshake(consumer)

	// The semaphore is the backpressure ADR 0004 asks for: the worker fetches
	// only as much as it can currently execute, rather than reserving jobs it
	// will sit on. Messages it holds without claiming are messages no other
	// worker can take.
	slots := make(chan struct{}, w.config.Concurrency)
	var inFlight sync.WaitGroup

	log.Printf("consuming jobs, concurrency %d", w.config.Concurrency)

	for {
		select {
		case <-ctx.Done():
			log.Print("shutdown requested; waiting for in-flight runs to finish")
			inFlight.Wait()
			return nil
		case slots <- struct{}{}:
		}

		if !w.pump(ctx, consumer, slots, &inFlight) {
			inFlight.Wait()
			return nil
		}
	}
}

// Pull timings.
//
// fetchMaxWait is how long one pull waits for work before it is reissued;
// fetchHeartbeat is how often the server must say something during it. The
// heartbeat is set explicitly because nats.go only supplies one for windows of
// ten seconds or more, and this window is deliberately shorter -- see pump.
const (
	fetchMaxWait    = 5 * time.Second
	fetchHeartbeat  = 1500 * time.Millisecond
	fetchRetryPause = 2 * time.Second
)

// pump fetches at most one job and hands it to a handler, reporting whether the
// consume loop should carry on.
//
// The caller has already reserved a slot for the message this fetch may return;
// pump releases it again on every path that does not start a handler.
//
// Both bounds on the fetch are load-bearing, and neither was there before.
//
// The *context* is what makes a shutdown prompt: a pull is a request-reply, and
// waiting out its window before noticing that the process is stopping made a
// worker take seconds longer to exit than it needed to -- or, when the pull was
// one the broker was never going to answer, not exit at all.
//
// The *heartbeat* is the more serious of the two. A pull request lost to a
// reconnect leaves the client waiting on a reply to an inbox the server has
// forgotten; with no heartbeat there is nothing to notice that with. The worker
// stays connected, keeps registering, keeps reporting itself alive, and quietly
// consumes nothing at all -- which is the worst shape a failure can take here,
// because every other signal says the fleet is healthy. Found by
// test/integration, where a broker restart mid-suite reproduced it.
func (w *Worker) pump(ctx context.Context, consumer jetstream.Consumer, slots chan struct{}, inFlight *sync.WaitGroup) bool {
	// Cancelled only once the batch has been drained: the batch is filled by a
	// goroutine watching this context, so cancelling it early would discard
	// messages the broker has already handed over.
	fetchCtx, cancelFetch := context.WithTimeout(ctx, fetchMaxWait)
	defer cancelFetch()

	batch, err := consumer.Fetch(1,
		jetstream.FetchContext(fetchCtx),
		jetstream.FetchHeartbeat(fetchHeartbeat),
	)
	if err != nil {
		<-slots

		if ctx.Err() != nil {
			return false
		}

		// A fetch failure is usually a reconnect in progress. The NATS client
		// reconnects on its own, so this backs off rather than tearing the
		// worker down -- a worker inside someone else's network that exits on a
		// blip becomes an operator callout.
		log.Printf("failed to fetch jobs: %v", err)
		select {
		case <-ctx.Done():
			return false
		case <-time.After(fetchRetryPause):
		}

		return true
	}

	var received bool
	for msg := range batch.Messages() {
		if received {
			// Only the first message uses the slot reserved before the fetch.
			// Fetch(1) means there should never be a second -- but the slot
			// accounting must not depend on that: every handler releases a
			// slot when it finishes, so a batch that returned two messages
			// against one reservation would drain the semaphore and wedge the
			// fetch loop permanently. Blocking here is ordinary backpressure
			// and resolves as soon as a running handler finishes.
			select {
			case <-ctx.Done():
				return false
			case slots <- struct{}{}:
			}
		}

		received = true
		inFlight.Add(1)

		go func(msg jetstream.Msg) {
			defer inFlight.Done()
			defer func() { <-slots }()

			w.handle(ctx, msg)
		}(msg)
	}

	// Only while the worker is still meant to be running. A batch ended by the
	// shutdown above reports the cancellation, which is not news.
	if err := batch.Error(); err != nil && ctx.Err() == nil {
		log.Printf("job batch ended with error: %v", err)
	}

	if !received {
		// Nothing waiting; release the slot we reserved for it.
		<-slots
	}

	return true
}

// budgetHandshake divides the consumer's AckWait between claim and ack.
func (w *Worker) budgetHandshake(consumer jetstream.Consumer) {
	var ackWait time.Duration
	if info := consumer.CachedInfo(); info != nil {
		ackWait = info.Config.AckWait
	}

	w.claimBudget, w.ackBudget = handshakeBudget(ackWait)

	log.Printf("claim handshake budget: %v to claim, %v to confirm the acknowledgement (ack-wait %v)",
		w.claimBudget, w.ackBudget, ackWait)
}

// handle claims and executes one delivered job.
func (w *Worker) handle(ctx context.Context, msg jetstream.Msg) {
	envelope, err := natsq.UnmarshalEnvelope(msg.Data())
	report := w.reportDispatchFailure(ctx, msg, envelope, err == nil)

	if err != nil {
		// A message nobody can parse will not parse next time either.
		// Terminating it stops an infinite redelivery loop -- but only once the
		// control plane has a record of it, because a terminated message is
		// gone and this log line is not something anyone will find.
		log.Printf("unreadable job message: %v", err)
		terminate(msg, report(urth.ReasonMalformedEnvelope, err.Error()), "unreadable message")

		return
	}

	// A message for another runner means this worker is bound to a consumer it
	// should not be. Executing it anyway would defeat the placement rules.
	if envelope.RunnerUID != w.runnerUID {
		detail := fmt.Sprintf("dispatched to runner %v, delivered to a worker of runner %v",
			envelope.RunnerUID, w.runnerUID)
		log.Printf("misrouted job: %s", detail)
		terminate(msg, report(urth.ReasonMisroutedDispatch, detail), "misrouted message")

		return
	}

	// Claimed locally before the API is asked, because the duplicate this guards
	// against is a redelivery arriving while the first claim is still in flight.
	// See inFlightRuns.
	if !w.inFlight.acquire(envelope.ResultUID) {
		w.metrics.duplicateDelivery()
		log.Printf("dropping a redelivery of %v: this worker is already executing it", envelope.ResultUID)

		// Acknowledged and dropped, not naked. It describes work this process
		// already owns, so the message is redundant: holding it would reserve one
		// of the runner's MaxAckPending slots for the length of a probe -- exactly
		// what ADR 0004 says an ack must never span -- and naking it would bring
		// it back every AckWait until MaxDeliver filed a dead letter for a
		// dispatch that was delivered perfectly well. If this process dies now,
		// the execution lease and the reconciler are the recovery, as they are for
		// any acknowledged message.
		if err := msg.Ack(); err != nil {
			log.Printf("failed to ack a duplicate delivery of %v: %v", envelope.ResultUID, err)
		}

		return
	}
	defer w.inFlight.release(envelope.ResultUID)

	auth, outcome := w.claim(ctx, envelope)
	w.metrics.claimed(outcome)

	if applyDisposition(ctx, msg, outcome, envelope.ResultUID, report, w.confirmClaimAck) {
		w.runJob(ctx, envelope, auth)
	}
}

// confirmClaimAck is the worker's ackConfirmer, bound to its handshake budget.
func (w *Worker) confirmClaimAck(ctx context.Context, msg jetstream.Msg) error {
	return confirmAck(ctx, msg, w.ackBudget, w.metrics)
}

// runJob executes a claimed job, through a seam the tests can replace.
//
// Everything this task cares about -- that the acknowledgement is confirmed
// before execution starts, and that a duplicate delivery starts no second
// execution -- is a statement about whether and when a probe runs. Asserting
// that needs a stand-in for the probe; the alternative is proving the ordering
// against a real prob, which tests the prober rather than the handshake.
func (w *Worker) runJob(ctx context.Context, envelope natsq.DispatchEnvelope, auth urth.AuthJobResponse) {
	if w.executeJob != nil {
		w.executeJob(ctx, envelope, auth)
		return
	}

	w.execute(ctx, envelope, auth)
}

// applyDisposition performs the JetStream acknowledgement decided by a claim
// outcome and reports whether the job should now be executed. This is the one
// place a claim outcome becomes an Ack/Nak/Term, kept separate from handle so the
// decision can be tested without a live probe.
func applyDisposition(ctx context.Context, msg jetstream.Msg, outcome claimOutcome, resultUID manifest.ResourceID, report dispatchReporter, ack ackConfirmer) (execute bool) {
	switch outcome {
	case claimAccepted:
		// Acknowledge now, and before execution, because the claim has committed.
		//
		// Both other orderings are wrong in a different way. Acking after
		// execution makes the ack-wait timer span an arbitrarily long probe, so a
		// slow run gets redelivered and executed twice. Not acking loses the
		// message on the next reconnect.
		//
		// Server-confirmed, not fire-and-forget: an ack that was only published
		// leaves a redelivery window this worker cannot see, and the redelivery
		// would be idempotently re-authorised rather than refused. See confirmAck.
		//
		// Detached from the caller's context. A worker asked to shut down drains
		// its in-flight runs, so the run this ack belongs to is still going to be
		// executed and reported; inheriting the cancellation would guarantee the
		// ack it needs is never confirmed.
		if err := ack(context.WithoutCancel(ctx), msg); err != nil {
			// Not a reason to refuse the run. The claim is committed in Postgres:
			// the Result is `running`, leased, with this worker recorded as its
			// executor. Declining to execute now would strand a run the control
			// plane already believes is in progress, and the one thing this worker
			// must not do is roll that back or hand it to somebody else.
			log.Printf("could not confirm the acknowledgement of claimed job %v (%v); executing it regardless", resultUID, err)
		}

		return true

	case claimRetry:
		// Leave it for redelivery, after a delay so a struggling API server is
		// not immediately asked again.
		if err := msg.NakWithDelay(5 * time.Second); err != nil {
			log.Printf("failed to nak job %v: %v", resultUID, err)
		}

	case claimStale:
		log.Printf("discarding stale job message for result %v", resultUID)
		if err := msg.Ack(); err != nil {
			log.Printf("failed to ack stale job %v: %v", resultUID, err)
		}

	case claimTerminal:
		// A permanent refusal is a dead letter, not just a message to drop: the
		// run stays pending and nothing else will ever start it. Reported before
		// the message goes, so the reason survives the message.
		log.Printf("job %v refused permanently for this worker", resultUID)
		terminate(msg, report(urth.ReasonPolicyRefused, "the API permanently refused this worker's claim"),
			fmt.Sprintf("refused job %v", resultUID))

	case claimAbandon:
		// Shutdown interrupted the claim before it resolved. Leave the message
		// untouched: the broker redelivers it after AckWait, and acking or naking
		// a run whose claim never got an answer would either lose it or fight the
		// shutdown that is already in progress.
		log.Printf("abandoning job %v; claim interrupted by shutdown", resultUID)
	}

	return false
}

// claim asks the API server for authority to run the job.
func (w *Worker) claim(ctx context.Context, envelope natsq.DispatchEnvelope) (urth.AuthJobResponse, claimOutcome) {
	// Bounded by this worker's share of the handshake budget rather than by a
	// number of its own, so that a claim which runs long enough to make its
	// acknowledgement meaningless is abandoned instead. See handshakeBudget.
	budget := w.claimBudget
	if budget <= 0 {
		budget, _ = handshakeBudget(0)
	}

	claimCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	auth, err := w.apiClient.Results(envelope.ScenarioName).ClaimRun(claimCtx,
		envelope.ResultUID,
		w.currentSession(),
		urth.ClaimJobRequest{
			DispatchID:    envelope.DispatchID,
			ResultVersion: envelope.ResultVersion,
			Timeout:       w.config.RunnerConfig.Timeout,
			Labels:        w.config.GetEffectiveLabels(),
		})
	if err == nil {
		return auth, claimAccepted
	}

	// A claim cut short by worker shutdown is not a verdict on the run. Leave the
	// message for redelivery rather than acking or naking a claim that never got
	// an answer. Checked before classification because a cancelled request often
	// surfaces as an opaque transport error, not a status the API chose.
	if ctx.Err() != nil {
		return auth, claimAbandon
	}

	outcome := classifyClaimFailure(err)
	log.Printf("claim for result %v: %v (%s)", envelope.ResultUID, err, outcomeName(outcome))
	return auth, outcome
}

// classifyClaimFailure turns a claim error into a queue disposition using only the
// HTTP status class the API returned. The API owns the mapping from "why" to
// status; the worker owns the mapping from status to Ack/Nak/Term, here, in one
// place. Anything that is not a recognised API status -- see apiStatus -- is
// transient by default, because the run may still be pending and losing its only
// message is the worse failure.
func classifyClaimFailure(err error) claimOutcome {
	status, ok := apiStatus(err)
	if !ok {
		return claimRetry
	}

	switch status {
	case http.StatusConflict:
		// The run no longer needs this dispatch: terminal, superseded, or held
		// elsewhere. Drop the message.
		return claimStale

	case http.StatusForbidden, http.StatusUnauthorized,
		http.StatusBadRequest, http.StatusNotFound:
		// A refusal redelivery to this worker will not reverse, or a message
		// malformed enough that the endpoint could not route it. Terminate it.
		return claimTerminal
	}

	return claimRetry
}

func outcomeName(o claimOutcome) string {
	switch o {
	case claimAccepted:
		return "accepted"
	case claimRetry:
		return "retry"
	case claimStale:
		return "stale"
	case claimTerminal:
		return "terminal"
	case claimAbandon:
		return "abandon"
	default:
		return "unknown"
	}
}
