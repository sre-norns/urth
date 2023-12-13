package urth

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/wyrd"
)

const paginationLimit = 512

var (
	ErrResourceUnauthorized    = &ErrorResponse{Code: 401, Message: "resource access unauthorized"}
	ErrForbidden               = &ErrorResponse{Code: 403, Message: "forbidden"}
	ErrResourceNotFound        = &ErrorResponse{Code: 404, Message: "requested resource not found"}
	ErrResourceVersionConflict = &ErrorResponse{Code: 409, Message: "resource version conflict"}
	ErrResourceSpecIsNil       = &ErrorResponse{Code: 400, Message: "resource has no spec"}
	ErrResourceSpecTypeInvalid = &ErrorResponse{Code: 400, Message: "resource spec type is invalid"}
)

type ReadableResourceApi[T interface{}] interface {
	// List all resources matching given search query
	List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error)

	// Get a single resource given its unique ID,
	// Returns a resource if it exists, false, if resource doesn't exists
	// error if there was communication error with the storage
	Get(ctx context.Context, id wyrd.ResourceID) (resource wyrd.ResourceManifest, exists bool, commError error)
}

type ScenarioApi interface {
	ReadableResourceApi[Scenario]

	Create(ctx context.Context, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id VersionedResourceId) (bool, error)

	// Update a single resource identified by a unique ID
	Update(ctx context.Context, id VersionedResourceId, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	UpdateScript(ctx context.Context, id VersionedResourceId, entry ProbManifest) (CreatedResponse, bool, error)

	// ClientAPI: Can it be done using filters?
	ListRunnable(ctx context.Context, query SearchQuery) ([]Scenario, error)
}

type ArtifactApi interface {
	ReadableResourceApi[Artifact]

	// FIXME: Only authorized runner are allowed to create artifacts
	Create(ctx context.Context, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id VersionedResourceId) (bool, error)

	GetContent(ctx context.Context, id wyrd.ResourceID) (resource ArtifactSpec, exists bool, commError error)
}

type RunResultApi interface {
	ReadableResourceApi[Result]

	Create(ctx context.Context, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	Auth(ctx context.Context, runID VersionedResourceId, authRequest AuthJobRequest) (AuthJobResponse, error)

	// TODO: Token can be used to look-up ID!
	Update(ctx context.Context, id VersionedResourceId, token ApiToken, entry FinalRunResults) (CreatedResponse, error)
}

// RunnersApi encapsulate APIs to interacting with `Runners`
type RunnersApi interface {
	ReadableResourceApi[Runner]

	// Client request to create a new 'slot' for a runner
	Create(ctx context.Context, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id VersionedResourceId) (bool, error)

	// Update a single resource identified by a unique ID
	Update(ctx context.Context, id VersionedResourceId, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	// Authenticate a worker and receive Identity from the server
	Auth(ctx context.Context, token ApiToken, entry RunnerRegistration) (Runner, error)
}

type LabelsApi interface {
	List(ctx context.Context, searchQuery SearchQuery) ([]ResourceLabel, error)
}

type Service interface {
	GetRunnerAPI() RunnersApi
	GetScenarioAPI() ScenarioApi
	GetResultsAPI(wyrd.ResourceID) RunResultApi
	GetArtifactsApi() ArtifactApi

	GetLabels() LabelsApi
}

func NewService(store Store, scheduler Scheduler) Service {
	return &serviceImpl{
		store:     store,
		scheduler: scheduler,
	}
}

type (
	serviceImpl struct {
		store     Store
		scheduler Scheduler
	}

	runnersApiImpl struct {
		store Store
	}

	scenarioApiImpl struct {
		store Store
	}

	resultsApiImpl struct {
		store       Store
		scenarioId  wyrd.ResourceID
		scheduler   Scheduler
		scenarioApi *scenarioApiImpl
		workersApi  *runnersApiImpl
	}

	labelsApiImpl struct {
		store Store
	}
)

func (s *serviceImpl) GetRunnerAPI() RunnersApi {
	return &runnersApiImpl{
		store: s.store,
	}
}

func (s *serviceImpl) GetScenarioAPI() ScenarioApi {
	return &scenarioApiImpl{
		store: s.store,
	}
}

func (s *serviceImpl) GetResultsAPI(id wyrd.ResourceID) RunResultApi {
	return &resultsApiImpl{
		store:      s.store,
		scenarioId: id,
		scheduler:  s.scheduler,
		scenarioApi: &scenarioApiImpl{
			store: s.store,
		},
		workersApi: &runnersApiImpl{
			store: s.store,
		},
	}
}

func (s *serviceImpl) GetArtifactsApi() ArtifactApi {
	return &artifactApiImp{
		store: s.store,
	}
}

func (s *serviceImpl) GetLabels() LabelsApi {
	return &labelsApiImpl{
		store: s.store,
	}
}

// Nice idea, but we use object not pointers...
// func getFromStore[T Resourceable](store Store, ctx context.Context, id ResourceID) (T, bool, error) {
// 	var result T
// 	ok, err := store.Get(ctx, &result, id)
// 	return result, ok && (result.GetID() == id) && !result.IsDeleted(), err

// }

//------------------------------
/// Scenarios API
//------------------------------
func (m *scenarioApiImpl) ListRunnable(ctx context.Context, query SearchQuery) ([]Scenario, error) {
	var resources []Scenario
	_, err := m.store.FindResources(ctx, &resources, query, paginationLimit)
	if err != nil {
		return nil, err
	}
	return resources, err
}

func (m *scenarioApiImpl) List(ctx context.Context, query SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Scenario
	_, err := m.store.FindResources(ctx, &resources, query, paginationLimit)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, resource := range resources {
		// TODO: Script should be moved into a separate table, that way we won't have to filter it out here
		resource.Prob.Spec = nil
		results = append(results, resource.asPartialMetadata())
	}

	return results, nil
}

func (m *scenarioApiImpl) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	// Precondition: entry.Spec != nil
	if newEntry.Spec == nil {
		return PartialObjectMetadata{}, ErrResourceSpecIsNil
	}
	spec, ok := newEntry.Spec.(*ScenarioSpec)
	if !ok {
		return PartialObjectMetadata{}, fmt.Errorf("user provided %w", ErrResourceSpecTypeInvalid)
	}

	entry := Scenario{
		ResourceMeta: GetResourceMetadata(newEntry),
		ScenarioSpec: *spec,
	}

	err := m.store.Create(ctx, &entry)
	return entry.asPartialMetadata(), err
}

func (m *scenarioApiImpl) getScenario(ctx context.Context, id wyrd.ResourceID) (Scenario, bool, error) {
	var result Scenario
	ok, err := m.store.Get(ctx, &result, id)
	return result,
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

func (m *scenarioApiImpl) Get(ctx context.Context, id wyrd.ResourceID) (wyrd.ResourceManifest, bool, error) {
	result, ok, err := m.getScenario(ctx, id)
	return result.asResourceManifest(), ok, err
}

func (m *scenarioApiImpl) Delete(ctx context.Context, id VersionedResourceId) (bool, error) {
	return m.store.Delete(ctx, &Scenario{}, id)
}

func (m *scenarioApiImpl) UpdateScript(ctx context.Context, id VersionedResourceId, prob ProbManifest) (CreatedResponse, bool, error) {
	var result Scenario
	kind, ok := wyrd.KindOf(&result.ScenarioSpec)
	if !ok {
		return CreatedResponse{}, false, wyrd.ErrUnknownKind
	}

	ok, err := m.store.GetWithVersion(ctx, &result, id)
	if !ok || err != nil {
		return CreatedResponse{}, ok, err
	}

	result.Prob = prob
	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())

	return CreatedResponse{
		TypeMeta:            wyrd.TypeMeta{Kind: kind},
		VersionedResourceId: result.GetVersionedID(),
	}, ok, err
}

func (m *scenarioApiImpl) Update(ctx context.Context, id VersionedResourceId, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	// Precondition: entry.Spec != nil
	if newEntry.Spec == nil {
		return PartialObjectMetadata{}, ErrResourceSpecIsNil
	}
	spec, ok := newEntry.Spec.(*ScenarioSpec)
	if !ok {
		return PartialObjectMetadata{}, fmt.Errorf("user provided %w", ErrResourceSpecTypeInvalid)
	}

	var result Scenario
	ok, err := m.store.GetWithVersion(ctx, &result, id)
	if err != nil {
		return PartialObjectMetadata{}, err
	}
	if !ok {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	// Identity check
	if result.Name != newEntry.Metadata.Name {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	result.Labels = newEntry.Metadata.Labels
	result.ScenarioSpec = *spec

	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())
	if !ok {
		return PartialObjectMetadata{}, ErrResourceVersionConflict
	}

	return result.asPartialMetadata(), err
}

//------------------------------
/// Scenarios run results
//------------------------------

func (s *resultsApiImpl) scheduleRun(ctx context.Context, runResult Result, scenarioMeta ResourceMeta, scenario *ScenarioSpec) (RunId, error) {
	if s.scheduler == nil || s.workersApi == nil {
		return InvalidRunId, nil
	}

	// TODO: Check if scenario is enabled!
	// if !scenario.IsActive {
	// 	return InvalidRunId, nil
	// }

	// Find all workers qualified to run the scenario:
	requirement, err := scenario.Requirements.AsLabels()
	if err != nil {
		return InvalidRunId, fmt.Errorf("failed to parse scenario requirements: %w", err)
	}

	log.Printf("Scheduling scenario: looking for workers that match: %q", requirement)
	workers, err := s.workersApi.List(ctx, SearchQuery{
		Labels: requirement,
	})
	if err != nil {
		return InvalidRunId, fmt.Errorf("failed to list workers to schedule a scenario: %w", err)
	}

	log.Printf("Scheduling scenario: %v (active=%t); qualified workers: %d", scenarioMeta.GetVersionedID(), scenario.IsActive, len(workers))
	return s.scheduler.Schedule(ctx, scenarioToRunnable(runResult, scenarioMeta, scenario))
}

func (m *resultsApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Result

	// Fixme: Should use typed requirements
	if searchQuery.Labels == "" {
		searchQuery.Labels = fmt.Sprintf("%v=%v", LabelScenarioId, m.scenarioId)
	} else if !strings.Contains(searchQuery.Labels, LabelScenarioId) {
		searchQuery.Labels = fmt.Sprintf("%v=%v,%v", LabelScenarioId, m.scenarioId, searchQuery.Labels)
	}

	_, err := m.store.FindResources(ctx, &resources, searchQuery, paginationLimit)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, resource := range resources {
		results = append(results, resource.asPartialMetadata())
	}

	return results, nil
}

func (m *resultsApiImpl) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	scenarioIdLabelValue := m.scenarioId.String()
	if v, ok := newEntry.Metadata.Labels[LabelScenarioId]; ok && v != scenarioIdLabelValue {
		return PartialObjectMetadata{}, fmt.Errorf("invalid scenario ID for the given results entry")
	}

	// Precondition: newEntry.Spec is either nil or of type &ResultSpec{}
	if newEntry.Spec == nil {
		newEntry.Spec = &ResultSpec{}
	}
	// Precondition: entry.Spec != nil
	spec, ok := newEntry.Spec.(*ResultSpec)
	if !ok {
		return PartialObjectMetadata{}, fmt.Errorf("user provided %w", ErrResourceSpecTypeInvalid)
	}

	scenario, ok, err := m.scenarioApi.getScenario(ctx, m.scenarioId)
	if err != nil {
		return PartialObjectMetadata{}, err
	}
	if !ok {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	if !scenario.ScenarioSpec.IsActive {
		return PartialObjectMetadata{}, ErrForbidden
	}

	if newEntry.Metadata.Name == "" || strings.HasPrefix(newEntry.Metadata.Name, "manual-") { // Generate run name for scheduled runs
		log.Print("manual run, prefix: ", newEntry.Metadata.Name)
		newEntry.Metadata.Name = fmt.Sprintf("%v%v-v%v-%v", newEntry.Metadata.Name, scenario.Name, scenario.Version, randToken(32))
		log.Print("...generated name: ", newEntry.Metadata.Name)
	}

	// Ensure labels are set correctly
	newEntry.Metadata.Labels = wyrd.MergeLabels(
		scenario.Labels,
		newEntry.Metadata.Labels,
		wyrd.Labels{
			LabelScenarioId: scenarioIdLabelValue,
		},
	)

	// Ensure timestamp is set:
	if spec.TimeStarted == nil {
		now := time.Now()
		spec.TimeStarted = &now
	}

	// Ensure initial status is set to pending
	spec.Status = JobPending

	// TODO: Validate that request is from an authentic worker that is allowed to take jobs!
	entry := Result{
		ResourceMeta: GetResourceMetadata(newEntry),
		ResultSpec:   *spec,
	}

	err = m.store.Create(ctx, &entry)
	if err != nil {
		return PartialObjectMetadata{}, err
	}

	_, err = m.scheduleRun(ctx, entry, scenario.ResourceMeta, &scenario.ScenarioSpec)
	if err != nil {
		// Well, scheduling failed. Might as well cancel it:
		entry.Status = JobErrored
		_, uerr := m.store.Update(ctx, &entry, entry.GetVersionedID())
		// TODO: Update metrics!
		if uerr != nil {
			log.Print("embarrassing error: failed to update run DB entry after failure to schedule it: ", uerr)
		}

		// Note: we do want to return original error, to know why we failed to schedule in a first place
		return PartialObjectMetadata{}, err
	}

	return entry.asPartialMetadata(), err
}

func (m *resultsApiImpl) Auth(ctx context.Context, id VersionedResourceId, authRequest AuthJobRequest) (AuthJobResponse, error) {
	var entry Result
	ok, err := m.store.GetWithVersion(ctx, &entry, id)
	if err != nil {
		return AuthJobResponse{}, ErrResourceNotFound
	}
	if !ok {
		return AuthJobResponse{}, ErrResourceUnauthorized
	}

	// Check that no one else took this job
	// Note: This means that no re-try is possible!
	if entry.UpdateToken != "" {
		return AuthJobResponse{}, ErrResourceUnauthorized
	}

	// TODO: Record expected deadline and JWT's exp claim
	entry.Status = JobRunning
	entry.UpdateToken = randToken(32) // FIXME: Generate JWT with valid-until clause, to give worker a time to post
	entry.Labels = wyrd.MergeLabels(
		entry.Labels,
		authRequest.Labels,
		// Last to ensure that LabelScenarioId can not be overriden by the worker labels
		wyrd.Labels{
			LabelScenarioId: m.scenarioId.String(),
		},
	)

	log.Print("authorizing worker ", authRequest.RunnerID, " to execute ", entry.Name, " for at most ", authRequest.Timeout)
	ok, err = m.store.Update(ctx, &entry, entry.GetVersionedID())
	if err != nil {
		return AuthJobResponse{}, err
	}
	if !ok {
		return AuthJobResponse{}, ErrResourceVersionConflict
	}

	return AuthJobResponse{
		CreatedResponse: CreatedResponse{
			VersionedResourceId: entry.GetVersionedID(),
		},
		Token: entry.UpdateToken,
	}, err
}

func (m *resultsApiImpl) Update(ctx context.Context, id VersionedResourceId, token ApiToken, runResults FinalRunResults) (CreatedResponse, error) {
	var entry Result
	ok, err := m.store.GetWithVersion(ctx, &entry, id)
	if err != nil {
		return CreatedResponse{}, ErrResourceNotFound
	}
	if !ok {
		return CreatedResponse{}, ErrResourceVersionConflict
	}

	//FIXME: Validate API Token
	if entry.UpdateToken != token {
		return CreatedResponse{}, ErrResourceUnauthorized
	}

	if runResults.TimeEnded == nil {
		now := time.Now()
		runResults.TimeEnded = &now
	}

	entry.Status = JobCompleted
	entry.FinalRunResults = runResults

	ok, err = m.store.Update(ctx, &entry, entry.GetVersionedID())
	if err != nil {
		return CreatedResponse{}, err
	}
	if !ok {
		return CreatedResponse{}, ErrResourceVersionConflict
	}

	return CreatedResponse{
		TypeMeta:            entry.asResourceManifest().TypeMeta,
		VersionedResourceId: entry.GetVersionedID(),
	}, err
}

func (m *resultsApiImpl) Get(ctx context.Context, id wyrd.ResourceID) (wyrd.ResourceManifest, bool, error) {
	var result Result
	ok, err := m.store.Get(ctx, &result, id)
	// Note, cant' use asManifest yet. Manifest only includes initial results
	return result.asResourceManifest(),
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

//------------------------------
/// Scenarios run results
//------------------------------
func (m *runnersApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Runner
	_, err := m.store.FindResources(ctx, &resources, searchQuery, paginationLimit)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, resource := range resources {
		results = append(results, resource.asPartialMetadata())
	}

	return results, nil
}

func (m *runnersApiImpl) Get(ctx context.Context, id wyrd.ResourceID) (wyrd.ResourceManifest, bool, error) {
	var result Runner
	ok, err := m.store.Get(ctx, &result, id)
	return result.asResourceManifest(),
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

func (m *runnersApiImpl) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	// Precondition: newEntry.Spec is not nil
	if newEntry.Spec == nil {
		return PartialObjectMetadata{}, ErrResourceSpecIsNil
	}
	spec, ok := newEntry.Spec.(*RunnerDefinition)
	if !ok {
		return PartialObjectMetadata{}, ErrResourceSpecTypeInvalid
	}

	entry := Runner{
		ResourceMeta: GetResourceMetadata(newEntry),
		RunnerSpec: RunnerSpec{
			RunnerDefinition: *spec,
		},
		IdToken: randToken(16),
	}

	err := m.store.Create(ctx, &entry)
	return entry.asPartialMetadata(), err
}

func (m *runnersApiImpl) Delete(ctx context.Context, id VersionedResourceId) (bool, error) {
	return m.store.Delete(ctx, &Runner{}, id)
}

func (m *runnersApiImpl) Update(ctx context.Context, id VersionedResourceId, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	// Precondition: entry.Spec != nil
	if newEntry.Spec == nil {
		return PartialObjectMetadata{}, ErrResourceSpecIsNil
	}
	spec, ok := newEntry.Spec.(*RunnerDefinition)
	if !ok {
		return PartialObjectMetadata{}, fmt.Errorf("user provided %w", ErrResourceSpecTypeInvalid)
	}

	var result Runner
	ok, err := m.store.GetWithVersion(ctx, &result, id)
	if err != nil {
		return PartialObjectMetadata{}, err
	}
	if !ok {
		return PartialObjectMetadata{}, ErrResourceVersionConflict
	}

	// Identity check
	if result.Name != newEntry.Metadata.Name {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	result.Labels = newEntry.Metadata.Labels
	result.RunnerDefinition = *spec

	// Persist changes
	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())
	if !ok {
		return PartialObjectMetadata{}, ErrResourceVersionConflict
	}

	return result.asPartialMetadata(), err
}

func (m *runnersApiImpl) Auth(ctx context.Context, token ApiToken, entry RunnerRegistration) (Runner, error) {
	var result Runner
	ok, err := m.store.GetByToken(ctx, &result, token)
	if err != nil {
		return result, err
	}
	if !ok {
		return result, ErrResourceUnauthorized
	}

	// Update runner record:
	result.IsOnline = entry.IsOnline

	// TODO: Figure out a way to combine with Custom user-set labels!
	if entry.InstanceLabels != nil {
		result.Labels = entry.InstanceLabels
	}

	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())
	if !ok {
		return result, ErrResourceVersionConflict
	}

	return result, err
}

//------------------------------
/// ArtifactsApis implementation
//------------------------------
type artifactApiImp struct {
	store Store
}

func (m *artifactApiImp) List(ctx context.Context, query SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Artifact
	_, err := m.store.FindResources(ctx, &resources, query, paginationLimit)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, resource := range resources {
		// Note: Do not return artifact value when listing
		resource.ArtifactSpec.Content = nil
		results = append(results, resource.asPartialMetadata())
	}

	return results, nil
}

func (m *artifactApiImp) Get(ctx context.Context, id wyrd.ResourceID) (wyrd.ResourceManifest, bool, error) {
	var result Artifact
	ok, err := m.store.Get(ctx, &result, id)
	return result.asResourceManifest(),
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

func (m *artifactApiImp) GetContent(ctx context.Context, id wyrd.ResourceID) (resource ArtifactSpec, exists bool, commError error) {
	var result Artifact
	ok, err := m.store.Get(ctx, &result, id)
	return result.ArtifactSpec, ok && result.GetID() == id && !result.IsDeleted(), err
}

func (m *artifactApiImp) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	// Precondition: newEntry.Spec is not nil
	if newEntry.Spec == nil {
		return PartialObjectMetadata{}, ErrResourceSpecIsNil
	}
	spec, ok := newEntry.Spec.(*ArtifactSpec)
	if !ok {
		return PartialObjectMetadata{}, fmt.Errorf("user provided %w", ErrResourceSpecTypeInvalid)
	}

	entry := Artifact{
		ResourceMeta: GetResourceMetadata(newEntry),
		ArtifactSpec: *spec,
	}

	err := m.store.Create(ctx, &entry)
	return entry.asPartialMetadata(), err
}

func (m *artifactApiImp) Delete(ctx context.Context, id VersionedResourceId) (bool, error) {
	return m.store.Delete(ctx, &Artifact{}, id)
}

//------------------------------
// Labels API
//------------------------------

func (api *labelsApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]ResourceLabel, error) {
	var resources []ResourceLabel
	// err := api.store.FindLabels(ctx, &ResourceLabelModel{}, &resources, searchQuery.Pagination)

	return resources, nil
}
