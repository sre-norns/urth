package urth

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// LabelsApi models helper APIs to access resource names and label to power search
type LabelsApi interface {
	ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error)
	ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error)
	ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error)
}

type ReadableResourceApi[T any] interface {
	// List all resources matching given search query
	List(ctx context.Context, searchQuery manifest.SearchQuery) (result []T, total int64, err error)

	// Get a single resource given its unique ID,
	// Returns a resource if it exists, false, if resource doesn't exists
	// error if there was communication error with the storage
	Get(ctx context.Context, id manifest.ResourceName) (resource T, exists bool, commError error)
}

type ManageableResourceApi interface {
	CreateOrUpdate(ctx context.Context, entry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error)

	// Create attempts to create a new resource (Scenario) based on the manifest provided
	Create(ctx context.Context, entry manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// Update a single resource identified by a unique ID
	Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error)
}

// RunnersApi encapsulate APIs to interacting with `Runners`
type RunnersApi interface {
	ReadableResourceApi[manifest.ResourceManifest]
	ManageableResourceApi

	// GetToken generates a JWT token for a worker instance to auth as a Runner
	GetToken(ctx context.Context, runID manifest.ResourceName) (ApiToken, bool, error)

	// Authenticate a worker and receive Identity from the server
	Auth(ctx context.Context, token ApiToken, worker manifest.ResourceManifest) (manifest.ResourceManifest, error)
}

type ScenarioApi interface {
	ReadableResourceApi[manifest.ResourceManifest]
	ManageableResourceApi

	UpdateScript(ctx context.Context, id manifest.VersionedResourceID, entry ProbManifest) (bark.CreatedResponse, bool, error)

	// ClientAPI: Can it be done using filters?
	ListRunnable(ctx context.Context, query manifest.SearchQuery) ([]Scenario, error)
}

type RunResultApi interface {
	ReadableResourceApi[Result]

	Create(ctx context.Context, entry manifest.ResourceManifest) (Result, error)

	// Auth(ctx context.Context, runID manifest.VersionedResourceID, authRequest AuthJobRequest) (AuthJobResponse, error)
	Auth(ctx context.Context, runID manifest.ResourceName, authRequest AuthJobRequest) (AuthJobResponse, error)

	// TODO: Token can be used to look-up ID!
	UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token ApiToken, entry ResultStatus) (bark.CreatedResponse, error)
}

type ArtifactApi interface {
	ReadableResourceApi[manifest.ResourceManifest]

	// Create create a new artifact resource to allow storage of artifact produced during script execution
	// Only authorized [Runners] are allowed to create artifacts
	Create(ctx context.Context, token ApiToken, entry manifest.ResourceManifest) (manifest.ResourceManifest, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error)

	GetContent(ctx context.Context, id manifest.ResourceName) (resource ArtifactSpec, exists bool, commError error)
}

type Service interface {
	// GetLabels returns APIs to access names/labels/label values to power resource search
	Labels(manifest.Kind) LabelsApi

	Runners() RunnersApi
	Scenarios() ScenarioApi
	Results(scenarioName manifest.ResourceName) RunResultApi
	Artifacts() ArtifactApi
}

func NewService(store dbstore.TransitionalStore, scheduler Scheduler) Service {
	return &serviceImpl{
		store:     store,
		scheduler: scheduler,
	}
}

type (
	serviceImpl struct {
		store     dbstore.TransitionalStore
		scheduler Scheduler
	}
)

func (s *serviceImpl) Runners() RunnersApi {
	return &runnersApiImpl{
		store:            s.store,
		hmacSampleSecret: []byte("my_secret_key"), // FIXME: Must be Runtime configurable secret
	}
}

func (s *serviceImpl) Scenarios() ScenarioApi {
	return &scenarioApiImpl{
		store: s.store,
	}
}

func (s *serviceImpl) Results(scenarioName manifest.ResourceName) RunResultApi {
	return &resultsApiImpl{
		store:      s.store,
		scenarioId: scenarioName,
		scheduler:  s.scheduler,
		workersApi: &runnersApiImpl{
			store: s.store,
		},

		resultsSigningKey: []byte("my_results signing secret key, duh"), // FIXME: Must be Runtime configurable secret
	}
}

func (s *serviceImpl) Artifacts() ArtifactApi {
	return &artifactApiImp{
		store: s.store,

		resultsSigningKey: []byte("my_results signing secret key, duh"), // FIXME: Must be Runtime configurable secret
	}
}

func (s *serviceImpl) Labels(k manifest.Kind) LabelsApi {
	return &labelsApiImpl{
		kind:  k,
		store: s.store,
	}
}

// ------------------------------
// / Scenarios API
// ------------------------------
type scenarioApiImpl struct {
	store dbstore.TransitionalStore
}

func (m *scenarioApiImpl) ListRunnable(ctx context.Context, query manifest.SearchQuery) (results []Scenario, err error) {
	_, err = m.store.Find(ctx, &results, query)
	// FIXME: Update query
	return
}

func (m *scenarioApiImpl) List(ctx context.Context, query manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []Scenario

	total, err = m.store.Find(ctx, &models, query) //, dbstore.Expand("Results", manifest.SearchQuery{
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

func (m *scenarioApiImpl) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
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

func (m *scenarioApiImpl) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
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

func (m *scenarioApiImpl) create(ctx context.Context, newEntry Scenario) (Scenario, error) {
	err := m.store.Create(ctx, &newEntry)
	return newEntry, err
}

func (m *scenarioApiImpl) update(ctx context.Context, id manifest.VersionedResourceID, entry Scenario) (Scenario, error) {
	// Find target entry to be updated
	var result Scenario
	if ok, err := m.store.GetByUIDWithVersion(ctx, &result, id); err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceNotFound
	}

	// FIXME: Move to a metadata update util function in wyrd/manifest
	// Identity check
	if result.Name != entry.Name {
		return entry, bark.ErrResourceNotFound
	}

	result.Labels = entry.Labels
	result.Spec = entry.Spec

	ok, err := m.store.Update(ctx, &result, result.GetVersionedID())
	if err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceVersionConflict
	}

	return result, err
}

func (m *scenarioApiImpl) Create(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewScenario(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.create(ctx, entry)
	return result.ToManifest(), err
}

func (m *scenarioApiImpl) Update(ctx context.Context, id manifest.VersionedResourceID, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewScenario(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.update(ctx, id, entry)
	return result.ToManifest(), err
}

func (m *scenarioApiImpl) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return m.store.Delete(ctx, &Scenario{}, id.ID, id.Version)
}

func (m *scenarioApiImpl) UpdateScript(ctx context.Context, id manifest.VersionedResourceID, prob ProbManifest) (bark.CreatedResponse, bool, error) {
	var result Scenario
	if ok, err := m.store.GetByUIDWithVersion(ctx, &result, id); !ok || err != nil {
		return bark.CreatedResponse{}, ok, err
	}

	result.Spec.Prob = prob
	ok, err := m.store.Update(ctx, &result, result.GetVersionedID())

	return bark.CreatedResponse{
		TypeMeta:            manifest.TypeMeta{Kind: KindScenario},
		VersionedResourceID: result.GetVersionedID(),
	}, ok, err
}

// ------------------------------
// / Scenarios run results
// ------------------------------
type resultsApiImpl struct {
	store      dbstore.TransitionalStore
	scenarioId manifest.ResourceName
	scheduler  Scheduler
	workersApi *runnersApiImpl

	resultsSigningKey []byte
}

func (s *resultsApiImpl) scheduleRun(ctx context.Context, runResult Result) (RunId, error) {
	if s.scheduler == nil || s.workersApi == nil {
		return InvalidRunId, nil
	}

	if runResult.Spec.Scenario.Name == "" {
		return InvalidRunId, fmt.Errorf("internal scheduling error: results.scenario has no name")
	}

	// Check if scenario is enabled!
	if !runResult.Spec.Scenario.Spec.IsActive {
		return InvalidRunId, nil
	}

	// Find all workers qualified to run the scenario:
	requirement := runResult.Spec.Scenario.Spec.Requirements.AsLabels()
	requirementsSelector, err := manifest.ParseSelector(requirement)
	if err != nil {
		return InvalidRunId, fmt.Errorf("failed to parse scenario requirements: %w", err)
	}

	// TODO: Its scheduler responsibility to match scenario to a worker. Move it there.
	log.Printf("Scheduling scenario: looking for workers that match: %q", requirement)
	workers, totalWorkers, err := s.workersApi.List(ctx, manifest.SearchQuery{
		Selector: requirementsSelector,
	})
	if err != nil {
		return InvalidRunId, fmt.Errorf("failed to list workers to schedule a scenario: %w", err)
	}

	log.Printf("Scheduling scenario: %v (active=%t); qualified workers: %d / %d qualified", runResult.Spec.Scenario.Name, runResult.Spec.Scenario.Spec.IsActive, len(workers), totalWorkers)
	return s.scheduler.Schedule(ctx, runResult, runResult.Spec.Scenario) //scenarioToRunnable(runResult, scenario))
}

func (m *resultsApiImpl) List(ctx context.Context, searchQuery manifest.SearchQuery) (results []Result, total int64, err error) {
	var scenario Scenario
	if exist, err := m.store.GetByName(ctx, &scenario, m.scenarioId); err != nil {
		return nil, 0, fmt.Errorf("failed to load required scenario: %w", err)
	} else if !exist {
		return nil, 0, bark.ErrResourceNotFound
	}

	total, err = m.store.FindLinked(ctx, &results, "Results", &scenario, searchQuery)
	return
}

func (m *resultsApiImpl) Create(ctx context.Context, newEntry manifest.ResourceManifest) (Result, error) {
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

	// Ensure labels are set correctly
	newEntry.Metadata.Labels = manifest.MergeLabels(
		// scenario.Labels,
		newEntry.Metadata.Labels,
		manifest.Labels{
			LabelScenarioId: string(m.scenarioId),
		},
	)

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
	if scenarioExist, err := m.store.GetByName(ctx, &entry.Spec.Scenario, m.scenarioId); err != nil {
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
		Result: RunNotFinished,
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
		if _, uerr := m.store.Update(ctx, &entry, entry.GetVersionedID()); uerr != nil {
			log.Print("embarrassing error: failed to update run DB entry after failure to schedule it: ", uerr)
		}
		// Note: we do want to return original error, to know why we failed to schedule in a first place
		return Result{}, err
	}

	return entry, err
}

func (m *resultsApiImpl) Auth(ctx context.Context, resultName manifest.ResourceName, authRequest AuthJobRequest) (AuthJobResponse, error) {
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

	entry.Labels = manifest.MergeLabels(
		entry.Labels,
		authRequest.Labels,
		// Last to ensure that LabelScenarioId can not be overriden by the worker labels
		manifest.Labels{
			LabelScenarioId: string(m.scenarioId),
		},
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
	if ok, err := m.store.Update(ctx, &entry, entry.GetVersionedID()); err != nil {
		return AuthJobResponse{}, err
	} else if !ok {
		// If version update failed, it means that someone else bit us to it and took the job
		return AuthJobResponse{}, bark.ErrResourceUnauthorized // ErrResourceVersionConflict
	}

	return AuthJobResponse{
		CreatedResponse: bark.CreatedResponse{
			VersionedResourceID: entry.GetVersionedID(),
		},
		Token: ApiToken(tokenString), // NewRandToken(32), //entry.UpdateToken,
	}, err
}

func (m *resultsApiImpl) validateUpdateRequest(_ context.Context, entry Result, bearerToken ApiToken) error {
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

func (m *resultsApiImpl) UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token ApiToken, runResults ResultStatus) (bark.CreatedResponse, error) {
	var entry Result
	if ok, err := m.store.GetByUIDWithVersion(ctx, &entry, id); err != nil {
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

	if ok, err := m.store.Update(ctx, &entry, entry.GetVersionedID()); err != nil {
		return bark.CreatedResponse{}, err
	} else if !ok {
		return bark.CreatedResponse{}, bark.ErrResourceVersionConflict
	}

	return bark.CreatedResponse{
		TypeMeta:            entry.ToManifest().TypeMeta,
		VersionedResourceID: entry.GetVersionedID(),
	}, nil
}

func (m *resultsApiImpl) Get(ctx context.Context, id manifest.ResourceName) (result Result, exist bool, err error) {
	exist, err = m.store.GetByName(ctx, &result, id)
	return
}

// ------------------------------
// / Runners resources API
// ------------------------------
type runnersApiImpl struct {
	store            dbstore.TransitionalStore
	hmacSampleSecret []byte
}

func (m *runnersApiImpl) List(ctx context.Context, searchQuery manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []Runner
	total, err = m.store.Find(ctx, &models, searchQuery)
	if err != nil {
		return
	}

	results = make([]manifest.ResourceManifest, 0, len(models))
	for _, model := range models {
		results = append(results, model.ToManifest())
	}
	return
}

func (m *runnersApiImpl) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
	var model Runner
	exist, err = m.store.GetByName(ctx, &model, id)
	result = model.ToManifest()
	return
}

func (m *runnersApiImpl) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
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

func (m *runnersApiImpl) create(ctx context.Context, newEntry Runner) (Runner, error) {
	// TODO: Generate auth token?
	// 	IdToken: randToken(16),

	err := m.store.Create(ctx, &newEntry)
	return newEntry, err
}
func (m *runnersApiImpl) update(ctx context.Context, id manifest.VersionedResourceID, newEntry Runner) (Runner, error) {
	var result Runner
	if ok, err := m.store.GetByUIDWithVersion(ctx, &result, id); err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceVersionConflict
	}

	// Identity check
	if result.Name != newEntry.Name {
		return result, bark.ErrResourceNotFound
	}

	result.Labels = newEntry.Labels
	result.Spec = newEntry.Spec

	// TODO: If a runner status changed to disabled, all instance must be disabled too

	// Persist changes
	ok, err := m.store.Update(ctx, &result, id)
	if err != nil {
		return result, err
	} else if !ok {
		return result, bark.ErrResourceVersionConflict
	}

	return result, err
}

func (m *runnersApiImpl) Create(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewRunner(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.create(ctx, entry)
	return result.ToManifest(), err
}

func (m *runnersApiImpl) Update(ctx context.Context, id manifest.VersionedResourceID, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	entry, err := NewRunner(newEntry)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}

	result, err := m.update(ctx, id, entry)
	return result.ToManifest(), err
}

func (m *runnersApiImpl) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return m.store.Delete(ctx, &Runner{}, id.ID, id.Version)
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

func (m *runnersApiImpl) GetToken(ctx context.Context, runnerName manifest.ResourceName) (ApiToken, bool, error) {
	var runner Runner
	if exist, err := m.store.GetByName(ctx, &runner, runnerName); err != nil {
		return ApiToken(""), false, err
	} else if !exist {
		return ApiToken(""), false, nil
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
		return ApiToken(tokenString), true, fmt.Errorf("failed to sign the JWT token: %w", err)
	}

	return ApiToken(tokenString), true, nil
}

func (m *runnersApiImpl) Auth(ctx context.Context, apiToken ApiToken, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
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
	// TODO: Should do min with pre-set TTL
	worker.Status.TTL = worker.Spec.RequestedTTL
	worker.Spec.Runner = runner

	log.Printf("Runner has %d workers matches", len(runner.Status.Instances))
	if len(runner.Status.Instances) > 0 && runner.Status.Instances[0].Name == worker.Name {
		existingWorkerRecord := runner.Status.Instances[0]
		// Re-auth attempt for the same worker?
		log.Printf("Worker %q re-authenticating before TTL timeout", existingWorkerRecord.Name)

		// if !existingWorkerRecord.Spec.IsActive {
		// }

		existingWorkerRecord.Labels = worker.Labels
		worker.Spec.RunnerID = existingWorkerRecord.Spec.RunnerID
		existingWorkerRecord.Spec = worker.Spec

		_, err = m.store.Update(ctx, &existingWorkerRecord, existingWorkerRecord.GetVersionedID())
	} else {
		// Business Rule: Runner can only have a number of new worker up-to-a limit, if limit is set
		if runner.Spec.MaxInstances > 0 && runner.Status.NumberInstances >= runner.Spec.MaxInstances {
			return result, bark.ErrResourceUnauthorized
		}

		err = m.store.Create(ctx, &worker)
		runner.Status.Instances = append(runner.Status.Instances, worker)
	}

	return runner.ToManifest(), err
}

// ------------------------------
// / ArtifactsApis implementation
// ------------------------------
type artifactApiImp struct {
	store dbstore.TransitionalStore

	resultsSigningKey []byte
}

func (m *artifactApiImp) List(ctx context.Context, query manifest.SearchQuery) (results []manifest.ResourceManifest, total int64, err error) {
	var models []Artifact
	total, err = m.store.Find(ctx, &models, query, dbstore.Omit("Content"))
	if err != nil {
		return
	}

	// dbstore.Omit is insufficient
	results = make([]manifest.ResourceManifest, 0, len(models))
	for _, model := range models {
		// Note: Do not return artifact value when listing
		model.Spec.Content = nil
		results = append(results, model.ToManifest())
	}

	return
}

func (m *artifactApiImp) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exist bool, err error) {
	var model Artifact
	exist, err = m.store.GetByName(ctx, &model, id, dbstore.Omit("Content"))

	// TODO: Find a better way to not-expand content
	model.Spec.Content = nil

	result = model.ToManifest()
	return
}

func (m *artifactApiImp) GetContent(ctx context.Context, name manifest.ResourceName) (resource ArtifactSpec, exists bool, commError error) {
	var result Artifact
	exist, err := m.store.GetByName(ctx, &result, name)
	return result.Spec, exist && result.Name == name, err
}

func (m *artifactApiImp) Create(ctx context.Context, apiToken ApiToken, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
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

func (m *artifactApiImp) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
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

type labelsApiImpl struct {
	store dbstore.TransitionalStore
	kind  manifest.Kind
}

func (m *labelsApiImpl) ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error) {
	model, found := kindToModel(m.kind)
	if !found {
		return nil, 0, manifest.ErrUnknownKind
	}

	result, err = m.store.FindNames(ctx, model, searchQuery)
	return
}

func (m *labelsApiImpl) ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error) {
	model, found := kindToModel(m.kind)
	if !found {
		return nil, 0, manifest.ErrUnknownKind
	}

	result, err = m.store.FindLabels(ctx, model, searchQuery)
	return
}

func (m *labelsApiImpl) ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (result manifest.StringSet, total int64, err error) {
	model, found := kindToModel(m.kind)
	if !found {
		return nil, 0, manifest.ErrUnknownKind
	}

	result, err = m.store.FindLabelValues(ctx, model, label, searchQuery)
	return
}
