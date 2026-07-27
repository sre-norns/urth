package urth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ErrPermanentDispatch marks a publication failure that retrying cannot fix.
//
// The distinction matters because the two outcomes want opposite handling: a
// broker that is down should be retried until it comes back, while a dispatch
// that can never be routed -- no runner, an envelope this build cannot encode --
// would otherwise be retried forever and bury the retryable entries behind it.
var ErrPermanentDispatch = errors.New("dispatch cannot be published")

// DispatchOutboxEntry is the durable record that a Result needs dispatching.
//
// It exists because committing a Result to Postgres and publishing its dispatch
// to the broker are two separate durable writes. Doing them in that order leaves
// a window -- a crash, a lost connection, a deployment -- in which authoritative
// state says work is pending and no message exists to wake a worker. Writing
// this row inside the Result's own transaction moves the publication decision
// into the same commit as the state that justifies it; a relay then carries the
// row to the broker at least once.
//
// It deliberately stores dispatch *fields* rather than an opaque pre-marshalled
// transport payload. Two reasons: the row stays queryable by an operator asking
// which runner has a backlog, and a relay running newer code than the writer
// re-encodes the envelope at publication time rather than replaying a stale wire
// format. The event UID is what must not be regenerated, and it is persisted.
type DispatchOutboxEntry struct {
	// ID is the table's own key. The outbox is not a manifest resource: it is
	// internal plumbing with no name, no labels, and no REST surface, so it does
	// not embed manifest.ObjectMeta.
	ID uint `gorm:"primaryKey;autoIncrement"`

	// SchemaVersion versions this record's layout, so a relay can refuse a row
	// written by a future writer rather than publish a half-understood dispatch.
	SchemaVersion int `gorm:"not null"`

	// EventUID is the stable identity of this dispatch, used as the broker's
	// deduplication key (`Nats-Msg-Id`). It is written once, at enqueue time, and
	// never regenerated: a relay that published and then died must present the
	// same value on retry, or the duplicate suppression it depends on is useless.
	EventUID string `gorm:"uniqueIndex;not null"`

	ResultUID     manifest.ResourceID   `gorm:"index;not null"`
	ResultVersion manifest.Version      `gorm:"not null"`
	ScenarioName  manifest.ResourceName `gorm:"not null"`

	// RunnerUID is the runner this dispatch is routed to. It may be empty: the
	// legacy asynq transport accepts unplaced runs, and a routing transport is
	// entitled to reject them. See resultsAPIImpl.placeRun.
	RunnerUID manifest.ResourceID `gorm:"index"`

	// Time columns carry no explicit `type:` tag. Postgres must store these as
	// TIMESTAMPTZ -- a naive TIMESTAMP reads back shifted by the server's offset,
	// which this project has already been bitten by once -- and gorm's Postgres
	// driver maps time.Time to timestamptz by default, so the correct type is
	// what AutoMigrate produces. Naming it explicitly would also force it on
	// SQLite, where the driver cannot scan the result, and the outbox is the one
	// table whose bookkeeping is worth testing without a container.
	CreatedAt   time.Time  `gorm:"not null;index"`
	UpdatedAt   time.Time  `gorm:"not null"`
	PublishedAt *time.Time `gorm:"index"`

	// NotBefore holds a failed entry back so a broker outage is retried with
	// backoff rather than spun on.
	NotBefore time.Time `gorm:"not null;index"`

	// Attempts counts publication attempts, including the one in flight.
	Attempts int `gorm:"not null"`

	// LastError is the most recent failure, kept for operators rather than for
	// control flow. Truncated on write; a driver error can be very long.
	LastError string

	// ClaimedBy and ClaimExpiresAt implement the relay lease. A relay that dies
	// mid-publication leaves its claim behind, and the lease expiry is what lets
	// another relay take the row over instead of the entry stalling forever.
	ClaimedBy      string     `gorm:"index"`
	ClaimExpiresAt *time.Time `gorm:"index"`
}

// DispatchOutboxEntryVersion is the current DispatchOutboxEntry layout.
const DispatchOutboxEntryVersion = 1

// LastErrorLimit bounds the stored failure text.
const LastErrorLimit = 1024

// TableName keeps the table out of gorm's pluralisation guesswork.
func (DispatchOutboxEntry) TableName() string {
	return "dispatch_outbox"
}

// DispatchEventUID derives the stable dispatch identity for a Result version.
//
// Result UID plus version, rather than a random value: a republication of the
// same Result state then produces the same key, so broker-side deduplication can
// collapse it and a worker retrying a claim presents an ID the API already knows.
func DispatchEventUID(uid manifest.ResourceID, version manifest.Version) string {
	return fmt.Sprintf("%v.%v", uid, version)
}

// NewDispatchOutboxEntry builds the outbox row for a placed, pending Result.
//
// Called with the Result as it exists inside the creating transaction, after the
// insert has assigned its UID and version -- those two values are the entry's
// identity, so building it any earlier would key it on zeroes.
func NewDispatchOutboxEntry(result Result, now time.Time) DispatchOutboxEntry {
	return DispatchOutboxEntry{
		SchemaVersion: DispatchOutboxEntryVersion,
		EventUID:      DispatchEventUID(result.UID, result.Version),
		ResultUID:     result.UID,
		ResultVersion: result.Version,
		ScenarioName:  result.Spec.Scenario.Name,
		RunnerUID:     result.Status.Executor.RunnerID,
		NotBefore:     now,
	}
}

// DispatchPublisher hands one outbox entry to a transport.
//
// The interface is owned here, and implemented by the transport packages, so
// that pkg/urth stays free of any broker dependency: the domain knows that a
// dispatch must be published exactly once per event UID, and nothing about how.
//
// An implementation must not return until the transport has durably accepted the
// message. Returning early would let the relay mark an entry published that the
// broker may never deliver, which is the very failure the outbox exists to close.
type DispatchPublisher interface {
	PublishDispatch(ctx context.Context, entry DispatchOutboxEntry) error
}

// DispatchOutbox is the relay's view of the outbox table.
type DispatchOutbox interface {
	// Claim leases up to limit due entries to one relay. Entries already leased
	// by a live relay are skipped, not waited on.
	Claim(ctx context.Context, relayID string, limit int, lease time.Duration) ([]DispatchOutboxEntry, error)

	// MarkPublished records a successful publication and releases the lease.
	MarkPublished(ctx context.Context, id uint, at time.Time) error

	// MarkFailed records a failed attempt, releases the lease, and holds the
	// entry back until notBefore.
	MarkFailed(ctx context.Context, id uint, cause error, notBefore time.Time) error

	// Stats summarises the unpublished backlog for monitoring.
	Stats(ctx context.Context, now time.Time) (DispatchOutboxStats, error)
}

// DispatchOutboxStats is what an operator needs in order to notice the outbox is
// not draining: a backlog that is growing, an entry that is old, or entries that
// have failed enough times to be worth looking at.
type DispatchOutboxStats struct {
	// Pending is the number of entries not yet published.
	Pending int64 `json:"pending" yaml:"pending"`

	// Failing is the number of unpublished entries that have already failed at
	// least once. A healthy outbox has a pending count that churns and a failing
	// count of zero.
	Failing int64 `json:"failing" yaml:"failing"`

	// OldestAge is how long the oldest unpublished entry has been waiting. This
	// is the number to alert on: it stays near zero while the relay keeps up,
	// regardless of throughput.
	OldestAge time.Duration `json:"oldestAge" yaml:"oldestAge"`

	// MaxAttempts is the highest attempt count among unpublished entries.
	MaxAttempts int `json:"maxAttempts" yaml:"maxAttempts"`

	// LastError is the most recent failure text among unpublished entries.
	LastError string `json:"lastError,omitempty" yaml:"lastError,omitempty"`
}

// truncateError renders a failure for storage, bounded in length.
func truncateError(cause error) string {
	if cause == nil {
		return ""
	}

	text := cause.Error()
	if len(text) > LastErrorLimit {
		return text[:LastErrorLimit]
	}

	return text
}
