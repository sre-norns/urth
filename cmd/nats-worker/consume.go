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

	// claimRetry: a transient failure. Leave the message for redelivery.
	claimRetry

	// claimStale: the run is already terminal or validly held elsewhere. The
	// message describes work that no longer needs doing, so drop it.
	claimStale

	// claimRefused: a policy decision that redelivery will not change.
	claimTerminal
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

	switch outcome {
	case claimRetry:
		// Leave it for redelivery, after a delay so a struggling API server is
		// not immediately asked again.
		if nerr := msg.NakWithDelay(5 * time.Second); nerr != nil {
			log.Printf("failed to nak job %v: %v", envelope.ResultUID, nerr)
		}
		return

	case claimStale:
		log.Printf("discarding stale job message for result %v", envelope.ResultUID)
		if aerr := msg.Ack(); aerr != nil {
			log.Printf("failed to ack stale job %v: %v", envelope.ResultUID, aerr)
		}
		return

	case claimTerminal:
		log.Printf("terminating job %v this worker may not run", envelope.ResultUID)
		if terr := msg.Term(); terr != nil {
			log.Printf("failed to terminate refused job: %v", terr)
		}
		return
	}

	// Acknowledge now, and synchronously, because the claim has committed.
	//
	// Both orderings are wrong in a different way. Acking before the claim
	// loses the run if the claim then fails. Holding the ack across execution
	// makes the ack-wait timer span an arbitrarily long probe, so a slow run
	// gets redelivered and executed twice. The API's record of the claim, not
	// the broker, owns the run from here.
	if err := msg.Ack(); err != nil {
		// The run is authorised and about to execute. If the ack was lost the
		// message will be redelivered, and the idempotent claim is what stops
		// that becoming a second execution.
		log.Printf("failed to ack claimed job %v: %v", envelope.ResultUID, err)
	}

	w.execute(ctx, envelope, auth)
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

	// The server deliberately does not distinguish "already taken" from "not
	// yours" from "no such run" -- telling a worker which would tell an
	// attacker which. So an unauthorized answer is read as "this job is not
	// mine to run", and the message is dropped rather than redelivered
	// forever. If the run really is still pending and unassigned, the
	// reconciler is what notices, not an endless retry here.
	if isUnauthorized(err) {
		log.Printf("claim refused for result %v: %v", envelope.ResultUID, err)
		return auth, claimStale
	}

	if ctx.Err() != nil {
		return auth, claimRetry
	}

	log.Printf("failed to claim result %v, will retry: %v", envelope.ResultUID, err)
	return auth, claimRetry
}

func isUnauthorized(err error) bool {
	if errors.Is(err, bark.ErrResourceUnauthorized) || errors.Is(err, bark.ErrResourceNotFound) {
		return true
	}

	var apiErr *bark.ErrorResponse
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusUnauthorized ||
			apiErr.Code == http.StatusForbidden ||
			apiErr.Code == http.StatusNotFound
	}

	return false
}
