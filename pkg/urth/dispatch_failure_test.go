package urth_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The dead-letter path is tested against a real Postgres for the same reason the
// reconciler is: what is being asserted is that a failure record and the Result
// it strands land together, that a repeat report produces one record rather than
// two, and that a retry's Result and outbox entry commit as a pair. All three are
// properties of a transaction rather than of the code around it.

// workerSession registers a worker and returns its session credential, so the
// reporting path is exercised through the same identity a real worker uses
// rather than a hand-made token.
func workerSession(t *testing.T, srv urth.Service, name manifest.ResourceName) urth.APIToken {
	t.Helper()

	ctx := context.Background()

	enrolment, found, err := srv.Runners().GetToken(ctx, "test-runner")
	require.NoError(t, err)
	require.True(t, found)

	registration, err := srv.Runners().AuthWorker(ctx, enrolment, manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance},
		Metadata: manifest.ObjectMeta{Name: name},
		Spec:     &urth.WorkerInstanceSpec{},
	})
	require.NoError(t, err)

	return registration.Session
}

// pendingRun creates a run that is placed and waiting for a worker.
func pendingRun(t *testing.T, srv urth.Service, scenarioName manifest.ResourceName) urth.Result {
	t.Helper()

	created, err := srv.Results(scenarioName).Create(context.Background(), newRunRequest())
	require.NoError(t, err)
	require.Equal(t, urth.JobPending, created.Status.Status)

	return created
}

func countFailures(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var total int64
	require.NoError(t, db.Model(&urth.DispatchFailure{}).Count(&total).Error)

	return total
}

func loadFailure(t *testing.T, store *dbstore.DBStore, name manifest.ResourceName) urth.DispatchFailure {
	t.Helper()

	var failure urth.DispatchFailure
	found, err := store.GetByName(context.Background(), &failure, name)
	require.NoError(t, err)
	require.True(t, found, "dispatch failure %q should exist", name)

	return failure
}

// TestReportStrandsThePendingRun is the headline behaviour. Before this path
// existed a permanently refused dispatch left the Result pending forever and the
// reason in a worker's log.
func TestReportStrandsThePendingRun(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")

	failure, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
		Reason:        urth.ReasonPolicyRefused,
		EventUID:      urth.DispatchEventUID(run.UID, run.Version),
		DispatchID:    urth.DispatchEventUID(run.UID, run.Version),
		ResultUID:     run.UID,
		ResultVersion: run.Version,
		Detail:        "the API permanently refused this worker's claim",
	})
	require.NoError(t, err)

	require.Equal(t, urth.ReasonPolicyRefused, failure.Spec.Reason)
	require.Equal(t, urth.ReporterWorker, failure.Spec.ReportedBy)
	require.Equal(t, run.UID, failure.Spec.ResultUID)
	require.NotEmpty(t, failure.Spec.WorkerUID, "the reporter is taken from the session")
	require.Equal(t, urth.ReasonPolicyRefused.String(), failure.Labels[urth.LabelDispatchFailureReason])
	require.Equal(t, "false", failure.Labels[urth.LabelDispatchFailureResolved])

	// `errored` rather than `timeout`: nothing ran out of time, the dispatch was
	// undeliverable.
	stranded := loadResult(t, store, run.UID)
	require.Equal(t, urth.JobErrored, stranded.Status.Status)
	require.Equal(t, prob.RunFinishedError, stranded.Status.Result)
	require.NotNil(t, stranded.Spec.TimeEnded)
	require.Equal(t, urth.ReasonPolicyRefused.String(), stranded.Labels[urth.LabelResultUnschedulable])
}

// TestDuplicateReportsRecordOneFailure covers the case a worker will actually
// produce: it reports, loses the response, and reports again rather than
// terminating a message whose failure it cannot confirm was recorded.
func TestDuplicateReportsRecordOneFailure(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")

	report := urth.ReportDispatchFailureRequest{
		Reason:        urth.ReasonPolicyRefused,
		EventUID:      urth.DispatchEventUID(run.UID, run.Version),
		ResultUID:     run.UID,
		ResultVersion: run.Version,
	}

	first, err := srv.DispatchFailures().Report(ctx, session, report)
	require.NoError(t, err)

	second, err := srv.DispatchFailures().Report(ctx, session, report)
	require.NoError(t, err)

	require.Equal(t, first.Name, second.Name)
	require.EqualValues(t, 1, countFailures(t, db))
}

// TestDifferentReasonsAreDistinctFailures guards the other half of the
// idempotency key. A dispatch that was misrouted and later exhausted its
// deliveries has told an operator two different things, and collapsing them
// would lose the second.
func TestDifferentReasonsAreDistinctFailures(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")
	eventUID := urth.DispatchEventUID(run.UID, run.Version)

	for _, reason := range []urth.DispatchFailureReason{urth.ReasonMisroutedDispatch, urth.ReasonMalformedEnvelope} {
		_, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
			Reason:    reason,
			EventUID:  eventUID,
			ResultUID: run.UID,
		})
		require.NoError(t, err)
	}

	require.EqualValues(t, 2, countFailures(t, db))
}

// TestReportDoesNotRewriteARunningRun is the "failure arrives after the run was
// claimed" case. A redelivery that failed does not entitle anyone to kill work a
// worker is actively doing.
func TestReportDoesNotRewriteARunningRun(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	claimRun(t, srv, run)

	running := loadResult(t, store, run.UID)
	require.Equal(t, urth.JobRunning, running.Status.Status)

	session := workerSession(t, srv, "test-worker")
	_, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
		Reason:    urth.ReasonPolicyRefused,
		EventUID:  urth.DispatchEventUID(run.UID, run.Version),
		ResultUID: run.UID,
	})
	require.NoError(t, err, "the failure is still recorded: the delivery really did fail")

	unchanged := loadResult(t, store, run.UID)
	require.Equal(t, urth.JobRunning, unchanged.Status.Status,
		"a claimed run is somebody's live work and must not be stranded by a stale delivery")
}

// TestReportRefusesAnUnknownReason keeps the label grammar and the idempotency
// key honest. An unrecognised reason would both fail label validation and open a
// second record for the same failure.
func TestReportRefusesAnUnknownReason(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)

	session := workerSession(t, srv, "test-worker")
	_, err := srv.DispatchFailures().Report(context.Background(), session, urth.ReportDispatchFailureRequest{
		Reason:   urth.DispatchFailureReason("something-invented"),
		EventUID: "event-1",
	})

	require.ErrorIs(t, err, urth.ErrInvalidDispatchFailure)
	require.Zero(t, countFailures(t, db))
}

// TestReportRequiresAWorkerSession proves identity comes from the credential.
// The prototype's claim endpoint trusted the request body; this path must not
// repeat that, since a report strands a run.
func TestReportRequiresAWorkerSession(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)

	_, err := srv.DispatchFailures().Report(context.Background(), "not-a-session",
		urth.ReportDispatchFailureRequest{
			Reason:   urth.ReasonPolicyRefused,
			EventUID: "event-1",
		})

	require.Error(t, err)
	disposition, ok := urth.ClaimDispositionOf(err)
	require.True(t, ok)
	require.Equal(t, urth.ClaimForbidden, disposition)
	require.Zero(t, countFailures(t, db))
}

// TestRetryCreatesANewRunAndDispatch covers the requirement ADR 0004 states
// directly: a retry creates a new Result and never reopens the failed one.
func TestRetryCreatesANewRunAndDispatch(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")

	reported, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
		Reason:        urth.ReasonPolicyRefused,
		EventUID:      urth.DispatchEventUID(run.UID, run.Version),
		ResultUID:     run.UID,
		ResultVersion: run.Version,
	})
	require.NoError(t, err)

	before := countOutbox(t, db)

	failure, retry, err := srv.DispatchFailures().Retry(ctx, reported.Name, urth.RetryDispatchFailureRequest{})
	require.NoError(t, err)

	require.NotEqual(t, run.UID, retry.UID, "a retry is a new run")
	require.Equal(t, urth.JobPending, retry.Status.Status)
	require.Equal(t, before+1, countOutbox(t, db), "a retry commits its dispatch with the run")

	// The execution input is copied, never re-derived: the retry must run what
	// the failed attempt was asked to run.
	require.Equal(t, run.Spec.Execution.ScenarioUID, retry.Spec.Execution.ScenarioUID)
	require.Equal(t, run.Spec.Execution.ScenarioVersion, retry.Spec.Execution.ScenarioVersion)
	require.False(t, retry.Spec.Execution.IsZero())

	// Placement is inherited rather than recomputed.
	require.Equal(t, run.Status.Executor.RunnerID, retry.Status.Executor.RunnerID)

	// The relation is traceable in both directions.
	require.Equal(t, retry.UID, failure.Status.RetryResultUID)
	require.Equal(t, retry.Name, failure.Status.RetryResultName)
	require.Equal(t, string(run.UID), retry.Labels[urth.LabelRetryOfResult])
	require.True(t, failure.Status.Resolved)

	// The failed attempt is untouched history.
	original := loadResult(t, store, run.UID)
	require.Equal(t, urth.JobErrored, original.Status.Status)
	require.NotNil(t, original.Spec.TimeEnded)
}

// TestRetryIsNotRepeatable stops a double-click scheduling two runs.
func TestRetryIsNotRepeatable(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")

	reported, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
		Reason:    urth.ReasonPolicyRefused,
		EventUID:  urth.DispatchEventUID(run.UID, run.Version),
		ResultUID: run.UID,
	})
	require.NoError(t, err)

	_, first, err := srv.DispatchFailures().Retry(ctx, reported.Name, urth.RetryDispatchFailureRequest{})
	require.NoError(t, err)

	outboxAfterFirst := countOutbox(t, db)

	_, second, err := srv.DispatchFailures().Retry(ctx, reported.Name, urth.RetryDispatchFailureRequest{})
	require.NoError(t, err, "asking twice is not an error; the caller wanted a retry to exist")

	require.Equal(t, first.UID, second.UID, "the second retry returns the first")
	require.Equal(t, outboxAfterFirst, countOutbox(t, db), "no second dispatch is enqueued")
}

// TestResolveClosesAFailureWithoutRetrying covers the other operator action: a
// failure they have decided needs no re-run.
func TestResolveClosesAFailureWithoutRetrying(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)
	session := workerSession(t, srv, "test-worker")

	reported, err := srv.DispatchFailures().Report(ctx, session, urth.ReportDispatchFailureRequest{
		Reason:    urth.ReasonPolicyRefused,
		EventUID:  urth.DispatchEventUID(run.UID, run.Version),
		ResultUID: run.UID,
	})
	require.NoError(t, err)

	before := countOutbox(t, db)

	resolved, err := srv.DispatchFailures().Resolve(ctx, reported.Name)
	require.NoError(t, err)

	require.True(t, resolved.Status.Resolved)
	require.NotNil(t, resolved.Status.ResolvedAt)
	require.Empty(t, resolved.Status.RetryResultUID)
	require.Equal(t, "true", resolved.Labels[urth.LabelDispatchFailureResolved])
	require.Equal(t, before, countOutbox(t, db), "resolving schedules nothing")
}

// TestAdvisoryRecordsAnAbandonedDispatch covers the one category no worker can
// report: the broker giving up on redelivery, which a work-queue stream does not
// otherwise surface at all.
func TestAdvisoryRecordsAnAbandonedDispatch(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)

	// The relay published it: the outbox row now addresses a broker message.
	const streamSeq = 4242
	require.NoError(t, db.Model(&urth.DispatchOutboxEntry{}).
		Where("result_uid = ?", run.UID).
		Updates(map[string]any{
			"published_at":  time.Now(),
			"published_seq": streamSeq,
		}).Error)

	recorder := urth.NewAdvisoryRecorder(db, store)
	require.NoError(t, recorder.RecordMaxDelivery(ctx, urth.DispatchAdvisory{
		StreamSequence: streamSeq,
		Deliveries:     5,
		Channel:        "urth-runner-1",
	}))

	require.EqualValues(t, 1, countFailures(t, db))

	failure := loadFailure(t, store,
		urth.DispatchFailureName(urth.DispatchEventUID(run.UID, run.Version), urth.ReasonMaxDeliveryExhausted))
	require.Equal(t, urth.ReporterControlPlane, failure.Spec.ReportedBy)
	require.Equal(t, 5, failure.Spec.Deliveries)
	require.Equal(t, run.UID, failure.Spec.ResultUID)

	stranded := loadResult(t, store, run.UID)
	require.Equal(t, urth.JobErrored, stranded.Status.Status)
}

// TestDuplicateAdvisoriesRecordOneFailure matters because advisories are
// at-most-once *per subscriber* and every replica subscribes. Several api-servers
// seeing the same event must converge on one record rather than one each.
func TestDuplicateAdvisoriesRecordOneFailure(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	scenarioName := seedScenario(t, store)
	ctx := context.Background()

	run := pendingRun(t, srv, scenarioName)

	const streamSeq = 77
	require.NoError(t, db.Model(&urth.DispatchOutboxEntry{}).
		Where("result_uid = ?", run.UID).
		Updates(map[string]any{"published_at": time.Now(), "published_seq": streamSeq}).Error)

	advisory := urth.DispatchAdvisory{StreamSequence: streamSeq, Deliveries: 5, Channel: "urth-runner-1"}

	for range 3 {
		require.NoError(t, urth.NewAdvisoryRecorder(db, store).RecordMaxDelivery(ctx, advisory))
	}

	require.EqualValues(t, 1, countFailures(t, db))
}

// TestAdvisoryForAnUnknownMessageIsIgnored keeps the control plane from
// inventing history for a dispatch it never made -- another deployment sharing
// the stream, or a row since pruned.
func TestAdvisoryForAnUnknownMessageIsIgnored(t *testing.T) {
	_, db, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)

	require.NoError(t, urth.NewAdvisoryRecorder(db, store).RecordMaxDelivery(context.Background(),
		urth.DispatchAdvisory{StreamSequence: 999999, Deliveries: 5}))

	require.Zero(t, countFailures(t, db))
}
