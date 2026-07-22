package urth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"sort"
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
	//
	// Deprecated: returns no session credential, leaving the caller to find its
	// own identity in the runner's Status.Instances. Use AuthWorker.
	Auth(ctx context.Context, token APIToken, worker manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// AuthWorker registers a worker, returning its assigned identity, a session
	// credential for authenticating later calls, and where to collect work.
	AuthWorker(ctx context.Context, token APIToken, worker manifest.ResourceManifest) (WorkerRegistrationResponse, error)
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

	// Auth claims a job using identity supplied in the request body.
	//
	// Deprecated: the caller asserts its own identity, which is not evidence of
	// anything. Retained for the asynq prototype worker. Use ClaimRun.
	Auth(ctx context.Context, runID manifest.ResourceName, authRequest AuthJobRequest) (AuthJobResponse, error)

	// ClaimRun claims a dispatched job on behalf of the worker that owns the
	// given session credential. The run is identified by UID, matching the
	// dispatch envelope.
	ClaimRun(ctx context.Context, resultUID manifest.ResourceID, session APIToken, request ClaimJobRequest) (AuthJobResponse, error)

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

// ServiceOption configures optional service dependencies.
//
// Variadic options rather than more constructor parameters: signing keys and a
// transport provider are things a production server has and a test usually does
// not, and threading nils through every call site to say so reads worse than
// leaving them out.
type ServiceOption func(*serviceImpl)

// WithSigningKeys supplies the secrets used to mint and verify tokens.
//
// A service built without this generates ephemeral keys, which is fine for
// tests and fatal for a multi-replica deployment -- see SigningKeysConfig.
func WithSigningKeys(keys SigningKeys) ServiceOption {
	return func(s *serviceImpl) { s.keys = keys }
}

// WithWorkerTransport supplies the provider that tells a registered worker
// where to collect its jobs. Without it, registration still succeeds but
// returns no connection details, which is what an asynq-only deployment wants.
func WithWorkerTransport(provider WorkerTransportProvider) ServiceOption {
	return func(s *serviceImpl) { s.transport = provider }
}

// WithSessionTTL sets how long an issued worker session stays valid.
func WithSessionTTL(ttl time.Duration) ServiceOption {
	return func(s *serviceImpl) { s.sessionTTL = ttl }
}

// WithMaxRunDuration caps how long a worker may hold a run capability.
func WithMaxRunDuration(d time.Duration) ServiceOption {
	return func(s *serviceImpl) { s.maxRunDuration = d }
}

const (
	// DefaultSessionTTL bounds a worker session. Short enough that revoking a
	// worker takes effect within a shift, long enough that renewal is not a
	// constant load on the API.
	DefaultSessionTTL = 1 * time.Hour

	// DefaultMaxRunDuration is the ceiling on any single run's capability.
	// A worker may request less; it cannot request more.
	DefaultMaxRunDuration = 30 * time.Minute
)

func NewService(store *dbstore.DBStore, scheduler Scheduler, options ...ServiceOption) Service {
	s := &serviceImpl{
		store:          store,
		scheduler:      scheduler,
		sessionTTL:     DefaultSessionTTL,
		maxRunDuration: DefaultMaxRunDuration,
	}

	for _, option := range options {
		option(s)
	}

	// A service with no keys at all would sign with empty secrets, which is
	// worse than the hardcoded literals this replaced. Generate instead.
	if len(s.keys.Session) == 0 || len(s.keys.Run) == 0 || len(s.keys.Enrolment) == 0 {
		keys, err := SigningKeysConfig{}.Build()
		if err == nil {
			if len(s.keys.Enrolment) == 0 {
				s.keys.Enrolment = keys.Enrolment
			}
			if len(s.keys.Session) == 0 {
				s.keys.Session = keys.Session
			}
			if len(s.keys.Run) == 0 {
				s.keys.Run = keys.Run
			}
		}
	}

	return s
}

type (
	serviceImpl struct {
		store     *dbstore.DBStore
		scheduler Scheduler

		keys           SigningKeys
		transport      WorkerTransportProvider
		sessionTTL     time.Duration
		maxRunDuration time.Duration
	}
)

func (s *serviceImpl) Runners() RunnersAPI {
	return &runnersAPIImpl{
		store:            s.store,
		hmacSampleSecret: s.keys.Enrolment,
		keys:             s.keys,
		transport:        s.transport,
		sessionTTL:       s.sessionTTL,
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

		resultsSigningKey: s.keys.Run,
		keys:              s.keys,
		maxRunDuration:    s.maxRunDuration,
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

		resultsSigningKey: s.keys.Run,
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

	keys           SigningKeys
	maxRunDuration time.Duration
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

	return m.scheduler.Schedule(ctx, runResult, runResult.Spec.Scenario)
}

// placeRun selects the runner a run should be dispatched to.
//
// Placement is where a scenario's requirements finally mean something. The
// prototype parsed the selector, listed the runners it matched, logged how many
// there were, and then threw the list away -- every job went to one shared
// queue and any worker could take it, so a scenario that declared it needed a
// runner inside a particular network had no way of getting one.
//
// Reports false when nothing matches rather than failing. A transport that does
// not route per runner -- the asynq prototype -- can still dispatch such a run,
// and it is the routing transport's business to object. See natsq.ErrNoRunner.
func (m *resultsAPIImpl) placeRun(ctx context.Context, runResult Result) (Runner, bool, error) {
	if m.workersAPI == nil {
		return Runner{}, false, nil
	}

	requirement := runResult.Spec.Scenario.Spec.Requirements.AsLabels()
	selector, err := manifest.ParseSelector(requirement)
	if err != nil {
		return Runner{}, false, fmt.Errorf("failed to parse scenario requirements: %w", err)
	}

	candidates, total, err := m.workersAPI.List(ctx, manifest.SearchQuery{Selector: selector})
	if err != nil {
		return Runner{}, false, fmt.Errorf("failed to list runners to schedule a scenario: %w", err)
	}

	// Only an active runner is a candidate: dispatching to a disabled one would
	// queue work that, by the claim rules, no worker of that runner may take.
	var eligible []Runner
	for _, candidate := range candidates {
		runner, err := NewRunner(candidate)
		if err != nil {
			continue
		}
		if runner.Spec.IsActive {
			eligible = append(eligible, runner)
		}
	}

	if len(eligible) == 0 {
		log.Printf("no active runner matches requirements %q for scenario %q (%d considered)",
			requirement, runResult.Spec.Scenario.Name, total)
		return Runner{}, false, nil
	}

	// Deterministic selection by UID. Least-loaded or round-robin placement
	// wants queue depth per runner, which belongs to the scheduler service that
	// does not exist yet; picking stably means a scenario's runs land on one
	// runner instead of scattering, which is easier to reason about in the
	// meantime.
	sort.Slice(eligible, func(i, j int) bool { return eligible[i].UID < eligible[j].UID })
	selected := eligible[0]

	log.Printf("placed run of %q on runner %q (%d of %d eligible)",
		runResult.Spec.Scenario.Name, selected.Name, len(eligible), total)

	return selected, true, nil
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

	// Place the run on a runner before persisting it, so the record carries the
	// channel it was dispatched to from the moment it exists. ADR 0003 binds a
	// scheduled Result to a Runner and leaves worker identity empty until a
	// claim, which is exactly the shape of ExecutorRef here.
	if runner, placed, err := m.placeRun(ctx, entry); err != nil {
		return Result{}, err
	} else if placed {
		entry.Status.Executor.RunnerID = runner.UID
		entry.Status.Executor.RunnerName = runner.Name
		entry.Labels = manifest.MergeLabels(entry.Labels, manifest.Labels{
			LabelRunnerName: string(runner.Name),
			LabelRunnerUID:  string(runner.UID),
		})
	}

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

// ClaimRun authorises a worker to execute a dispatched job.
//
// The worker's identity comes from its session credential, never from the
// request. This is the check the prototype did not have: its claim endpoint
// carried no bearer middleware at all and trusted the WorkerID and RunnerID in
// the body, so anything that could reach the API could claim any job as anyone.
//
// The claim is idempotent for the same worker and dispatch. A worker whose
// claim committed but whose response was lost must be able to ask again and get
// the same authorization back -- otherwise a dropped packet strands a run that
// the server already believes is executing. A *different* worker asking for the
// same run still loses, which is what keeps the race safe.
func (m *resultsAPIImpl) ClaimRun(ctx context.Context, resultUID manifest.ResourceID, session APIToken, request ClaimJobRequest) (AuthJobResponse, error) {
	claims, err := ParseWorkerSession(m.keys, session)
	if err != nil {
		return AuthJobResponse{}, claimForbidden("invalid worker session")
	}

	if request.DispatchID == "" {
		// A well-formed dispatch always carries an ID. A claim without one is
		// malformed, not transient, so redelivery would only repeat the mistake.
		return AuthJobResponse{}, claimForbidden("claim requires a dispatch ID")
	}

	worker, runner, err := m.loadClaimant(ctx, claims)
	if err != nil {
		return AuthJobResponse{}, err
	}

	// Looked up by UID, not name. The dispatch envelope identifies the run by
	// UID precisely because a name can be reused, and a claim that resolved a
	// recycled name would authorise a worker against the wrong run.
	var entry Result
	if ok, err := m.store.GetByUID(ctx, &entry, resultUID); err != nil {
		// A store failure is not evidence the run is gone. Reporting it as
		// "not found" -- as the prototype did -- would have the worker ack and
		// discard the only dispatch for a run that may still be pending.
		return AuthJobResponse{}, claimUnavailable("load result", err)
	} else if !ok {
		return AuthJobResponse{}, claimObsolete("result not found")
	}

	// The dispatch must describe the Result as it now is. A message published
	// for an older version has been overtaken -- the run was rescheduled or
	// amended -- and executing it would run a stale definition.
	if request.ResultVersion != 0 && request.ResultVersion != entry.Version {
		log.Printf("rejecting claim for %q: dispatch is for version %v, current is %v",
			entry.Name, request.ResultVersion, entry.Version)
		return AuthJobResponse{}, claimObsolete("dispatch superseded by newer result version")
	}

	// Business Rule: a run may only be claimed by a worker of the runner it was
	// dispatched to. Without this a worker could claim jobs placed on a runner
	// it has no membership of, and label-based placement would mean nothing.
	// Reaching here means the run was already claimed for another runner, so for
	// this worker the dispatch is obsolete rather than a policy error.
	if entry.Status.Executor.RunnerID != "" && entry.Status.Executor.RunnerID != runner.UID {
		log.Printf("worker %q of runner %q may not claim %q, dispatched to runner %q",
			worker.Name, runner.UID, resultUID, entry.Status.Executor.RunnerID)
		return AuthJobResponse{}, claimObsolete("result claimed by another runner")
	}

	// The idempotent case: this worker already holds this run.
	if entry.Status.Status == JobRunning {
		if entry.Status.Executor.WorkerID == worker.UID && entry.Status.DispatchID == request.DispatchID {
			log.Printf("worker %q re-claiming %q for dispatch %v; re-issuing authorization",
				worker.Name, entry.Name, request.DispatchID)
			return m.authorizeRun(ctx, entry, entry.Status.Deadline)
		}

		// Someone else has it, or this worker has it for an older dispatch.
		return AuthJobResponse{}, claimObsolete("result already claimed")
	}

	if entry.Status.Status != JobPending {
		return AuthJobResponse{}, claimObsolete("result is not pending")
	}

	// Business Rule: the server sets the deadline. A worker may ask for less
	// time than the server allows -- and often should, so a hung probe fails
	// rather than holding a slot -- but the ceiling is not negotiable. The
	// prototype signed the run token with the worker's requested timeout
	// verbatim, so a worker could mint itself a capability valid for a week.
	duration := clampRunDuration(request.Timeout, m.maxRunDuration)

	now := time.Now()
	deadline := now.Add(duration)

	entry.Spec.TimeStarted = &now
	entry.Status.Status = JobRunning
	entry.Status.Executor = executorRef(worker, runner)
	entry.Status.DispatchID = request.DispatchID
	entry.Status.Deadline = deadline

	entry.Labels = manifest.MergeLabels(
		entry.Labels,
		manifest.Labels{
			LabelResultJobState: string(entry.Status.Status),
		},
		executorLabels(entry.Status.Executor),
	)

	// Version-guarded, deliberately. This is the update that decides a race
	// between two workers reaching for the same run: the loser's version is
	// stale and its update does not apply. Do not convert this to saveResource
	// -- that path uses gorm Save, which would let both writes succeed.
	if ok, err := m.store.Update(ctx, &entry, entry.UID, dbstore.WithVersion(entry.Version)); err != nil {
		return AuthJobResponse{}, claimUnavailable("commit claim", err)
	} else if !ok {
		// The version guard rejected the write: another worker committed its
		// claim first. The dispatch is now obsolete for this one.
		return AuthJobResponse{}, claimObsolete("lost claim race")
	}

	log.Printf("worker %q claimed %q until %v (dispatch %v)", worker.Name, entry.Name, deadline, request.DispatchID)

	return m.authorizeRun(ctx, entry, deadline)
}

// loadClaimant resolves and vets the worker behind a session credential.
func (m *resultsAPIImpl) loadClaimant(ctx context.Context, claims WorkerSessionClaims) (WorkerInstance, Runner, error) {
	var worker WorkerInstance
	var runner Runner

	if ok, err := m.store.GetByUID(ctx, &worker, claims.WorkerID); err != nil {
		return worker, runner, claimUnavailable("load worker", err)
	} else if !ok {
		// The session is validly signed but its worker record is gone -- the
		// instance was deleted or expired. ADR 0002 makes that a revocation, so
		// the credential must stop working even though it has not expired.
		return worker, runner, claimForbidden("worker instance revoked")
	}

	// The session names the runner it was issued for. If the worker has since
	// been re-registered against a different runner, the credential no longer
	// describes reality and is refused rather than silently re-scoped.
	if worker.Spec.RunnerID != claims.RunnerID {
		return worker, runner, claimForbidden("session runner mismatch")
	}

	// Business Rule: a paused worker stays registered and keeps its identity,
	// but takes no new jobs.
	if worker.Status.IsPaused {
		return worker, runner, claimForbidden("worker is paused")
	}

	if ok, err := m.store.GetByUID(ctx, &runner, worker.Spec.RunnerID); err != nil {
		return worker, runner, claimUnavailable("load runner", err)
	} else if !ok {
		return worker, runner, claimForbidden("runner missing")
	}

	// Business Rule: a worker of a disabled runner takes no jobs either.
	if !runner.Spec.IsActive {
		return worker, runner, claimForbidden("runner is disabled")
	}

	return worker, runner, nil
}

// authorizeRun mints the run capability and assembles the claim response,
// including the execution snapshot the worker needs in order to run anything.
func (m *resultsAPIImpl) authorizeRun(ctx context.Context, entry Result, deadline time.Time) (AuthJobResponse, error) {
	// Load the scenario explicitly. Result.Spec.Scenario is a lazy association
	// and comes back zero-valued from GetByName, so reading the prob straight
	// off it would hand the worker an empty execution snapshot and a run that
	// fails for no visible reason.
	scenario := entry.Spec.Scenario
	if scenario.Spec.Prob.Kind == "" {
		if ok, err := m.store.GetByUID(ctx, &scenario, entry.Spec.ScenarioID); err != nil {
			return AuthJobResponse{}, claimUnavailable("load scenario", err)
		} else if !ok {
			// The run's scenario has been deleted out from under it. There is
			// nothing left to execute, so the dispatch is obsolete rather than
			// something to retry.
			return AuthJobResponse{}, claimObsolete("scenario no longer exists")
		}
	}

	now := time.Now()

	claims := &jwt.RegisteredClaims{
		Issuer:    TokenIssuer,
		Subject:   string(entry.UID),
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		// A small grace beyond the deadline, so a run that used its full budget
		// can still report what happened. A worker that cannot upload its
		// result is a worker whose failure looks identical to a crash.
		ExpiresAt: jwt.NewNumericDate(deadline.Add(artifactUploadGrace)),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.keys.Run)
	if err != nil {
		return AuthJobResponse{}, claimUnavailable("sign run capability", err)
	}

	return AuthJobResponse{
		CreatedResponse: bark.CreatedResponse{
			VersionedResourceID: entry.GetVersionedID(),
		},
		Token:    APIToken(signed),
		Prob:     scenario.Spec.Prob,
		Scenario: scenario.Name,
		Deadline: deadline,
	}, nil
}

// artifactUploadGrace is how long past a run's deadline its capability keeps
// working, so results and artifacts from a run that used its whole budget still
// land.
const artifactUploadGrace = 5 * time.Minute

// clampRunDuration decides how long a worker may hold a run capability.
//
// The direction of the clamp is the point. A worker asking for less than the
// server allows is granted it -- a worker that knows its probe should finish in
// ten seconds is right to ask for a short lease, because a hung probe then
// fails instead of occupying a slot. A worker asking for more is given the
// server's limit, not its request: the prototype passed the requested timeout
// straight into the token's expiry, so a worker could ask for a week and get it.
func clampRunDuration(requested, maximum time.Duration) time.Duration {
	if maximum <= 0 {
		maximum = DefaultMaxRunDuration
	}

	if requested > 0 && requested < maximum {
		return requested
	}

	return maximum
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

	keys       SigningKeys
	transport  WorkerTransportProvider
	sessionTTL time.Duration
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

// Auth registers a worker and returns the runner manifest.
//
// This is the prototype's registration call, kept because the asynq worker digs
// its identity out of Status.Instances[0] of the returned runner. New workers
// should use AuthWorker, which returns an explicit identity and a session
// credential instead.
func (m *runnersAPIImpl) Auth(ctx context.Context, apiToken APIToken, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	runner, _, err := m.admitWorker(ctx, apiToken, newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	return runner.ToManifest(), nil
}

// AuthWorker registers a worker and issues it a session credential and the
// details of where to collect work.
//
// The admission rules are exactly those of Auth -- the same enrolment token,
// runner, requirements, and instance-limit checks -- because there should be
// only one answer to "may this worker join". What differs is what the caller
// gets back: an identity it does not have to guess at, a credential it can
// authenticate later calls with, and its queue.
func (m *runnersAPIImpl) AuthWorker(ctx context.Context, apiToken APIToken, newEntry manifest.ResourceManifest) (WorkerRegistrationResponse, error) {
	runner, worker, err := m.admitWorker(ctx, apiToken, newEntry)
	if err != nil {
		return WorkerRegistrationResponse{}, err
	}

	ttl := m.sessionTTL
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	// A worker may ask for a shorter session than the server's default -- a
	// short-lived worker has no reason to hold a credential outliving it -- but
	// never a longer one.
	if requested := worker.Spec.RequestedTTL; requested > 0 && requested < ttl {
		ttl = requested
	}

	session, expiresAt, err := IssueWorkerSession(m.keys, runner.UID, worker.UID, ttl)
	if err != nil {
		return WorkerRegistrationResponse{}, err
	}

	var connectionInfo NATSConnectionInfo
	if m.transport != nil {
		// Provisioning the runner's queue here, at registration, means a runner
		// created before this transport existed still gets one the first time a
		// worker shows up, rather than having its jobs published into a stream
		// with nothing bound to it.
		if connectionInfo, err = m.transport.ConnectionInfoFor(ctx, runner.UID); err != nil {
			return WorkerRegistrationResponse{}, fmt.Errorf("failed to prepare worker transport: %w", err)
		}
	}

	return WorkerRegistrationResponse{
		Runner:           runner.ToManifest(),
		Worker:           worker.ToManifest(),
		Session:          session,
		SessionExpiresAt: expiresAt,
		NATS:             connectionInfo,
	}, nil
}

// admitWorker decides whether a worker may join a runner, and creates or
// refreshes its WorkerInstance record if so.
//
// It returns the runner and the worker's own record. The caller gets the worker
// separately rather than having to find it inside runner.Status.Instances,
// which is what the prototype forced on its caller and what made the assigned
// identity ambiguous once a runner had more than one instance.
func (m *runnersAPIImpl) admitWorker(ctx context.Context, apiToken APIToken, newEntry manifest.ResourceManifest) (Runner, WorkerInstance, error) {
	var result Runner
	var registered WorkerInstance

	token, err := jwt.Parse(string(apiToken), func(token *jwt.Token) (interface{}, error) {
		// Don't forget to validate the alg is what you expect:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// hmacSampleSecret is a []byte containing your secret, e.g. []byte("my_secret_key")
		return m.hmacSampleSecret, nil
	})
	if err != nil {
		return result, registered, bark.ErrResourceUnauthorized
	}

	tokenSubj, err := token.Claims.GetSubject()
	if err != nil {
		return result, registered, bark.ErrResourceUnauthorized
	}

	var runner Runner
	if ok, err := m.store.GetByUID(ctx, &runner, manifest.ResourceID(tokenSubj),
		dbstore.Expand("Instances", manifestMatch(newEntry.Metadata))); err != nil {
		return result, registered, err
	} else if !ok {
		return result, registered, bark.ErrResourceUnauthorized
	}

	// Business Rule: Runner must be active to accept new workers auth
	if !runner.Spec.IsActive {
		return result, registered, bark.ErrResourceUnauthorized
	}

	// It's ok to have nameless workers, will generate name if none provided
	// if newEntry.Metadata.Name == "" {
	// 	newEntry.Metadata.Name = manifest.ResourceName(NewRandToken(16))
	// }

	worker, err := NewWorkerInstance(newEntry)
	if err != nil {
		return result, registered, err
	}

	// TODO: Validate that the worker matches runner's requirements
	reqSelector, err := runner.Spec.Requirements.AsSelector()
	if err != nil {
		// Note, failed to parse Runner's requirements so can't auth any workers
		return result, registered, &bark.ErrorResponse{
			Code:    http.StatusUnauthorized,
			Message: fmt.Sprintf("runner's requirements are invalid: %v", err),
		}
	}

	log.Printf("Checking if a worker matches runner's requirements: %q", runner.Spec.Requirements.AsLabels())
	if !reqSelector.Matches(worker.Labels) {
		log.Printf("worker doesn't matches runner's requirements: %q", runner.Spec.Requirements.AsLabels())
		// Note, failed to parse Runner's requirements so can't auth any workers
		return result, registered, &bark.ErrorResponse{
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
		registered = existingWorkerRecord
	} else {
		// Business Rule: Runner can only have a number of new worker up-to-a limit, if limit is set
		if runner.Spec.MaxInstances > 0 && runner.Status.NumberInstances >= runner.Spec.MaxInstances {
			return result, registered, bark.ErrResourceUnauthorized
		}

		worker.Labels = manifest.MergeLabels(worker.Labels, workerLabels(runner))

		err = m.store.Create(ctx, &worker)
		runner.Status.Instances = append(runner.Status.Instances, worker)
		registered = worker
	}

	return runner, registered, err
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
