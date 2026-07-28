package urth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/urth"
)

// What the relay does with a dispatch that can never be published.
//
// Before this, nothing: the entry was held back for an hour and tried again,
// forever, while the run it was for stayed `pending`. The reconciler cannot help
// -- an unpublished outbox row is the relay's by design -- so the relay is the
// only component in a position to decide that a dispatch is over.
//
// These tests use a real store because the outcome under test is a state change
// to two other tables, which a fake outbox cannot show.

// A dispatch that was never placed strands its run and files no dead letter.
//
// This is the shape of every entry written before placement was enforced at
// creation, and of a run whose runner was deleted between placement and
// publication. It is deliberately not a dead-letter record: a selector matching
// nothing is an ordinary operational state, and a scheduled scenario in that
// state would file one record per tick. See ReasonNoEligibleRunner.
func TestRelayStrandsUnplacedDispatchWithoutADeadLetter(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	created := pendingRun(t, srv, seedScenario(t, store))

	relay := urth.NewDispatchRelay(
		urth.NewDispatchOutbox(db),
		&recordingPublisher{
			err: errors.Join(urth.ErrPermanentDispatch, urth.ErrDispatchUnplaced),
		},
		urth.WithUndeliverableDispatches(urth.NewUndeliverableRecorder(store)),
	)

	_, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)

	var stored urth.Result
	found, err := store.GetByUID(context.Background(), &stored, created.UID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, urth.JobErrored, stored.Status.Status,
		"a run whose dispatch can never be published must not stay pending")
	require.Equal(t, urth.ReasonNoEligibleRunner, stored.Labels[urth.LabelResultUnschedulable])
	require.NotNil(t, stored.Spec.TimeEnded)

	require.Zero(t, countFailures(t, db),
		"an unplaceable run is an operational state, not a dead letter")

	// The row is left for the reconciler's stale-dispatch sweep, which is what
	// retires an entry whose Result has gone terminal. Held back meanwhile so it
	// is not re-attempted on every poll of the window before that scan.
	var entry urth.DispatchOutboxEntry
	require.NoError(t, db.First(&entry).Error)
	require.Nil(t, entry.PublishedAt)
	require.Nil(t, entry.RetiredAt)
	require.WithinDuration(t, time.Now().Add(urth.PermanentDispatchBackoff), entry.NotBefore, time.Minute)
}

// Every other permanent failure is a dead letter: it is an exception, and an
// operator has to see it.
func TestRelayDeadLettersAnUndeliverableDispatch(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	created := pendingRun(t, srv, seedScenario(t, store))

	relay := urth.NewDispatchRelay(
		urth.NewDispatchOutbox(db),
		&recordingPublisher{
			err: errors.Join(urth.ErrPermanentDispatch, errors.New("failed to encode dispatch")),
		},
		urth.WithUndeliverableDispatches(urth.NewUndeliverableRecorder(store)),
	)

	_, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)

	var failures []urth.DispatchFailure
	require.NoError(t, db.Find(&failures).Error)
	require.Len(t, failures, 1)

	failure := failures[0]
	require.Equal(t, urth.ReasonUndeliverableDispatch, failure.Spec.Reason)
	require.Equal(t, urth.ReporterControlPlane, failure.Spec.ReportedBy)
	require.Equal(t, created.UID, failure.Spec.ResultUID)
	require.Equal(t, urth.DispatchEventUID(created.UID, created.Version), failure.Spec.EventUID)
	require.Contains(t, failure.Spec.Detail, "failed to encode dispatch")

	var stored urth.Result
	found, err := store.GetByUID(context.Background(), &stored, created.UID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, urth.JobErrored, stored.Status.Status)
	require.Equal(t, string(urth.ReasonUndeliverableDispatch), stored.Labels[urth.LabelResultUnschedulable])
}

// Recording is idempotent: a relay that fails the same entry twice -- because
// the row was not retired between two scans -- must not manufacture history.
func TestRelayRecordsOneDeadLetterPerDispatch(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	pendingRun(t, srv, seedScenario(t, store))

	relay := urth.NewDispatchRelay(
		urth.NewDispatchOutbox(db),
		&recordingPublisher{
			err: errors.Join(urth.ErrPermanentDispatch, errors.New("failed to encode dispatch")),
		},
		urth.WithUndeliverableDispatches(urth.NewUndeliverableRecorder(store)),
	)

	_, err := relay.RunOnce(context.Background())
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)

	// The entry is due again, which is what the second scan needs: the permanent
	// backoff would otherwise hold it back for an hour.
	require.NoError(t, db.Model(&urth.DispatchOutboxEntry{}).
		Where("1 = 1").Update("not_before", time.Now().Add(-time.Minute)).Error)

	_, err = relay.RunOnce(context.Background())
	require.ErrorIs(t, err, urth.ErrPermanentDispatch)

	require.EqualValues(t, 1, countFailures(t, db))
}
