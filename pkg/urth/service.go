package urth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// LabelsAPI models helper APIs to access resource names and label to power search
type LabelsAPI interface {
	ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error)
	ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error)
	ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error)
}

type ReadableResourceAPI[T any] interface {
	// List all resources matching given search query
	List(ctx context.Context, searchQuery manifest.SearchQuery) (result []T, total int64, err error)

	// Get a single resource given its unique ID,
	// Returns a resource if it exists, false, if resource doesn't exists
	// error if there was communication error with the storage
	Get(ctx context.Context, id manifest.ResourceName) (resource T, exists bool, commError error)
}

type ManageableResourceAPI interface {
	CreateOrUpdate(ctx context.Context, entry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error)

	// Create attempts to create a new resource (Scenario) based on the manifest provided
	Create(ctx context.Context, entry manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// Update a single resource identified by a unique ID
	Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error)
}

// RunnersAPI encapsulate APIs to interacting with `Runners`
type RunnersAPI interface {
	ReadableResourceAPI[manifest.ResourceManifest]
	ManageableResourceAPI

	// GetToken generates a JWT token for a worker instance to auth as a Runner
	GetToken(ctx context.Context, runID manifest.ResourceName) (APIToken, bool, error)

	// Authenticate a worker and receive Identity from the server
	Auth(ctx context.Context, token APIToken, worker manifest.ResourceManifest) (manifest.ResourceManifest, error)
}

// WorkersAPI encapsulates APIs for the worker instances that have registered
// against a runner.
//
// The surface is deliberately narrow. A worker instance is not something an
// operator creates -- it comes into existence when a process authenticates with
// a runner's token -- so there is no Create or Update here. What an operator
// needs is to see who has registered, to take one out of service, and to revoke
// one that should not be there.
type WorkersAPI interface {
	ReadableResourceAPI[manifest.ResourceManifest]

	// SetPaused stops or resumes a single worker taking new jobs, leaving it
	// registered and its runner untouched. Reports false if no such worker is
	// registered, so that a stale name reads as "not found" rather than as a
	// failed request.
	SetPaused(ctx context.Context, id manifest.ResourceName, paused bool) (manifest.ResourceManifest, bool, error)

	// Delete revokes a worker's registration. The worker keeps its token and can
	// register again unless its runner is disabled or its token revoked, so this
	// is how a worker is dropped, not how it is permanently barred.
	Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error)
}

type ScenarioAPI interface {
	ReadableResourceAPI[manifest.ResourceManifest]
	ManageableResourceAPI

	UpdateScript(ctx context.Context, id manifest.VersionedResourceID, entry prob.Manifest) (bark.CreatedResponse, bool, error)
}

type RunResultAPI interface {
	ReadableResourceAPI[Result]

	Create(ctx context.Context, entry manifest.ResourceManifest) (Result, error)

	// Auth(ctx context.Context, runID manifest.VersionedResourceID, authRequest AuthJobRequest) (AuthJobResponse, error)
	Auth(ctx context.Context, runID manifest.ResourceName, authRequest AuthJobRequest) (AuthJobResponse, error)

	// TODO: Token can be used to look-up ID!
	UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token APIToken, entry ResultStatus) (bark.CreatedResponse, error)
}

// RunResultsAPI reads run results across all scenarios.
//
// This is separate from RunResultAPI, which is scoped to one scenario and also
// creates and updates runs. Answering "what has run recently, anywhere" and
// "what happened in this scenario" are different questions: the first is how an
// operator finds a failure they have not been told the name of yet.
//
// It is read-only by construction. A run comes into existence by being scheduled
// against a scenario, so there is nothing to create here.
type RunResultsAPI interface {
	ReadableResourceAPI[Result]
}

type ArtifactAPI interface {
	ReadableResourceAPI[manifest.ResourceManifest]

	// Create create a new artifact resource to allow storage of artifact produced during script execution
	// Only authorized [Runners] are allowed to create artifacts
	Create(ctx context.Context, token APIToken, entry manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error)

	GetContent(ctx context.Context, id manifest.ResourceName) (resource ArtifactSpec, exists bool, commError error)
}

type Service interface {
	// GetLabels returns APIs to access names/labels/label values to power resource search
	Labels(manifest.Kind) LabelsAPI

	Runners() RunnersAPI
	Workers() WorkersAPI
	Scenarios() ScenarioAPI
	Results(scenarioName manifest.ResourceName) RunResultAPI

	// AllResults reads runs across every scenario.
	AllResults() RunResultsAPI

	Artifacts() ArtifactAPI
}

func NewService(store *dbstore.DBStore, scheduler Scheduler) Service {
	return &serviceImpl{
		store:     store,
		scheduler: scheduler,
	}
}

type (
	serviceImpl struct {
		store     *dbstore.DBStore
		scheduler Scheduler
	}
)

func (s *serviceImpl) Runners() RunnersAPI {
	return &runnersAPIImpl{
		store:            s.store,
		hmacSampleSecret: []byte("my_secret_key"), // FIXME: Must be Runtime configurable secret
	}
}

func (s *serviceImpl) Workers() WorkersAPI {
	return &workersAPIImpl{
		store: s.store,
	}
}

func (s *serviceImpl) Scenarios() ScenarioAPI {
	return &scenarioAPIImpl{
		store: s.store,
	}
}

func (s *serviceImpl) Results(scenarioName manifest.ResourceName) RunResultAPI {
	return &resultsAPIImpl{
		store:      s.store,
		scenarioID: scenarioName,
		scheduler:  s.scheduler,
		workersAPI: &runnersAPIImpl{
			store: s.store,
		},

		resultsSigningKey: []byte("my_results signing secret key, duh"), // FIXME: Must be Runtime configurable secret
	}
}

func (s *serviceImpl) AllResults() RunResultsAPI {
	return &allResultsAPIImpl{
		store: s.store,
	}
}

func (s *serviceImpl) Artifacts() ArtifactAPI {
	return &artifactAPIImp{
		store: s.store,

		resultsSigningKey: []byte("my_results signing secret key, duh"), // FIXME: Must be Runtime configurable secret
	}
}

func (s *serviceImpl) Labels(k manifest.Kind) LabelsAPI {
	return &labelsAPIImpl{
		kind:  k,
		store: s.store,
	}
}

// ------------------------------
// / Scenarios API
// ------------------------------
type scenarioAPIImpl struct {
	store dbstore.TransactionalStore
}

func (m *scenarioAPIImpl) List(ctx context.Context, query manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []Scenario

	total, err = m.store.Find(ctx, &models, query, dbstore.OrderByCreatedAt(dbstore.OrderAscending)) //, dbstore.Expand("Results", manifest.SearchQuery{
	// Limit: 1,
	// })) // , dbstore.Omit("Prob.Spec")) - omit doesn't work on a json serialized field
	if err != nil {
		return
	}

	results = make([]manifest.ResourceManifest, 0, len(models))
	for _, model := range models {
		// TODO: Script should be moved into a separate table, that way we won't have to filter it out here
		model.Spec.Prob.Spec = nil
		results = append(results, model.ToManifest())
	}

	return
}

func (m *scenarioAPIImpl) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
	var model Scenario
	exist, err = m.store.GetByName(ctx, &model, id, dbstore.Expand("Results", manifest.SearchQuery{
		Limit: 1,
	}))
	if err != nil {
		return
	}
	result = model.ToManifest()

	return
}

func (m *scenarioAPIImpl) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	scenario, err := NewScenario(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, false, err
	}

	var existEntry Scenario
	if exist, err := m.store.GetByName(ctx, &existEntry, newEntry.Metadata.Name); err != nil {
		return manifest.ResourceManifest{}, false, err
	} else if !exist { // Easy-peasy - such name is not takes, try to create a new entry
		result, err := m.create(ctx, scenario)
		return result.ToManifest(), !exist, err
	}

	result, err := m.update(ctx, existEntry.GetVersionedID(), scenario)
	return result.ToManifest(), false, err
}

func (m *scenarioAPIImpl) create(ctx context.Context, newEntry Scenario) (Scenario, error) {
	err := m.store.Create(ctx, &newEntry)
	return newEntry, err
}

func (m *scenarioAPIImpl) update(ctx context.Context, id manifest.VersionedResourceID, entry Scenario) (Scenario, error) {
	// Find target entry to be updated
	var result Scenario
	if ok, err := m.store.GetByUID(ctx, &result, id.ID, dbstore.WithVersion(id.Version)); err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceNotFound
	}

	// FIXME: Move to a metadata update util function in wyrd/manifest
	// Identity check
	if result.Name != entry.Name {
		return entry, bark.ErrResourceNotFound
	}

	result.Spec = entry.Spec

	// TODO: Update system labels!
	result.Labels = entry.Labels
	result.Labels = manifest.MergeLabels(
		entry.Labels,
		manifest.Labels{
			LabelScenarioKind: string(result.Spec.Prob.Kind),
		},
	)

	log.Printf("updating scenario: prod.kind: %q, prod.type %q", result.Spec.Prob.Kind, reflect.TypeOf(result.Spec.Prob.Spec))
	// saveResource, not Update: a scenario being switched to active=false is a
	// zero value, which Update drops. See saveResource.
	if err := saveResource(ctx, m.store, &result); err != nil {
		return result, err
	}

	return result, nil
}

func (m *scenarioAPIImpl) Create(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewScenario(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.create(ctx, entry)
	return result.ToManifest(), err
}

func (m *scenarioAPIImpl) Update(ctx context.Context, id manifest.VersionedResourceID, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewScenario(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.update(ctx, id, entry)
	return result.ToManifest(), err
}

func (m *scenarioAPIImpl) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return m.store.Delete(ctx, &Scenario{}, id.ID, id.Version)
}

func (m *scenarioAPIImpl) UpdateScript(ctx context.Context, id manifest.VersionedResourceID, prob prob.Manifest) (bark.CreatedResponse, bool, error) {
	var result Scenario
	if ok, err := m.store.GetByUID(ctx, &result, id.ID, dbstore.WithVersion(id.Version)); !ok || err != nil {
		return bark.CreatedResponse{}, ok, err
	}

	result.Spec.Prob = prob
	ok, err := m.store.Update(ctx, &result, result.UID, dbstore.WithVersion(result.Version))

	return bark.CreatedResponse{
		TypeMeta:            manifest.TypeMeta{Kind: KindScenario},
		VersionedResourceID: result.GetVersionedID(),
	}, ok, err
}

// ------------------------------
// / Scenarios run results
// ------------------------------
type resultsAPIImpl struct {
	store      *dbstore.DBStore
	scenarioID manifest.ResourceName
	scheduler  Scheduler
	workersAPI *runnersAPIImpl

	resultsSigningKey []byte
}

func (m *resultsAPIImpl) scheduleRun(ctx context.Context, runResult Result) (RunID, error) {
	if m.scheduler == nil || m.workersAPI == nil {
		return InvalidRunID, nil
	}

	if runResult.Spec.Scenario.Name == "" {
		return InvalidRunID, fmt.Errorf("internal scheduling error: results.scenario has no name")
	}

	// Check if scenario is enabled!
	if !runResult.Spec.Scenario.Spec.IsActive {
		return InvalidRunID, nil
	}

	// Find all workers qualified to run the scenario:
	requirement := runResult.Spec.Scenario.Spec.Requirements.AsLabels()
	requirementsSelector, err := manifest.ParseSelector(requirement)
	if err != nil {
		return InvalidRunID, fmt.Errorf("failed to parse scenario requirements: %w", err)
	}

	// TODO: Its scheduler responsibility to match scenario to a worker. Move it there.
	log.Printf("Scheduling scenario: looking for workers that match: %q", requirement)
	workers, totalWorkers, err := m.workersAPI.List(ctx, manifest.SearchQuery{
		Selector: requirementsSelector,
	})
	if err != nil {
		return InvalidRunID, fmt.Errorf("failed to list workers to schedule a scenario: %w", err)
	}

	log.Printf("Scheduling scenario: %v (active=%t); qualified workers: %d / %d qualified", runResult.Spec.Scenario.Name, runResult.Spec.Scenario.Spec.IsActive, len(workers), totalWorkers)
	return m.scheduler.Schedule(ctx, runResult, runResult.Spec.Scenario) //scenarioToRunnable(runResult, scenario))
}

func (m *resultsAPIImpl) List(ctx context.Context, searchQuery manifest.SearchQuery) (results []Result, total int64, err error) {
	var scenario Scenario
	if exist, err := m.store.GetByName(ctx, &scenario, m.scenarioID); err != nil {
		return nil, 0, fmt.Errorf("failed to load required scenario: %w", err)
	} else if !exist {
		return nil, 0, bark.ErrResourceNotFound
	}

	total, err = m.store.FindLinked(ctx, &results, "Results", &scenario, searchQuery)
	return
}

func (m *resultsAPIImpl) Create(ctx context.Context, newEntry manifest.ResourceManifest) (Result, error) {
	// scenarioIdLabelValue := string(m.scenarioId)
	// // Validate that the Result is labeled with the correct Scenario ID, if any
	// if v, ok := newEntry.Metadata.Labels[LabelScenarioId]; ok && v != scenarioIdLabelValue {
	// 	return Result{}, fmt.Errorf("invalid scenario ID for the given results entry")
	// }

	// Its ok to post Results without a name, in this case - we will generate a new one:
	if newEntry.Metadata.Name == "" || strings.HasPrefix(string(newEntry.Metadata.Name), "manual-") { // Generate run name for scheduled runs
		// log.Print("manual run, prefix: ", newEntry.Metadata.Name)
		// newEntry.Metadata.Name = manifest.ResourceName(fmt.Sprintf("%v%v-v%v-%v", newEntry.Metadata.Name, scenario.Name, scenario.Version, randToken(32)))
		newEntry.Metadata.Name = manifest.ResourceName(strings.ToLower(fmt.Sprintf("%v%v", newEntry.Metadata.Name, NewRandToken(16))))
		log.Print("manual run, generated name: ", newEntry.Metadata.Name)
	}

	entry, err := NewResult(newEntry)
	if err != nil {
		return entry, err
	}

	// Ensure start timestamp is unset:
	if entry.Spec.TimeStarted != nil {
		now := time.Now()
		entry.Spec.TimeStarted = &now
	}

	// Ensure end time is unset:
	if entry.Spec.TimeEnded != nil {
		// Can't post to create a completed jobs
		return Result{}, bark.ErrForbidden
	}

	// Fetch Scenario to create a new run request
	// scenario, ok, err := m.scenarioApi.Get(ctx, m.scenarioId)
	if scenarioExist, err := m.store.GetByName(ctx, &entry.Spec.Scenario, m.scenarioID); err != nil {
		return Result{}, err
	} else if !scenarioExist {
		return Result{}, bark.ErrResourceNotFound
	}

	// Check if scenario is active and enabled for scheduling
	if !entry.Spec.Scenario.Spec.IsActive {
		return Result{}, bark.ErrForbidden
	}

	// Check if a scenario has a prob section, otherwise it can't be scheduled
	if entry.Spec.Scenario.Spec.Prob.Kind == "" || entry.Spec.Scenario.Spec.Prob.Spec == nil {
		return Result{}, bark.ErrForbidden
	}

	// Should we override of just trust the value passed in?
	entry.Spec.ProbKind = entry.Spec.Scenario.Spec.Prob.Kind

	// Ensure initial status is set to pending
	entry.Status = ResultStatus{
		Status: JobPending,
		Result: prob.RunNotFinished,
	}

	// Ensure labels are set correctly
	entry.Labels = manifest.MergeLabels(
		// scenario.Labels,
		entry.Labels,
		manifest.Labels{
			LabelScenarioName:    string(entry.Spec.Scenario.Name),
			LabelScenarioUID:     string(entry.Spec.Scenario.UID),
			LabelScenarioVersion: entry.Spec.Scenario.Version.String(),
			LabelScenarioKind:    string(entry.Spec.ProbKind),

			LabelResultJobState: string(entry.Status.Status),
			// LabelResultStatus: string(entry.Status.Result),
		},
	)

	// TODO: Validate that request is from an authentic worker that is allowed to take jobs!
	if err := m.store.Create(ctx, &entry); err != nil {
		return Result{}, err
	}

	// FIXME: Its scheduler responsibility to react to a newly created run-request and schedule it.
	// Thus it should be removed from here once we have the scheduler as a stand-alone service.
	if _, err = m.scheduleRun(ctx, entry); err != nil {
		// Well, scheduling failed. Might as well cancel it:
		entry.Status.Status = JobErrored
		// TODO: Update metrics!
		if _, uerr := m.store.Update(ctx, &entry, entry.UID, dbstore.WithVersion(entry.Version)); uerr != nil {
			log.Print("embarrassing error: failed to update run DB entry after failure to schedule it: ", uerr)
		}
		// Note: we do want to return original error, to know why we failed to schedule in a first place
		return Result{}, err
	}

	return entry, err
}

// executorRef builds the record of who is executing a run, from the worker that
// just authenticated and the runner it belongs to.
func executorRef(worker WorkerInstance, runner Runner) ExecutorRef {
	return ExecutorRef{
		RunnerID:   worker.Spec.RunnerID,
		RunnerName: runner.Name,
		WorkerID:   worker.UID,
		WorkerName: worker.Name,
	}
}

// workerLabels ties a worker instance back to its runner, so that "the workers
// claiming to be this runner" is a label query. Without it the association is
// only a foreign key, which the search API cannot reach.
func workerLabels(runner Runner) manifest.Labels {
	labels := manifest.Labels{}

	putLabel(labels, LabelRunnerName, string(runner.Name))
	putLabel(labels, LabelRunnerUID, string(runner.UID))

	return labels
}

// executorLabels exposes the executor as labels, so that "every run this worker
// took" is a label query rather than a scan. Artifacts already carry the same
// keys, which keeps a run and its output selectable the same way.
func executorLabels(executor ExecutorRef) manifest.Labels {
	labels := manifest.Labels{}

	putLabel(labels, LabelRunnerName, string(executor.RunnerName))
	putLabel(labels, LabelRunnerUID, string(executor.RunnerID))
	putLabel(labels, LabelWorkerName, string(executor.WorkerName))
	putLabel(labels, LabelWorkerUID, string(executor.WorkerID))

	return labels
}

func (m *resultsAPIImpl) Auth(ctx context.Context, resultName manifest.ResourceName, authRequest AuthJobRequest) (AuthJobResponse, error) {
	var worker WorkerInstance
	if ok, err := m.store.GetByUID(ctx, &worker, authRequest.WorkerID.ID); err != nil {
		log.Print("error while looking up Worker ", authRequest.WorkerID.ID, " err", err)
		return AuthJobResponse{}, bark.ErrResourceNotFound
	} else if !ok {
		log.Print("no Worker manifest found by ID ", authRequest.WorkerID.ID)
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	// Validate that worker if for the right Runner:
	if authRequest.RunnerID.ID != worker.Spec.RunnerID {
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	// Business Rule: a paused worker stays registered and keeps its identity,
	// but takes no new jobs. This is the check that makes pausing mean
	// something -- without it the flag is a label on a worker that carries on
	// working.
	if worker.Status.IsPaused {
		log.Printf("worker %q is paused and may not take job %q", worker.Name, resultName)
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	// The runner is loaded here rather than through worker.Spec.Runner, which is
	// a lazy association and arrives zero-valued from GetByUID -- reading
	// IsActive off it would reject every worker.
	var runner Runner
	if ok, err := m.store.GetByUID(ctx, &runner, worker.Spec.RunnerID); err != nil {
		log.Print("error while looking up Runner ", worker.Spec.RunnerID, " err", err)
		return AuthJobResponse{}, bark.ErrResourceNotFound
	} else if !ok {
		log.Print("no Runner found by ID ", worker.Spec.RunnerID)
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	// Business Rule: a worker of a disabled runner takes no jobs either.
	// Disabling a runner already stops new workers registering; without this it
	// would not stop the ones already connected, so a runner could be "disabled"
	// and still executing work.
	if !runner.Spec.IsActive {
		log.Printf("runner %q is not active; worker %q may not take job %q", runner.Name, worker.Name, resultName)
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	var entry Result
	if ok, err := m.store.GetByName(ctx, &entry, resultName); err != nil {
		log.Print("error while looking up Results Object", resultName, "err", err)
		return AuthJobResponse{}, bark.ErrResourceNotFound
	} else if !ok {
		log.Print("not found Results Object", resultName)
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	// Check that no one else took this job
	// Note: This means that no re-try is possible!
	if entry.Status.Status != JobPending {
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}

	// Ensure start timestamp is set:
	if entry.Spec.TimeStarted != nil {
		return AuthJobResponse{}, bark.ErrResourceUnauthorized
	}
	now := time.Now()

	// Update start time
	entry.Spec.TimeStarted = &now

	// TODO: Record expected deadline and JWT's exp claim
	entry.Status.Status = JobRunning

	// Record who took the job. This is the only moment the association is known
	// for certain -- the server has just authenticated this worker and checked
	// it belongs to the runner the job was dispatched to.
	entry.Status.Executor = executorRef(worker, runner)

	entry.Labels = manifest.MergeLabels(
		entry.Labels,
		// authRequest.Labels,
		// Set labels to reflect results pending status
		manifest.Labels{
			LabelResultJobState: string(entry.Status.Status),
		},
		executorLabels(entry.Status.Executor),
	)

	// Generate JWT with valid-until clause, to give worker a time to post
	claims := &jwt.RegisteredClaims{
		// ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(authRequest.Timeout)),
		Subject:   string(entry.UID),
		// Issuer: ,
		// ID: ,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.resultsSigningKey)
	if err != nil {
		return AuthJobResponse{}, fmt.Errorf("failed to sign an auth token: %w", err)
	}

	log.Print("authorizing worker ", authRequest.RunnerID, " to execute ", entry.Name, " for at most ", authRequest.Timeout)
	if ok, err := m.store.Update(ctx, &entry, entry.UID, dbstore.WithVersion(entry.Version)); err != nil {
		return AuthJobResponse{}, err
	} else if !ok {
		// If version update failed, it means that someone else bit us to it and took the job
		return AuthJobResponse{}, bark.ErrResourceUnauthorized // ErrResourceVersionConflict
	}

	return AuthJobResponse{
		CreatedResponse: bark.CreatedResponse{
			VersionedResourceID: entry.GetVersionedID(),
		},
		Token: APIToken(tokenString), // NewRandToken(32), //entry.UpdateToken,
	}, err
}

func (m *resultsAPIImpl) validateUpdateRequest(_ context.Context, entry Result, bearerToken APIToken) error {
	token, err := jwt.Parse(string(bearerToken), func(token *jwt.Token) (interface{}, error) {
		// Don't forget to validate the alg is what you expect:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		// return m.hmacSampleSecret, nil
		return m.resultsSigningKey, nil // FIXME: Terribly insecure way to confirm token signature. Should use results auth-token
	})
	if err != nil {
		return bark.ErrResourceUnauthorized
	}

	if token.Claims == nil {
		return bark.ErrResourceUnauthorized
	}

	subj, err := token.Claims.GetSubject()
	if err != nil {
		return bark.ErrResourceUnauthorized
	}

	if subj != string(entry.UID) {
		return bark.ErrResourceUnauthorized
	}

	// TODO: Do more validation!
	return nil
}

func (m *resultsAPIImpl) UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token APIToken, runResults ResultStatus) (bark.CreatedResponse, error) {
	var entry Result
	if ok, err := m.store.GetByUID(ctx, &entry, id.ID, dbstore.WithVersion(id.Version)); err != nil {
		return bark.CreatedResponse{}, bark.ErrResourceNotFound
	} else if !ok {
		return bark.CreatedResponse{}, bark.ErrResourceVersionConflict
	}

	// Validate API Token
	if validationErr := m.validateUpdateRequest(ctx, entry, token); validationErr != nil {
		return bark.CreatedResponse{}, validationErr
	}

	now := time.Now()
	entry.Spec.TimeEnded = &now
	entry.Status.Status = JobCompleted
	entry.Status.Result = runResults.Result

	entry.Labels = manifest.MergeLabels(
		entry.Labels,
		// Set labels to reflect results pending status
		manifest.Labels{
			// TODO: Add duration!
			// Note: executor labels are set when the job is claimed, in Auth.
			LabelResultJobState: string(entry.Status.Status),
			LabelResultStatus:   string(entry.Status.Result),
		},
	)

	if ok, err := m.store.Update(ctx, &entry, entry.UID, dbstore.WithVersion(entry.Version)); err != nil {
		return bark.CreatedResponse{}, err
	} else if !ok {
		return bark.CreatedResponse{}, bark.ErrResourceVersionConflict
	}

	return bark.CreatedResponse{
		TypeMeta:            entry.ToManifest().TypeMeta,
		VersionedResourceID: entry.GetVersionedID(),
	}, nil
}

func (m *resultsAPIImpl) Get(ctx context.Context, id manifest.ResourceName) (result Result, exist bool, err error) {
	exist, err = m.store.GetByName(ctx, &result, id)
	return
}

// ------------------------------
// / Runners resources API
// ------------------------------
type runnersAPIImpl struct {
	store            dbstore.TransactionalStore
	hmacSampleSecret []byte
}

func (m *runnersAPIImpl) List(ctx context.Context, searchQuery manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []Runner
	total, err = m.store.Find(ctx, &models, searchQuery, dbstore.OrderByCreatedAt(dbstore.OrderAscending))
	if err != nil {
		return
	}

	results = make([]manifest.ResourceManifest, 0, len(models))
	for _, model := range models {
		results = append(results, model.ToManifest())
	}
	return
}

func (m *runnersAPIImpl) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
	var model Runner
	exist, err = m.store.GetByName(ctx, &model, id)
	result = model.ToManifest()
	return
}

func (m *runnersAPIImpl) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	runner, err := NewRunner(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, false, err
	}

	var existEntry Runner
	if exist, err := m.store.GetByName(ctx, &existEntry, runner.Name); err != nil {
		return manifest.ResourceManifest{}, false, err
	} else if !exist { // Easy-peasy - such name is not takes, try to create a new entry
		result, err := m.create(ctx, runner)
		return result.ToManifest(), true, err
	}

	result, err := m.update(ctx, existEntry.GetVersionedID(), runner)
	return result.ToManifest(), false, err
}

func (m *runnersAPIImpl) create(ctx context.Context, newEntry Runner) (Runner, error) {
	// TODO: Generate auth token?
	// 	IdToken: randToken(16),

	// Validate runner's requirements
	if _, err := newEntry.Spec.Requirements.AsSelector(); err != nil {
		// Note, failed to parse Runner's requirements so wont be able auth any workers
		return newEntry, fmt.Errorf("runner's requirements are invalid: %v", err)
	}

	err := m.store.Create(ctx, &newEntry)
	return newEntry, err
}
func (m *runnersAPIImpl) update(ctx context.Context, id manifest.VersionedResourceID, newEntry Runner) (Runner, error) {
	var result Runner
	if ok, err := m.store.GetByUID(ctx, &result, id.ID, dbstore.WithVersion(id.Version)); err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceVersionConflict
	}

	// Identity check
	if result.Name != newEntry.Name {
		return result, bark.ErrResourceNotFound
	}

	// Validate runner's requirements
	if _, err := newEntry.Spec.Requirements.AsSelector(); err != nil {
		// Note, failed to parse Runner's requirements so wont be able auth any workers
		return newEntry, fmt.Errorf("runner's requirements are invalid: %v", err)
	}

	result.Labels = newEntry.Labels
	result.Spec = newEntry.Spec

	// Note: workers of a disabled runner are stopped at the point they try to
	// claim a job, rather than by disabling each instance here. See Results.Auth.

	// Persist changes. saveResource, not Update: a runner being switched to
	// active=false is a zero value, which Update drops. See saveResource.
	if err := saveResource(ctx, m.store, &result); err != nil {
		return result, err
	}

	return result, nil
}

func (m *runnersAPIImpl) Create(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewRunner(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.create(ctx, entry)
	return result.ToManifest(), err
}

func (m *runnersAPIImpl) Update(ctx context.Context, id manifest.VersionedResourceID, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewRunner(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.update(ctx, id, entry)
	return result.ToManifest(), err
}

func (m *runnersAPIImpl) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return m.store.Delete(ctx, &Runner{}, id.ID, id.Version)
}

// ------------------------------
// / RunResultsAPI implementation (across scenarios)
// ------------------------------
type allResultsAPIImpl struct {
	store *dbstore.DBStore
}

// List returns runs newest first. A run list is read to find what just happened,
// so the most recent runs belong at the top -- unlike the resource lists, which
// are ordered oldest first because their order is meant to be stable.
func (m *allResultsAPIImpl) List(ctx context.Context, searchQuery manifest.SearchQuery) (results []Result, total int64, err error) {
	total, err = m.store.Find(ctx, &results, searchQuery, dbstore.OrderByCreatedAt(dbstore.OrderDescending))
	return
}

// Get finds a run by name without knowing which scenario it belongs to. Run
// names are generated to be unique, so a run can be linked to directly.
func (m *allResultsAPIImpl) Get(ctx context.Context, id manifest.ResourceName) (result Result, exists bool, err error) {
	exists, err = m.store.GetByName(ctx, &result, id)
	return
}

// ------------------------------
// / WorkersAPI implementation
// ------------------------------
type workersAPIImpl struct {
	store *dbstore.DBStore
}

func (m *workersAPIImpl) List(ctx context.Context, searchQuery manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []WorkerInstance
	total, err = m.store.Find(ctx, &models, searchQuery, dbstore.OrderByCreatedAt(dbstore.OrderAscending))
	if err != nil {
		return
	}

	results = make([]manifest.ResourceManifest, 0, len(models))
	for _, model := range models {
		results = append(results, model.ToManifest())
	}

	return
}

func (m *workersAPIImpl) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
	var model WorkerInstance
	exist, err = m.store.GetByName(ctx, &model, id)
	result = model.ToManifest()

	return
}

// SetPaused takes a single worker out of service, or puts it back.
//
// The flag lives in Status, which a re-registering worker does not overwrite, so
// a paused worker stays paused when it reconnects. Read-modify-write is used
// rather than a blind update so that the rest of the worker's record -- which
// the worker itself owns and rewrites on every registration -- is left alone.
func (m *workersAPIImpl) SetPaused(ctx context.Context, id manifest.ResourceName, paused bool) (manifest.ResourceManifest, bool, error) {
	var worker WorkerInstance
	if exist, err := m.store.GetByName(ctx, &worker, id); err != nil {
		return manifest.ResourceManifest{}, false, err
	} else if !exist {
		return manifest.ResourceManifest{}, false, nil
	}

	if worker.Status.IsPaused == paused {
		// Nothing to do. Returning early avoids bumping the resource version for
		// a request that changes nothing.
		return worker.ToManifest(), true, nil
	}

	worker.Status.IsPaused = paused

	// CreateOrUpdate rather than Update, and not by preference: Update passes the
	// struct to gorm's Updates, which ignores zero-valued fields. Pausing (false
	// -> true) would persist while resuming (true -> false) silently did nothing,
	// leaving a worker that could be taken out of service and never brought back.
	// CreateOrUpdate goes through Save, which writes every field.
	//
	// The cost is the optimistic version check, which Save does not apply. For an
	// operator toggling one worker that is an acceptable trade -- and arguably
	// the right one, since a pause should not fail because the worker happened to
	// re-register a moment earlier.
	if _, err := m.store.CreateOrUpdate(ctx, &worker); err != nil {
		return manifest.ResourceManifest{}, false, err
	}

	log.Printf("worker %q paused=%t", worker.Name, paused)

	return worker.ToManifest(), true, nil
}

func (m *workersAPIImpl) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return m.store.Delete(ctx, &WorkerInstance{}, id.ID, id.Version)
}

// resourceSaver is the part of the store needed to write a resource back whole.
type resourceSaver interface {
	CreateOrUpdate(ctx context.Context, value any, options ...dbstore.Option) (bool, error)
}

// saveResource persists a resource that was read, modified, and is being written
// back in full.
//
// It deliberately avoids dbstore.Update, which hands the struct to gorm's
// Updates and therefore skips every zero-valued field. Any bool set to false,
// string set to "" or number set to 0 is silently dropped -- which is why
// disabling a scenario or a runner appeared to succeed and changed nothing.
//
// The optimistic version check moves to the read: callers load with
// dbstore.WithVersion, so a write against a stale version is rejected there.
// That is a narrower guarantee than a version-guarded write, so this is for
// resource edits, not for status transitions that race -- claiming a job still
// uses a version-guarded Update, because two workers reaching for the same run
// is exactly the case it has to lose.
func saveResource(ctx context.Context, store resourceSaver, value any) error {
	_, err := store.CreateOrUpdate(ctx, value)
	return err
}

func manifestMatch(entry manifest.ObjectMeta) manifest.SearchQuery {
	// var selector manifest.Selector
	// if len(entry.Labels) > 0 {
	// 	req := make(manifest.Requirements, 0, len(entry.Labels))
	// 	for k, v := range entry.Labels {
	// 		r, err := manifest.NewRequirement(k, manifest.Equals, []string{v})
	// 		if err != nil {
	// 			continue
	// 		}
	// 		req = append(req, r)
	// 	}

	// 	selector = manifest.NewSelector(req...)
	// }

	return manifest.SearchQuery{
		Name: string(entry.Name),
		// Selector: selector,
	}
}

func (m *runnersAPIImpl) GetToken(ctx context.Context, runnerName manifest.ResourceName) (APIToken, bool, error) {
	var runner Runner
	if exist, err := m.store.GetByName(ctx, &runner, runnerName); err != nil {
		return APIToken(""), false, err
	} else if !exist {
		return APIToken(""), false, nil
	}

	now := time.Now()
	// Generate JWT with valid-until clause, to give worker a time to post
	claims := &jwt.RegisteredClaims{
		// ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Minute)),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(23 * time.Hour)),
		Subject:   string(runner.UID),
		// Issuer: ,
		// ID: ,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(m.hmacSampleSecret)
	if err != nil {
		return APIToken(tokenString), true, fmt.Errorf("failed to sign the JWT token: %w", err)
	}

	return APIToken(tokenString), true, nil
}

func (m *runnersAPIImpl) Auth(ctx context.Context, apiToken APIToken, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	var result manifest.ResourceManifest
	token, err := jwt.Parse(string(apiToken), func(token *jwt.Token) (interface{}, error) {
		// Don't forget to validate the alg is what you expect:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return m.hmacSampleSecret, nil
	})
	if err != nil {
		return result, bark.ErrResourceUnauthorized
	}

	tokenSubj, err := token.Claims.GetSubject()
	if err != nil {
		return result, bark.ErrResourceUnauthorized
	}

	var runner Runner
	if ok, err := m.store.GetByUID(ctx, &runner, manifest.ResourceID(tokenSubj),
		dbstore.Expand("Instances", manifestMatch(newEntry.Metadata))); err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceUnauthorized
	}

	// Business Rule: Runner must be active to accept new workers auth
	if !runner.Spec.IsActive {
		return result, bark.ErrResourceUnauthorized
	}

	// It's ok to have nameless workers, will generate name if none provided
	// if newEntry.Metadata.Name == "" {
	// 	newEntry.Metadata.Name = manifest.ResourceName(NewRandToken(16))
	// }

	worker, err := NewWorkerInstance(newEntry)
	if err != nil {
		return result, err
	}

	// TODO: Validate that the worker matches runner's requirements
	reqSelector, err := runner.Spec.Requirements.AsSelector()
	if err != nil {
		// Note, failed to parse Runner's requirements so can't auth any workers
		return result, &bark.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: fmt.Sprintf("runner's requirements are invalid: %v", err),
		}
	}

	log.Printf("Checking if a worker matches runner's requirements: %q", runner.Spec.Requirements.AsLabels())
	if !reqSelector.Matches(worker.Labels) {
		log.Printf("worker doesn't matches runner's requirements: %q", runner.Spec.Requirements.AsLabels())
		// Note, failed to parse Runner's requirements so can't auth any workers
		return result, &bark.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: "worker does not satisfy runner's requirements",
		}
	} else {
		log.Printf("...its a match!")
	}

	// TODO: Should do min with pre-set TTL
	worker.Status.TTL = worker.Spec.RequestedTTL
	worker.Spec.Runner = runner

	log.Printf("Runner has %d workers matches", len(runner.Status.Instances))
	if len(runner.Status.Instances) > 0 && runner.Status.Instances[0].Name == worker.Name {
		existingWorkerRecord := runner.Status.Instances[0]
		// Re-auth attempt for the same worker?
		log.Printf("Worker %q re-authenticating before TTL timeout", existingWorkerRecord.Name)

		// Note: only Spec and Labels are taken from the re-registering worker.
		// Status is left as the server has it, which is what keeps an operator's
		// pause in force across a reconnect -- a worker must not be able to
		// un-pause itself by restarting.
		existingWorkerRecord.Labels = manifest.MergeLabels(worker.Labels, workerLabels(runner))
		worker.Spec.RunnerID = existingWorkerRecord.Spec.RunnerID
		existingWorkerRecord.Spec = worker.Spec

		_, err = m.store.Update(ctx, &existingWorkerRecord, existingWorkerRecord.UID, dbstore.WithVersion(existingWorkerRecord.Version))
	} else {
		// Business Rule: Runner can only have a number of new worker up-to-a limit, if limit is set
		if runner.Spec.MaxInstances > 0 && runner.Status.NumberInstances >= runner.Spec.MaxInstances {
			return result, bark.ErrResourceUnauthorized
		}

		worker.Labels = manifest.MergeLabels(worker.Labels, workerLabels(runner))

		err = m.store.Create(ctx, &worker)
		runner.Status.Instances = append(runner.Status.Instances, worker)
	}

	return runner.ToManifest(), err
}

// ------------------------------
// / ArtifactsApis implementation
// ------------------------------
type artifactAPIImp struct {
	store dbstore.TransactionalStore

	resultsSigningKey []byte
}

func (m *artifactAPIImp) List(ctx context.Context, query manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []Artifact
	total, err = m.store.Find(ctx, &models, query, dbstore.Omit("Content"), dbstore.OrderByCreatedAt(dbstore.OrderAscending))
	if err != nil {
		return
	}

	// dbstore.Omit is insufficient
	results = make([]manifest.ResourceManifest, 0, len(models))
	for _, model := range models {
		// Note: Do not return artifact value when listing
		model.Spec.Artifact.Content = nil
		results = append(results, model.ToManifest())
	}

	return
}

func (m *artifactAPIImp) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
	var model Artifact
	exist, err = m.store.GetByName(ctx, &model, id, dbstore.Omit("Content"))

	// TODO: Find a better way to not-expand content
	model.Spec.Artifact.Content = nil

	result = model.ToManifest()
	return
}

func (m *artifactAPIImp) GetContent(ctx context.Context, name manifest.ResourceName) (resource ArtifactSpec, exists bool, commError error) {
	var result Artifact
	exist, err := m.store.GetByName(ctx, &result, name)
	return result.Spec, exist && result.Name == name, err
}

// artifactLabels derives the system labels for an uploaded artifact, given the
// labels the worker supplied with it.
//
// System labels are merged last, and therefore win. That ordering is the point:
// the data-class labels are what retention and audits act on, so a worker must
// not be able to relabel its own upload as clean. The class is taken from the
// artifact spec, which the server has already parsed, rather than from any label
// the worker attached.
func artifactLabels(workerLabels manifest.Labels, spec ArtifactSpec, result Result) manifest.Labels {
	dataClass := spec.Artifact.DataClass

	systemLabels := manifest.Labels{
		LabelArtifactDataClass:         dataClass.String(),
		LabelArtifactMayContainSecrets: strconv.FormatBool(dataClass.MayContainSecrets()),
	}

	// Groups all artifacts produced by the content type: logs / HAR / etc.
	// These are derived from values that do not follow the label grammar -- a
	// MIME type contains '/', a puppeteer artifact's kind is a file extension --
	// so they are mapped into it rather than written through unchanged.
	putLabel(systemLabels, LabelArtifactKind, spec.Artifact.Rel)
	putLabel(systemLabels, LabelArtifactMime, spec.Artifact.MimeType)

	putLabel(systemLabels, LabelResultName, string(result.Name))
	putLabel(systemLabels, LabelResultUID, string(result.UID))
	putLabel(systemLabels, LabelResultVersion, result.Version.String())

	// Updated concurrently and will not be up-to-date
	// LabelResultJobState: string(result.Status.Status),
	// LabelResultStatus:   string(result.Status.Result),

	return manifest.MergeLabels(workerLabels, systemLabels)
}

func (m *artifactAPIImp) Create(ctx context.Context, apiToken APIToken, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	token, err := jwt.Parse(string(apiToken), func(token *jwt.Token) (interface{}, error) {
		// Don't forget to validate the alg is what you expect:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return m.resultsSigningKey, nil
	})
	if err != nil {
		return manifest.ResourceManifest{}, bark.ErrResourceUnauthorized
	}

	tokenSubj, err := token.Claims.GetSubject()
	if err != nil {
		return manifest.ResourceManifest{}, bark.ErrResourceUnauthorized
	}

	// Find result this artifact is for:
	// Subject:   string(entry.UID),
	var result Result
	if ok, err := m.store.GetByUID(ctx, &result, manifest.ResourceID(tokenSubj),
		dbstore.Expand("Artifacts", manifestMatch(newEntry.Metadata))); err != nil {
		return manifest.ResourceManifest{}, err
	} else if !ok {
		return manifest.ResourceManifest{}, bark.ErrResourceUnauthorized
	}

	entry, err := NewArtifact(newEntry)
	if err != nil {
		log.Printf("Failed to convert Artifact Manifest into a model: %v", err)
		return manifest.ResourceManifest{}, err
	}
	entry.Spec.Result = result

	if entry.Spec.Artifact.Rel == "" {
		return manifest.ResourceManifest{}, bark.ErrNotAcceptableMediaType
	}

	entry.Labels = artifactLabels(entry.Labels, entry.Spec, result)

	log.Printf("Result has %d artifacts with Name matches", len(result.Status.Artifacts))
	if len(result.Status.Artifacts) > 0 && result.Status.Artifacts[0].Name == entry.Name {
		existingRecord := result.Status.Artifacts[0]
		// Double posting the same artifact, just ignore
		log.Printf("Attempt to post the same artifact %q again. Rejected", existingRecord.Name)
		return manifest.ResourceManifest{}, bark.ErrResourceVersionConflict
	}

	//////////////////
	err = m.store.Create(ctx, &entry)
	return entry.ToManifest(), err
}

func (m *artifactAPIImp) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return m.store.Delete(ctx, &Artifact{}, id.ID, id.Version)
}

// ------------------------------
// / LabelsApi implementation
// ------------------------------
func kindToModel(kind manifest.Kind) (model any, found bool) {
	switch kind {
	case KindWorkerInstance:
		return &WorkerInstance{}, true
	case KindRunner:
		return &Runner{}, true
	case KindResult:
		return &Result{}, true
	case KindScenario:
		return &Scenario{}, true
	case KindArtifact:
		return &Artifact{}, true
	default:
		return nil, false
	}
}

type labelsAPIImpl struct {
	store dbstore.LabelStore
	kind  manifest.Kind
}

func (m *labelsAPIImpl) ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error) {
	model, found := kindToModel(m.kind)
	if !found {
		return nil, 0, manifest.ErrUnknownKind
	}

	result, err = m.store.FindNames(ctx, model, searchQuery)
	return
}

func (m *labelsAPIImpl) ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error) {
	model, found := kindToModel(m.kind)
	if !found {
		return nil, 0, manifest.ErrUnknownKind
	}

	result, err = m.store.FindLabels(ctx, model, searchQuery)
	return
}

func (m *labelsAPIImpl) ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error) {
	model, found := kindToModel(m.kind)
	if !found {
		return nil, 0, manifest.ErrUnknownKind
	}

	result, err = m.store.FindLabelValues(ctx, model, label, searchQuery)
	return
}
