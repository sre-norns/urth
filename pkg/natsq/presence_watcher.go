package natsq

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/sre-norns/urth/pkg/urth"
)

// PresenceWatcher records workers announcing themselves on their runner queues.
//
// A control loop in the sense of ADR 0006 and, like AdvisoryWatcher, one of the
// two that is not periodic: it is driven by arriving messages rather than a
// ticker. It needs no lease and is safe in every replica, because recording
// presence is idempotent -- several api-servers seeing the same announcement
// write the same timestamp to the same row.
//
// Running it in every replica is not merely safe but wanted. Core NATS delivers
// to whoever is subscribed at the time, so a single designated listener would be
// a single point at which a fleet appears to go dark.
type PresenceWatcher struct {
	conn  *nats.Conn
	store urth.WorkerPresenceStore

	// recordTimeout bounds one announcement's database work, so a slow store
	// cannot back up the subscription channel behind it.
	recordTimeout time.Duration
}

// NewPresenceWatcher builds the watcher over an existing NATS connection.
func NewPresenceWatcher(conn *nats.Conn, store urth.WorkerPresenceStore) *PresenceWatcher {
	return &PresenceWatcher{
		conn:          conn,
		store:         store,
		recordTimeout: 10 * time.Second,
	}
}

// Run subscribes until the context is cancelled. Implements controllers.Loop.
func (w *PresenceWatcher) Run(ctx context.Context) error {
	if w.conn == nil || w.store == nil {
		return fmt.Errorf("presence watcher needs both a connection and a presence store")
	}

	// Buffered and read from this loop rather than handled on the client's
	// dispatch goroutine: recording presence is a database write, and doing it
	// inline would stall every other subscription on the connection.
	messages := make(chan *nats.Msg, 256)

	subscription, err := w.conn.ChanSubscribe(AllPresenceSubjects, messages)
	if err != nil {
		return fmt.Errorf("failed to subscribe to %q: %w", AllPresenceSubjects, err)
	}
	defer func() {
		if err := subscription.Unsubscribe(); err != nil {
			log.Printf("failed to stop the worker presence subscription: %v", err)
		}
	}()

	log.Printf("watching %q for worker presence", AllPresenceSubjects)

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

// handle records one announcement.
func (w *PresenceWatcher) handle(ctx context.Context, msg *nats.Msg) {
	// Everything the announcement says is in its subject: a worker that could
	// publish under its runner's prefix has already said all there is to say.
	announcement, ok := ParsePresenceSubject(msg.Subject)
	if !ok {
		log.Printf("ignoring a malformed worker presence subject %q", msg.Subject)
		return
	}

	recordCtx, cancel := context.WithTimeout(ctx, w.recordTimeout)
	defer cancel()

	// The runner is passed through so the write can require the worker to be a
	// member of it. The subscription is a wildcard over runners, so the subject
	// alone does not establish that the announcing worker belongs where it claims
	// -- the same reasoning as the executor check in SubscribeRunLog.
	found, err := w.store.RecordNATSPresence(recordCtx, announcement.WorkerUID, announcement.RunnerUID, time.Now())
	if err != nil {
		log.Printf("failed to record presence for worker %v: %v", announcement.WorkerUID, err)
		return
	}

	if !found {
		// Either the registration is gone, or a worker announced itself under a
		// runner it is not a member of. Both are worth a line and neither is
		// worth acting on: a worker whose registration was dropped learns so from
		// its own heartbeat, which is the path that can tell it.
		log.Printf("presence announcement for unknown worker %v of runner %v",
			announcement.WorkerUID, announcement.RunnerUID)
	}
}
