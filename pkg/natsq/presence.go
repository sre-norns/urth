package natsq

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Worker presence travels on Core NATS, for the reason run logs do: it is worth
// something while it is fresh and worth nothing once it is stale. Persisting it
// in JetStream would add a durable stream whose entire content is superseded
// every interval, and a dropped presence message costs one interval of
// resolution rather than a lost fact.
//
// This is the second of Urth's two liveness signals, and the reason it exists as
// well as the heartbeat is that it travels a different path. A worker's HTTPS
// route to the API server and its NATS route to the broker fail independently,
// and which of them failed is the diagnosis: a worker present here but silent to
// the API cannot claim the work it is being offered, while one heartbeating but
// absent here has nowhere to collect work from. See urth.WorkerPresenceAt.
//
// The consequence to keep in mind: the worker publishes this *unconditionally*,
// not only when its heartbeat succeeded. Making one contingent on the other
// would collapse the two signals into one and make the interesting cases
// unobservable.

// PresenceSubject returns the subject a worker announces itself on.
//
// The runner UID precedes the worker UID for the reason it does in LogSubject: a
// worker's publish permission can then be scoped to its own runner's prefix, so
// no worker can manufacture presence for another runner's fleet. Presence that
// any worker could assert for any other would not be evidence of anything.
func PresenceSubject(runnerUID, workerUID manifest.ResourceID) string {
	return fmt.Sprintf("%s.presence.%s.%s", SubjectPrefix, runnerUID, workerUID)
}

// RunnerPresenceSubjectPrefix returns the subject prefix a runner's workers may
// announce themselves on. It is the permission grant that scopes
// PresenceSubject.
func RunnerPresenceSubjectPrefix(runnerUID manifest.ResourceID) string {
	return fmt.Sprintf("%s.presence.%s.*", SubjectPrefix, runnerUID)
}

// AllPresenceSubjects is the wildcard the control plane listens on.
const AllPresenceSubjects = SubjectPrefix + ".presence.*.*"

// PresenceAnnouncement is who a presence subject names.
type PresenceAnnouncement struct {
	RunnerUID manifest.ResourceID
	WorkerUID manifest.ResourceID
}

// ParsePresenceSubject recovers the announcing runner and worker.
//
// Splitting on the separator and requiring an exact token count, rather than
// pattern matching, so that a subject with a missing or extra element is
// rejected instead of silently yielding a partial UID -- the same reasoning as
// RunnerUIDFromLogSubject.
//
// Both IDs are additionally required to be well-formed UUIDs, because they are
// on their way into a query against `uuid` columns. Postgres rejects a malformed
// literal with an error rather than simply not matching, so without this a
// worker publishing rubbish in its own subject would turn every announcement
// into a logged database failure instead of a message quietly ignored.
func ParsePresenceSubject(subject string) (PresenceAnnouncement, bool) {
	// urth.v1.presence.<runner-uid>.<worker-uid>
	const (
		runnerTokenIndex = 3
		workerTokenIndex = 4
		tokenCount       = 5
	)

	tokens := strings.Split(subject, ".")
	if len(tokens) != tokenCount || tokens[0] != "urth" || tokens[1] != "v1" || tokens[2] != "presence" {
		return PresenceAnnouncement{}, false
	}

	runnerUID, ok := resourceUUID(tokens[runnerTokenIndex])
	if !ok {
		return PresenceAnnouncement{}, false
	}

	workerUID, ok := resourceUUID(tokens[workerTokenIndex])
	if !ok {
		return PresenceAnnouncement{}, false
	}

	return PresenceAnnouncement{RunnerUID: runnerUID, WorkerUID: workerUID}, true
}

// resourceUUID accepts a subject token only if it is a resource ID as this
// system mints them -- wyrd's ObjectMeta.BeforeCreate assigns a UUID to every
// resource, so anything else in this position did not come from a UID.
func resourceUUID(token string) (manifest.ResourceID, bool) {
	if _, err := uuid.Parse(token); err != nil {
		return "", false
	}

	return manifest.ResourceID(token), true
}

// PresenceSubscriber receives worker announcements for the whole fleet.
type PresenceSubscriber struct {
	sub *nats.Subscription
}

// SubscribeWorkerPresence starts receiving announcements from every worker.
//
// handle is called on the NATS client's dispatch goroutine, so it must return
// promptly and must not block: a slow handler here stalls every other
// subscription on the same connection.
func SubscribeWorkerPresence(conn *nats.Conn, handle func(PresenceAnnouncement)) (*PresenceSubscriber, error) {
	if conn == nil {
		// No broker configured. Reported as "nothing subscribed" rather than as
		// an error, because a deployment without NATS legitimately has no second
		// signal -- its workers simply report presence `unknown` on that path.
		return nil, nil
	}

	sub, err := conn.Subscribe(AllPresenceSubjects, func(msg *nats.Msg) {
		// The announcement is entirely in the subject: a worker able to publish
		// under its runner's prefix has said all there is to say by publishing at
		// all, and a body would only be something to validate. Payloads are
		// ignored rather than rejected so the message can grow later without
		// breaking older servers.
		announcement, ok := ParsePresenceSubject(msg.Subject)
		if !ok {
			return
		}

		handle(announcement)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to worker presence: %w", err)
	}

	return &PresenceSubscriber{sub: sub}, nil
}

// Close stops the subscription.
func (s *PresenceSubscriber) Close() error {
	if s == nil || s.sub == nil {
		return nil
	}

	return s.sub.Unsubscribe()
}

// PresencePublisher announces one worker.
type PresencePublisher struct {
	conn    *nats.Conn
	subject string
}

// NewPresencePublisher returns a publisher for one worker's presence.
func NewPresencePublisher(conn *nats.Conn, runnerUID, workerUID manifest.ResourceID) *PresencePublisher {
	return &PresencePublisher{
		conn:    conn,
		subject: PresenceSubject(runnerUID, workerUID),
	}
}

// Announce says that this worker is on its queue.
//
// Reports its error rather than dropping it, unlike LogPublisher.PublishLine: a
// failure to publish here is itself the diagnosis the caller is trying to make,
// and a worker that cannot announce should say so in its log.
func (p *PresencePublisher) Announce() error {
	if p == nil || p.conn == nil {
		return nil
	}

	return p.conn.Publish(p.subject, nil)
}
