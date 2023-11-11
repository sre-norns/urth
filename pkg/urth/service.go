package urth

import (
	"context"
	"fmt"
	"log"
	"reflect"
)

type ReadableRecourseApi[T interface{}] interface {
	// List all resources matching given search query
	List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error)

	// Get a single resource given its unique ID,
	// Returns a resource if it exists, false, if resource doesn't exists
	// error if there was communication error with the storage
	Get(ctx context.Context, id ResourceID) (resource T, exists bool, commError error)
}

type ScenarioApi interface {
	ReadableRecourseApi[Scenario]

	Create(ctx context.Context, scenario CreateScenario) (CreatedResponse, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id ResourceID) (bool, error)

	// Update a single resource identified by a unique ID
	Update(ctx context.Context, id ResourceID, scenario CreateScenario) (CreatedResponse, error)

	UpdateScript(ctx context.Context, id ResourceID, script ScenarioScript) (VersionedResourceId, bool, error)

	// ClientAPI?
	ListRunnable(ctx context.Context, query SearchQuery) ([]Scenario, error)
}

type RunResultApi interface {
	ReadableRecourseApi[ScenarioRunResults]

	Create(ctx context.Context, runResults CreateScenarioRunResults) (CreatedRunResponse, error)

	Update(ctx context.Context, id VersionedResourceId, token ApiToken, runResults FinalRunResults) (CreatedResponse, error)
}

type RunnersApi interface {
	ReadableRecourseApi[Runner]

	// UserControl() error
	// PostUpdate() error
}

type LabelsApi interface {
	List(ctx context.Context, searchQuery SearchQuery) ([]ResourceLabel, error)
}

type Service interface {
	GetRunnerAPI() RunnersApi
	GetScenarioAPI() ScenarioApi
	GetResultsAPI(ResourceID) RunResultApi

	GetLabels() LabelsApi

	GetScheduler() Scheduler

	ScheduleScenarioRun(ctx context.Context, id ResourceID, request CreateScenarioManualRunRequest) (ManualRunRequestResponse, bool, error)
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
		store      Store
		scenarioId ResourceID
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

func (s *serviceImpl) GetResultsAPI(id ResourceID) RunResultApi {
	return &resultsApiImpl{
		store:      s.store,
		scenarioId: id,
	}
}

func (s *serviceImpl) GetLabels() LabelsApi {
	return &labelsApiImpl{
		store: s.store,
	}
}

func (s *serviceImpl) GetScheduler() Scheduler {
	return s.scheduler
}

func (s *serviceImpl) ScheduleScenarioRun(ctx context.Context, id ResourceID, request CreateScenarioManualRunRequest) (ManualRunRequestResponse, bool, error) {
	scenario, ok, err := s.GetScenarioAPI().Get(ctx, id)
	if !ok || err != nil {
		return ManualRunRequestResponse{}, ok, err
	}

	if s.scheduler == nil {
		return ManualRunRequestResponse{}, true, err
	}

	log.Printf("Scheduling manually: %v (active=%t)", scenario.GetVersionedID(), scenario.IsActive)
	runId, err := s.scheduler.Schedule(ctx, ScenarioToRunnable(scenario))
	if err != nil {
		return ManualRunRequestResponse{}, ok, err
	}

	return ManualRunRequestResponse{
		RunId: runId,
	}, true, err
}

//------------------------------
/// Scenarios API
//------------------------------
func (m *scenarioApiImpl) ListRunnable(ctx context.Context, query SearchQuery) ([]Scenario, error) {
	var resources []Scenario
	_, err := m.store.FindResources(ctx, &resources, query)
	if err != nil {
		return nil, err
	}
	return resources, err
}

func (m *scenarioApiImpl) List(ctx context.Context, query SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Scenario
	kind, err := m.store.FindResources(ctx, &resources, query)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		// TODO: Script should be moved into a separate table, that way we wont have to filter it out
		sc.Script = nil
		results = append(results, PartialObjectMetadata{
			TypeMeta:     kind,
			ResourceMeta: sc.ResourceMeta,
			Spec:         sc.CreateScenario,
		})
	}

	return results, nil
}

func (m *scenarioApiImpl) Create(ctx context.Context, newEntry CreateScenario) (CreatedResponse, error) {
	entry := Scenario{CreateScenario: newEntry}
	kind, err := m.store.GuessKind(reflect.ValueOf(&entry))
	if err != nil {
		return CreatedResponse{}, err
	}

	err = m.store.Create(ctx, &entry)

	return CreatedResponse{
		TypeMeta:            kind,
		VersionedResourceId: NewVersionedId(entry.ID, entry.Version),
	}, err
}

func (m *scenarioApiImpl) Get(ctx context.Context, id ResourceID) (Scenario, bool, error) {
	var result Scenario
	_, err := m.store.Get(ctx, &result, id)

	return result, result.ID == uint(id) && !result.DeletedAt.Valid, err
}

func (m *scenarioApiImpl) Delete(ctx context.Context, id ResourceID) (bool, error) {
	return m.store.Delete(ctx, &Scenario{}, id)
}

func (m *scenarioApiImpl) UpdateScript(ctx context.Context, id ResourceID, script ScenarioScript) (VersionedResourceId, bool, error) {
	var result Scenario
	ok, err := m.store.Get(ctx, &result, id)
	if err != nil || !ok {
		return result.GetVersionedID(), ok, err
	}

	result.Script = &script
	result.Version += 1
	_, err = m.store.Update(ctx, &result, id)

	return result.GetVersionedID(), true, err
}

// FIXME: Must take versionedID?
// TODO: Return kind!
func (m *scenarioApiImpl) Update(ctx context.Context, id ResourceID, scenarioUpdate CreateScenario) (CreatedResponse, error) {
	var result Scenario
	kind, err := m.store.GuessKind(reflect.ValueOf(&result))
	if err != nil {
		return CreatedResponse{}, err
	}

	_, err = m.store.Get(ctx, &result, id)
	if err != nil {
		return CreatedResponse{}, err
	}

	// // TODO: Update other fields!
	// if len(scenario.Requirements.MatchLabels) > 0 {
	// 	resource.Requirements.MatchLabels = scenario.Requirements.MatchLabels
	// }
	// if len(scenario.Requirements.MatchSelector) > 0 {
	// 	resource.Requirements.MatchSelector = scenario.Requirements.MatchSelector
	// }

	if scenarioUpdate.RunSchedule != "" {
		result.RunSchedule = scenarioUpdate.RunSchedule
	}

	if scenarioUpdate.Script.Kind != "" {
		result.Script = scenarioUpdate.Script
	}

	result.IsActive = scenarioUpdate.IsActive

	result.Version += 1
	_, err = m.store.Update(ctx, &result, id)

	return CreatedResponse{
		TypeMeta:            kind,
		VersionedResourceId: result.GetVersionedID(),
	}, err
}

//------------------------------
/// Scenarios run results
//------------------------------
func (m *resultsApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []ScenarioRunResults
	kind, err := m.store.FindResources(ctx, &resources, searchQuery)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		results = append(results, PartialObjectMetadata{
			TypeMeta:     kind,
			ResourceMeta: sc.ResourceMeta,
		})
	}

	return results, nil
}

func (m *resultsApiImpl) Create(ctx context.Context, newEntry CreateScenarioRunResults) (CreatedRunResponse, error) {
	if newEntry.ScenarioID.ID != m.scenarioId {
		return CreatedRunResponse{}, fmt.Errorf("invalid scenario ID for given results entry")
	}

	entry := ScenarioRunResults{
		CreateScenarioRunResults: newEntry,
		UpdateToken:              "super-secret", // FIXME: Generate JWT
	}
	kind, err := m.store.GuessKind(reflect.ValueOf(&entry))
	if err != nil {
		return CreatedRunResponse{}, err
	}

	err = m.store.Create(ctx, &entry)

	return CreatedRunResponse{
		CreatedResponse: CreatedResponse{
			TypeMeta:            kind,
			VersionedResourceId: NewVersionedId(entry.ID, entry.Version),
		},
		Token: entry.UpdateToken,
	}, err
}

func (m *resultsApiImpl) Update(ctx context.Context, id VersionedResourceId, token ApiToken, runResults FinalRunResults) (CreatedResponse, error) {
	var entry ScenarioRunResults

	kind, err := m.store.GuessKind(reflect.ValueOf(&entry))
	if err != nil {
		return CreatedResponse{}, err
	}

	ok, err := m.store.GetWithVersion(ctx, &entry, id)
	if !ok || err != nil {
		return CreatedResponse{}, fmt.Errorf("requested resource not found") // FIXME: 404!
	}

	// Validate API Token
	if entry.UpdateToken != token {
		return CreatedResponse{}, fmt.Errorf("invalid token") // FIXME: 404!
	}

	entry.FinalRunResults = runResults
	ok, err = m.store.Update(ctx, &entry, ResourceID(entry.ID))
	if !ok && err == nil {
		return CreatedResponse{}, fmt.Errorf("update failed to find resource by ID")
	}

	return CreatedResponse{
		TypeMeta:            kind,
		VersionedResourceId: NewVersionedId(entry.ID, entry.Version),
	}, err
}

func (m *resultsApiImpl) Get(ctx context.Context, id ResourceID) (ScenarioRunResults, bool, error) {
	var result ScenarioRunResults
	_, err := m.store.Get(ctx, &result, id)

	return result, result.ID == uint(id) && !result.DeletedAt.Valid, err
}

//------------------------------
/// Scenarios run results
//------------------------------
func (m *runnersApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Runner
	kind, err := m.store.FindResources(ctx, &resources, searchQuery)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		results = append(results, PartialObjectMetadata{
			TypeMeta:     kind,
			ResourceMeta: sc.ResourceMeta,
		})
	}

	return results, nil
}

func (m *runnersApiImpl) Get(ctx context.Context, id ResourceID) (Runner, bool, error) {
	var result Runner
	_, err := m.store.Get(ctx, &result, id)

	return result, result.ID == uint(id) && !result.DeletedAt.Valid, err
}

//------------------------------
// Labels API
//------------------------------

func (api *labelsApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]ResourceLabel, error) {
	var resources []ResourceLabel
	err := api.store.FindInto(ctx, &ResourceLabelModel{}, &resources, searchQuery.Pagination)

	return resources, err
}
