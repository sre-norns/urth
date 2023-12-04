package urth

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/wyrd"
)

var (
	ErrResourceUnauthorized    = &ErrorResponse{Code: 401, Message: "resource access unauthorized"}
	ErrForbidden               = &ErrorResponse{Code: 403, Message: "forbidden"}
	ErrResourceNotFound        = &ErrorResponse{Code: 404, Message: "requested resource not found"}
	ErrResourceVersionConflict = &ErrorResponse{Code: 409, Message: "resource version conflict"}
)

type ReadableResourceApi[T interface{}] interface {
	// List all resources matching given search query
	List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error)

	// Get a single resource given its unique ID,
	// Returns a resource if it exists, false, if resource doesn't exists
	// error if there was communication error with the storage
	Get(ctx context.Context, id wyrd.ResourceID) (resource PartialObjectMetadata, exists bool, commError error)
}

type ScenarioApi interface {
	ReadableResourceApi[Scenario]

	Create(ctx context.Context, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	// Delete a single resource identified by a unique ID
	Delete(ctx context.Context, id VersionedResourceId) (bool, error)

	// Update a single resource identified by a unique ID
	Update(ctx context.Context, id VersionedResourceId, entry wyrd.ResourceManifest) (PartialObjectMetadata, error)

	UpdateScript(ctx context.Context, id VersionedResourceId, entry ScenarioScript) (CreatedResponse, bool, error)

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

	Auth(ctx context.Context, runID VersionedResourceId, authRequest AuthRunRequest) (CreatedRunResponse, error)

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
		scenarioApi ScenarioApi
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
		store:       s.store,
		scenarioId:  id,
		scheduler:   s.scheduler,
		scenarioApi: s.GetScenarioAPI(),
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
	_, err := m.store.FindResources(ctx, &resources, query)
	if err != nil {
		return nil, err
	}
	return resources, err
}

func (m *scenarioApiImpl) List(ctx context.Context, query SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Scenario
	kind, ok := wyrd.KindOf(&ScenarioSpec{})
	if !ok {
		return nil, wyrd.ErrUnknownKind
	}

	_, err := m.store.FindResources(ctx, &resources, query)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		// TODO: Script should be moved into a separate table, that way we won't have to filter it out here
		sc.Script = nil
		results = append(results, PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: sc.ResourceMeta,
			Spec:         sc.ScenarioSpec,
		})
	}

	return results, nil
}

func (m *scenarioApiImpl) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	entry := Scenario{
		ResourceMeta: GetMetadata(newEntry),
		ScenarioSpec: *newEntry.Spec.(*ScenarioSpec),
	}

	_, err := m.store.Create(ctx, &entry)

	return entry.asManifest(), err
}

func (m *scenarioApiImpl) Get(ctx context.Context, id wyrd.ResourceID) (PartialObjectMetadata, bool, error) {
	var result Scenario
	ok, err := m.store.Get(ctx, &result, id)
	return result.asManifest(),
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

func (m *scenarioApiImpl) Delete(ctx context.Context, id VersionedResourceId) (bool, error) {
	return m.store.Delete(ctx, &Scenario{}, id)
}

func (m *scenarioApiImpl) UpdateScript(ctx context.Context, id VersionedResourceId, script ScenarioScript) (CreatedResponse, bool, error) {
	var result Scenario
	kind, ok := wyrd.KindOf(&result.ScenarioSpec)
	if !ok {
		return CreatedResponse{}, false, wyrd.ErrUnknownKind
	}

	ok, err := m.store.GetWithVersion(ctx, &result, id)
	if !ok || err != nil {
		return CreatedResponse{}, ok, err
	}

	result.Script = &script
	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())

	return CreatedResponse{
		TypeMeta:            wyrd.TypeMeta{Kind: kind},
		VersionedResourceId: result.GetVersionedID(),
	}, ok, err
}

func (m *scenarioApiImpl) Update(ctx context.Context, id VersionedResourceId, entry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	var result Scenario
	ok, err := m.store.GetWithVersion(ctx, &result, id)
	if err != nil {
		return PartialObjectMetadata{}, err
	}
	if !ok {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	// Identity check
	if result.Name != entry.Metadata.Name {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	result.Labels = entry.Metadata.Labels
	currentScript := result.Script
	newScenario := *entry.Spec.(*ScenarioSpec)

	// Ensure that manifest without a script section does not accidentally deletes a script
	// TODO: A better way to move .script out of `CreateScenario` and into `Scenario` directly
	if newScenario.Script == nil && currentScript != nil {
		newScenario.Script = currentScript
	}
	result.ScenarioSpec = newScenario

	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())
	if !ok {
		return PartialObjectMetadata{}, ErrResourceVersionConflict
	}

	return result.asManifest(), err
}

//------------------------------
/// Scenarios run results
//------------------------------

func (s *resultsApiImpl) scheduleRun(ctx context.Context, runResult Result, meta ResourceMeta, scenario *ScenarioSpec) (RunId, error) {
	if s.scheduler == nil {
		return InvalidRunId, nil
	}

	// TODO: Check if scenario is enabled!
	// if !scenario.IsActive {
	// 	return InvalidRunId, nil
	// }

	log.Printf("Scheduling scenario: %v (active=%t)", meta.GetVersionedID(), scenario.IsActive)
	return s.scheduler.Schedule(ctx, scenarioToRunnable(runResult, meta, scenario))
}

func (m *resultsApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Result
	kind, ok := wyrd.KindOf(&InitialRunResults{})
	if !ok {
		return nil, wyrd.ErrUnknownKind
	}

	// Fixme: Should use typed requirements
	if searchQuery.Labels == "" {
		searchQuery.Labels = fmt.Sprintf("%v=%v", LabelScenarioId, m.scenarioId)
	} else if !strings.Contains(searchQuery.Labels, LabelScenarioId) {
		searchQuery.Labels = fmt.Sprintf("%v=%v,%v", LabelScenarioId, m.scenarioId, searchQuery.Labels)
	}

	_, err := m.store.FindResources(ctx, &resources, searchQuery)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		results = append(results, PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: sc.ResourceMeta,
			Spec:         sc.ResultSpec,
		})
	}

	return results, nil
}

func (m *resultsApiImpl) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	scenarioIdLabelValue := m.scenarioId.String()
	if v, ok := newEntry.Metadata.Labels[LabelScenarioId]; ok && v != scenarioIdLabelValue {
		return PartialObjectMetadata{}, fmt.Errorf("invalid scenario ID for the given results entry")
	}

	scenarioManifest, ok, err := m.scenarioApi.Get(ctx, m.scenarioId)
	if err != nil {
		return PartialObjectMetadata{}, err
	}
	if !ok {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	scenario := scenarioManifest.Spec.(*ScenarioSpec)
	if !scenario.IsActive {
		return PartialObjectMetadata{}, ErrForbidden
	}

	if newEntry.Metadata.Name == "" || strings.HasPrefix(newEntry.Metadata.Name, "manual-") { // Generate run name for scheduled runs
		log.Print("manual run, prefix: ", newEntry.Metadata.Name)
		newEntry.Metadata.Name = fmt.Sprintf("%v%v-v%v-%v", newEntry.Metadata.Name, scenarioManifest.Name, scenarioManifest.Version, randToken(32))
		log.Print("...generated name: ", newEntry.Metadata.Name)
	}

	// Ensure labels are set correctly
	newEntry.Metadata.Labels = wyrd.MergeLabels(
		scenarioManifest.Labels,
		newEntry.Metadata.Labels,
		wyrd.Labels{
			LabelScenarioId: scenarioIdLabelValue,
		},
	)

	spec := newEntry.Spec.(*InitialRunResults)
	// Ensure timestamp is set:
	if spec.TimeStarted == nil {
		now := time.Now()
		spec.TimeStarted = &now
	}

	// TODO: Validate that request is from an authentic worker that is allowed to take jobs!

	entry := Result{
		ResourceMeta: GetMetadata(newEntry),
		ResultSpec: ResultSpec{
			Status:            JobPending, // Ensure initial status is set
			InitialRunResults: *spec,
		},
	}

	_, err = m.store.Create(ctx, &entry)
	if err != nil {
		return PartialObjectMetadata{}, err
	}

	_, err = m.scheduleRun(ctx, entry, scenarioManifest.ResourceMeta, scenario)
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

	return entry.asManifest(), err
}

func (m *resultsApiImpl) Auth(ctx context.Context, id VersionedResourceId, authRequest AuthRunRequest) (CreatedRunResponse, error) {
	var entry Result
	ok, err := m.store.GetWithVersion(ctx, &entry, id)
	if err != nil {
		return CreatedRunResponse{}, ErrResourceNotFound
	}
	if !ok {
		return CreatedRunResponse{}, ErrResourceUnauthorized
	}

	// Check that no one else took this job
	// Note: This means that no re-try is possible!
	if entry.UpdateToken != "" {
		return CreatedRunResponse{}, ErrResourceUnauthorized
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
		return CreatedRunResponse{}, err
	}
	if !ok {
		return CreatedRunResponse{}, ErrResourceVersionConflict
	}

	return CreatedRunResponse{
		CreatedResponse: CreatedResponse{
			VersionedResourceId: entry.GetVersionedID(),
		},
		Token: entry.UpdateToken,
	}, err
}

func (m *resultsApiImpl) Update(ctx context.Context, id VersionedResourceId, token ApiToken, runResults FinalRunResults) (CreatedResponse, error) {
	var entry Result
	kind, ok := wyrd.KindOf(&entry.InitialRunResults)
	if !ok {
		return CreatedResponse{}, wyrd.ErrUnknownKind
	}

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
		TypeMeta:            wyrd.TypeMeta{Kind: kind},
		VersionedResourceId: entry.GetVersionedID(),
	}, err
}

func (m *resultsApiImpl) Get(ctx context.Context, id wyrd.ResourceID) (PartialObjectMetadata, bool, error) {
	var result Result
	kind, ok := wyrd.KindOf(&result.InitialRunResults)
	if !ok {
		return PartialObjectMetadata{}, ok, wyrd.ErrUnknownKind
	}

	ok, err := m.store.Get(ctx, &result, id)
	return PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: result.ResourceMeta,
			Spec:         &result.ResultSpec,
		},
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

//------------------------------
/// Scenarios run results
//------------------------------
func (m *runnersApiImpl) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	var resources []Runner
	kind, ok := wyrd.KindOf(&RunnerDefinition{})
	if !ok {
		return nil, wyrd.ErrUnknownKind
	}

	_, err := m.store.FindResources(ctx, &resources, searchQuery)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		// TODO: Token should be hidden?
		sc.IdToken = ""
		// sc.
		results = append(results, PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: sc.ResourceMeta,
			Spec:         sc.RunnerSpec,
		})
	}

	return results, nil
}

func (m *runnersApiImpl) Get(ctx context.Context, id wyrd.ResourceID) (PartialObjectMetadata, bool, error) {
	var result Runner
	kind, ok := wyrd.KindOf(&result.RunnerDefinition)
	if !ok {
		return PartialObjectMetadata{}, ok, wyrd.ErrUnknownKind
	}

	ok, err := m.store.Get(ctx, &result, id)
	return PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: result.ResourceMeta,
			Spec:         &result.RunnerSpec,
		},
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

func (m *runnersApiImpl) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	entry := Runner{
		ResourceMeta: GetMetadata(newEntry),
		RunnerSpec: RunnerSpec{
			RunnerDefinition: *newEntry.Spec.(*RunnerDefinition),
		},
		IdToken: randToken(16),
	}

	_, err := m.store.Create(ctx, &entry)

	return entry.asManifest(), err
}

func (m *runnersApiImpl) Delete(ctx context.Context, id VersionedResourceId) (bool, error) {
	return m.store.Delete(ctx, &Runner{}, id)
}

func (m *runnersApiImpl) Update(ctx context.Context, id VersionedResourceId, entry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	var result Runner
	ok, err := m.store.GetWithVersion(ctx, &result, id)
	if err != nil {
		return PartialObjectMetadata{}, err
	}
	if !ok {
		return PartialObjectMetadata{}, ErrResourceVersionConflict
	}

	// Identity check
	if result.Name != entry.Metadata.Name {
		return PartialObjectMetadata{}, ErrResourceNotFound
	}

	result.Labels = entry.Metadata.Labels
	result.RunnerDefinition = *entry.Spec.(*RunnerDefinition)

	// Persist changes
	ok, err = m.store.Update(ctx, &result, result.GetVersionedID())
	if !ok {
		return PartialObjectMetadata{}, ErrResourceVersionConflict
	}

	return result.asManifest(), err
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
	kind, ok := wyrd.KindOf(&ArtifactSpec{})
	if !ok {
		return nil, wyrd.ErrUnknownKind
	}

	_, err := m.store.FindResources(ctx, &resources, query)
	if err != nil {
		return nil, err
	}

	results := make([]PartialObjectMetadata, 0, len(resources))
	for _, sc := range resources {
		// Note: Do not return artifact value when listing
		sc.ArtifactSpec.Content = nil
		results = append(results, PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: sc.ResourceMeta,
			Spec:         sc.ArtifactSpec,
		})
	}

	return results, nil
}

func (m *artifactApiImp) Get(ctx context.Context, id wyrd.ResourceID) (PartialObjectMetadata, bool, error) {
	var result Artifact
	kind, ok := wyrd.KindOf(&result.ArtifactSpec)
	if !ok {
		return PartialObjectMetadata{}, ok, wyrd.ErrUnknownKind
	}

	ok, err := m.store.Get(ctx, &result, id)
	return PartialObjectMetadata{
			TypeMeta:     wyrd.TypeMeta{Kind: kind},
			ResourceMeta: result.ResourceMeta,
			Spec:         &result.ArtifactSpec,
		},
		ok && result.GetID() == id && !result.IsDeleted(),
		err
}

func (m *artifactApiImp) GetContent(ctx context.Context, id wyrd.ResourceID) (resource ArtifactSpec, exists bool, commError error) {
	var result Artifact
	ok, err := m.store.Get(ctx, &result, id)
	return result.ArtifactSpec, ok && result.GetID() == id && !result.IsDeleted(), err
}

func (m *artifactApiImp) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	entry := Artifact{
		ResourceMeta: GetMetadata(newEntry),
		ArtifactSpec: *newEntry.Spec.(*ArtifactSpec),
	}

	_, err := m.store.Create(ctx, &entry)
	return entry.asManifest(), err
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
