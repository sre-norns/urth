package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
)

// dispatchReportTimeout bounds a dead-letter report.
//
// Short, because the worker is holding a message while it waits and the message
// is invisible to every other worker for the duration. A report that cannot be
// made quickly is better retried on the next redelivery than blocking a slot.
const dispatchReportTimeout = 15 * time.Second

// dispatchReporter records why a message is undeliverable and answers whether it
// may now be removed from the queue.
//
// Written as a function type so the disposition logic can be exercised without
// an API server: what matters in a test is that a refused report keeps the
// message, and that needs a stub rather than a live control plane.
type dispatchReporter func(reason urth.DispatchFailureReason, detail string) (mayTerminate bool)

// reportDispatchFailure tells the control plane why a dispatch is dead, and
// reports whether the message may now be terminated.
//
// The ordering is the point. Terminating first and reporting afterwards means a
// worker that dies in between -- or an API that is briefly unreachable -- has
// destroyed the only evidence that the dispatch ever existed: JetStream will not
// redeliver a terminated message, and the Result is left pending with nothing
// left anywhere to say why. So the report goes first, and the message is removed
// only once its failure is on record.
//
// A report the API refuses as malformed is a different case. It will be refused
// again on every redelivery, so insisting on recording it first would spin on
// that message forever. Those terminate anyway and say so in the log, which is
// the one outcome here that loses information -- deliberately, because the
// alternative loses a worker.
func (w *worker) reportDispatchFailure(ctx context.Context, msg jetstream.Msg, envelope natsq.DispatchEnvelope, envelopeOK bool) dispatchReporter {
	return func(reason urth.DispatchFailureReason, detail string) bool {
		request := urth.ReportDispatchFailureRequest{
			Reason:     reason,
			EventUID:   dispatchEventUID(msg, envelope, envelopeOK),
			Detail:     detail,
			Deliveries: deliveryCount(msg),
		}

		if envelopeOK {
			request.DispatchID = envelope.DispatchID
			request.ResultUID = envelope.ResultUID
			request.ResultVersion = envelope.ResultVersion
		}

		// Detached from the caller's deadline: a report made while the worker is
		// shutting down is exactly the report most worth making, and inheriting
		// a cancelled context would drop it.
		reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dispatchReportTimeout)
		defer cancel()

		failure, err := w.apiClient.DispatchFailures().Report(reportCtx, w.currentSession(), request)
		if err == nil {
			log.Printf("reported dispatch failure %q (%s) for result %v",
				failure.Name, reason, request.ResultUID)

			return true
		}

		if permanentReportRefusal(err) {
			// Recorded loudly: this is the path where a failure goes unrecorded,
			// and an operator seeing it repeatedly is looking at a worker and a
			// control plane that disagree about what a valid report is.
			log.Printf("control plane refused a %s report for result %v as invalid (%v); "+
				"terminating the message anyway", reason, request.ResultUID, err)

			return true
		}

		log.Printf("could not report %s for result %v (%v); leaving the message queued",
			reason, request.ResultUID, err)

		return false
	}
}

// permanentReportRefusal reports whether a refused report will always be refused.
//
// The same status-class reading the claim path uses, and for the same reason: a
// 5xx is the control plane being briefly unable to answer, which must not be
// mistaken for a verdict on the message.
func permanentReportRefusal(err error) bool {
	var apiErr *bark.ErrorResponse
	if errors.As(err, &apiErr) {
		return apiErr.Code >= 400 && apiErr.Code < 500
	}

	// An opaque transport error is transient by default. Terminating a message
	// because the network hiccuped would destroy the evidence this whole path
	// exists to preserve.
	return false
}

// dispatchEventUID names the dispatch a failure is about.
//
// A message this build could not parse still has an identity worth recording:
// its position in the stream. Without one the report has no idempotency key, and
// the same unreadable message redelivered would either be refused or accumulate
// a record per delivery.
func dispatchEventUID(msg jetstream.Msg, envelope natsq.DispatchEnvelope, envelopeOK bool) string {
	if envelopeOK && envelope.DispatchID != "" {
		return envelope.DispatchID
	}

	if meta, err := msg.Metadata(); err == nil && meta != nil {
		return fmt.Sprintf("unreadable.%v.%v", meta.Stream, meta.Sequence.Stream)
	}

	return "unreadable.unknown"
}

// deliveryCount reports how many times the broker has delivered this message.
func deliveryCount(msg jetstream.Msg) int {
	meta, err := msg.Metadata()
	if err != nil || meta == nil {
		return 0
	}

	return int(meta.NumDelivered)
}

// terminate removes a message once its failure has been recorded, or leaves it
// queued when it has not.
//
// The NAK is delayed rather than immediate because the reason a report failed is
// usually that the control plane is unwell, and redelivering straight back into
// the same failure helps nobody.
func terminate(msg jetstream.Msg, mayTerminate bool, what string) {
	if !mayTerminate {
		if err := msg.NakWithDelay(reportRetryDelay); err != nil {
			log.Printf("failed to nak an unreported %s: %v", what, err)
		}

		return
	}

	if err := msg.Term(); err != nil {
		log.Printf("failed to terminate %s: %v", what, err)
	}
}

// reportRetryDelay holds an unreported message back before it is redelivered.
const reportRetryDelay = 30 * time.Second

// httpStatusOf is a small helper for tests asserting the classification above.
func httpStatusOf(err error) int {
	var apiErr *bark.ErrorResponse
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}

	return http.StatusOK
}
