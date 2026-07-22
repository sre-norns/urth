package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
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
func (w *worker) consume(ctx context.Context, consumer jetstream.Consumer) error {
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

		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			<-slots

			if ctx.Err() != nil {
				inFlight.Wait()
				return nil
			}

			// A fetch failure is usually a reconnect in progress. The NATS
			// client reconnects on its own, so this backs off rather than
			// tearing the worker down -- a worker inside someone else's network
			// that exits on a blip becomes an operator callout.
			log.Printf("failed to fetch jobs: %v", err)
			select {
			case <-ctx.Done():
				inFlight.Wait()
				return nil
			case <-time.After(2 * time.Second):
			}
			continue
		}

		var received bool
		for msg := range batch.Messages() {
			received = true
			inFlight.Add(1)

			go func(msg jetstream.Msg) {
				defer inFlight.Done()
				defer func() { <-slots }()

				w.handle(ctx, msg)
			}(msg)
		}

		if err := batch.Error(); err != nil {
			log.Printf("job batch ended with error: %v", err)
		}

		if !received {
			// Nothing waiting; release the slot we reserved for it.
			<-slots
		}
	}
}

// handle claims and executes one delivered job.
func (w *worker) handle(ctx context.Context, msg jetstream.Msg) {
	envelope, err := natsq.UnmarshalEnvelope(msg.Data())
	if err != nil {
		// A message nobody can parse will not parse next time either.
		// Terminating it stops an infinite redelivery loop and surfaces the
		// message to the dead-letter path instead.
		log.Printf("terminating unreadable job message: %v", err)
		if terr := msg.Term(); terr != nil {
			log.Printf("failed to terminate unreadable message: %v", terr)
		}
		return
	}

	// A message for another runner means this worker is bound to a consumer it
	// should not be. Executing it anyway would defeat the placement rules.
	if envelope.RunnerUID != w.runnerUID {
		log.Printf("terminating job for runner %q delivered to worker of runner %q",
			envelope.RunnerUID, w.runnerUID)
		if terr := msg.Term(); terr != nil {
			log.Printf("failed to terminate misrouted message: %v", terr)
		}
		return
	}

	auth, outcome := w.claim(ctx, envelope)

	if applyDisposition(msg, outcome, envelope.ResultUID) {
		w.execute(ctx, envelope, auth)
	}
}

// applyDisposition performs the JetStream acknowledgement decided by a claim
// outcome and reports whether the job should now be executed. This is the one
// place a claim outcome becomes an Ack/Nak/Term, kept separate from handle so the
// decision can be tested without a live probe.
func applyDisposition(msg jetstream.Msg, outcome claimOutcome, resultUID manifest.ResourceID) (execute bool) {
	switch outcome {
	case claimAccepted:
		// Acknowledge now, and before execution, because the claim has committed.
		//
		// Both other orderings are wrong in a different way. Acking after
		// execution makes the ack-wait timer span an arbitrarily long probe, so a
		// slow run gets redelivered and executed twice. Not acking loses the
		// message on the next reconnect. The API's record of the claim, not the
		// broker, owns the run from here; if this ack is lost the idempotent claim
		// is what stops the redelivery becoming a second execution.
		if err := msg.Ack(); err != nil {
			log.Printf("failed to ack claimed job %v: %v", resultUID, err)
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
		log.Printf("terminating job %v this worker may not run", resultUID)
		if err := msg.Term(); err != nil {
			log.Printf("failed to terminate refused job %v: %v", resultUID, err)
		}

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
func (w *worker) claim(ctx context.Context, envelope natsq.DispatchEnvelope) (urth.AuthJobResponse, claimOutcome) {
	claimCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
// place. Anything that is not a recognised API status -- a network error, a
// connection reset, an unparseable body -- is transient by default, because the
// run may still be pending and losing its only message is the worse failure.
func classifyClaimFailure(err error) claimOutcome {
	var apiErr *bark.ErrorResponse
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.Code == http.StatusConflict:
			// The run no longer needs this dispatch: terminal, superseded, or
			// held elsewhere. Drop the message.
			return claimStale
		case apiErr.Code == http.StatusForbidden,
			apiErr.Code == http.StatusUnauthorized,
			apiErr.Code == http.StatusBadRequest,
			apiErr.Code == http.StatusNotFound:
			// A refusal redelivery to this worker will not reverse, or a message
			// malformed enough that the endpoint could not route it. Terminate it.
			return claimTerminal
		case apiErr.Code >= 500:
			return claimRetry
		}
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
