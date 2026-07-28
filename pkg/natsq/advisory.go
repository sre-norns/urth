package natsq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/sre-norns/urth/pkg/urth"
)

// MaxDeliveryAdvisorySubject is where JetStream announces that it has stopped
// redelivering a message.
//
// Scoped to the jobs stream rather than subscribing to every advisory: the
// control plane's interest is exactly the messages it published, and a wildcard
// over all advisory types would have this loop decoding events it has no opinion
// about.
const MaxDeliveryAdvisorySubject = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES." + JobsStreamName + ".*"

// maxDeliveryAdvisory is JetStream's published payload for a message that
// exhausted its delivery limit.
//
// Declared here rather than imported from nats-server, which is a test-only
// dependency in this build: pulling a server into every api-server binary to
// borrow one struct is a poor trade for a wire format that is versioned and
// stable.
type maxDeliveryAdvisory struct {
	Type       string `json:"type"`
	Stream     string `json:"stream"`
	Consumer   string `json:"consumer"`
	StreamSeq  uint64 `json:"stream_seq"`
	Deliveries uint64 `json:"deliveries"`
}

// maxDeliveryAdvisoryType is the schema identifier JetStream stamps on the event.
const maxDeliveryAdvisoryType = "io.nats.jetstream.advisory.v1.max_deliver"

// AdvisoryWatcher records dispatches the broker has given up on.
//
// It is a control loop in the sense of ADR 0006 -- it runs beside the API,
// coordinates through the database, and is safe to run in several replicas --
// but it is the one that is *not* periodic. Its safety under replication comes
// from the sink instead: recording a dead letter is idempotent by dispatch and
// reason, so every replica seeing the same advisory produces one record.
//
// Core NATS delivers advisories at most once, so this is deliberately not the
// only path to the same conclusion. An advisory that arrives while no replica is
// listening is lost, and the pending Result is then expired by the reconciler
// once its message outlives the transport's job age. The advisory buys a prompt,
// specific diagnosis; the reconciler remains the guarantee.
type AdvisoryWatcher struct {
	conn *nats.Conn
	sink urth.DispatchAdvisorySink
}

// NewAdvisoryWatcher builds the watcher over an existing NATS connection.
func NewAdvisoryWatcher(conn *nats.Conn, sink urth.DispatchAdvisorySink) *AdvisoryWatcher {
	return &AdvisoryWatcher{conn: conn, sink: sink}
}

// Run subscribes until the context is cancelled.
//
// Implements controllers.Loop. The subscription is drained rather than dropped
// on shutdown so an advisory already in the client's buffer is still recorded --
// it is the last chance anyone gets at it.
func (w *AdvisoryWatcher) Run(ctx context.Context) error {
	if w.conn == nil || w.sink == nil {
		return fmt.Errorf("advisory watcher needs both a connection and a sink")
	}

	messages := make(chan *nats.Msg, 64)

	subscription, err := w.conn.ChanSubscribe(MaxDeliveryAdvisorySubject, messages)
	if err != nil {
		return fmt.Errorf("failed to subscribe to %q: %w", MaxDeliveryAdvisorySubject, err)
	}
	defer func() {
		if err := subscription.Drain(); err != nil {
			log.Printf("failed to drain the advisory subscription: %v", err)
		}
	}()

	log.Printf("watching %q for abandoned dispatches", MaxDeliveryAdvisorySubject)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg := <-messages:
			if msg == nil {
				continue
			}

			w.handle(ctx, msg)
		}
	}
}

// handle records one advisory.
func (w *AdvisoryWatcher) handle(ctx context.Context, msg *nats.Msg) {
	var advisory maxDeliveryAdvisory
	if err := json.Unmarshal(msg.Data, &advisory); err != nil {
		log.Printf("could not decode a delivery advisory on %q: %v", msg.Subject, err)
		return
	}

	// Checked rather than assumed from the subject: a wildcard subscription can
	// be reached by a payload this build does not understand, and acting on one
	// would mean dead-lettering a dispatch on the strength of an event that
	// might not mean what it appears to.
	if advisory.Type != "" && advisory.Type != maxDeliveryAdvisoryType {
		log.Printf("ignoring advisory of unexpected type %q on %q", advisory.Type, msg.Subject)
		return
	}

	if advisory.Stream != JobsStreamName {
		return
	}

	// Detached from the loop's context so a shutdown mid-advisory still records
	// it. Core NATS will not deliver this event again.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
	defer cancel()

	err := w.sink.RecordMaxDelivery(recordCtx, urth.DispatchAdvisory{
		StreamSequence: advisory.StreamSeq,
		Deliveries:     int(advisory.Deliveries),
		Channel:        advisory.Consumer,
		ObservedAt:     time.Now(),
	})
	if err != nil {
		log.Printf("failed to record an abandoned dispatch at sequence %d: %v", advisory.StreamSeq, err)
	}
}
