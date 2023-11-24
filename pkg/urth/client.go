package urth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
)

type RestApiClient struct {
	baseUrl    *url.URL
	httpClient *http.Client
}

func NewRestApiClient(baseUrl string) (Service, error) {
	url, err := url.Parse(baseUrl)

	return &RestApiClient{
		baseUrl:    url,
		httpClient: &http.Client{},
	}, err
}

func (c *RestApiClient) GetRunnerAPI() RunnersApi {
	return &RunnersApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) GetScenarioAPI() ScenarioApi {
	return &scenariosApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) GetResultsAPI(id ResourceID) RunResultApi {
	return &RunResultApiRestClient{
		RestApiClient: *c,
		ScenarioId:    id,
	}
}

func (c *RestApiClient) GetLabels() LabelsApi {
	return &LabelsApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) GetScheduler() Scheduler {
	return nil
}

func (c *RestApiClient) GetArtifactsApi() ArtifactApi {
	return &artifactApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) ApplyObjectDefinition(ctx context.Context, spec ResourceManifest) (CreatedResponse, error) {
	var result CreatedResponse
	data, err := json.Marshal(spec)
	if err != nil {
		return result, err
	}

	// queryParams := url.Values{}
	// queryParams.Set("name", spec.Metadata.Name)

	targetApi := apiUrlForPath(c.baseUrl, spec.TypeMeta, spec.Metadata.Name, nil)
	resp, err := c.post(targetApi, bytes.NewReader(data))
	if err != nil {
		return result, err
	}

	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c *RestApiClient) ScheduleScenarioRun(ctx context.Context, id ResourceID, request CreateScenarioManualRunRequest) (ManualRunRequestResponse, bool, error) {
	return ManualRunRequestResponse{}, false, nil
}

func (c *RestApiClient) get(apiUrl *url.URL) (*http.Response, error) {
	request, err := http.NewRequest("GET", apiUrl.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")

	return c.httpClient.Do(request)
}

func (c *RestApiClient) postWithAuth(apiUrl *url.URL, token string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest("POST", apiUrl.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Content-Type", "application/json")
	if token != "" {
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %v", token))

	}

	return c.httpClient.Do(request)
}

func (c *RestApiClient) post(apiUrl *url.URL, body io.Reader) (*http.Response, error) {
	return c.postWithAuth(apiUrl, "", body)
}

func (c *RestApiClient) putWithAuth(apiUrl *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest("PUT", apiUrl.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Content-Type", "application/json")
	if token != "" {
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %v", token))

	}
	for k, values := range extraHeaders {
		for _, v := range values {
			request.Header.Add(k, v)
		}
	}

	return c.httpClient.Do(request)
}

func (c *RestApiClient) put(apiUrl *url.URL, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	return c.putWithAuth(apiUrl, "", extraHeaders, body)
}

func (c *RestApiClient) delete(apiUrl *url.URL) (*http.Response, error) {
	request, err := http.NewRequest("DELETE", apiUrl.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")

	return c.httpClient.Do(request)
}

func readApiError(resp *http.Response) error {
	if resp.StatusCode < 500 {
		var errorResponse ErrorResponse
		err := json.NewDecoder(resp.Body).Decode(&errorResponse)
		if err != nil {
			return err
		}

		return fmt.Errorf(errorResponse.Message)
	}

	return fmt.Errorf(resp.Status)
}

func (c *RestApiClient) deleteResource(uri string) (bool, error) {
	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.delete(targetApi)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK, err
}

func (c *RestApiClient) listResources(uri string, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	targetApi := urlForPath(c.baseUrl, uri, searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readApiError(resp)
	}

	var responseObject PaginatedResponse[PartialObjectMetadata]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}

func (c *RestApiClient) getResource(uri string, dest any) (bool, error) {
	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.get(targetApi)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, readApiError(resp)
	}

	return true, json.NewDecoder(resp.Body).Decode(dest)
}

func (c *RestApiClient) createResource(uri string, entry any) (CreatedResponse, error) {
	var result CreatedResponse
	data, err := json.Marshal(entry)
	if err != nil {
		return result, err
	}

	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.post(targetApi, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func apiUrlForPath(baseUrl *url.URL, typeInfo TypeMeta, element string, query url.Values) *url.URL {
	return urlForPath(baseUrl, path.Join(typeInfo.APIVersion, typeInfo.Kind, element), query)
}

func urlForPath(baseUrl *url.URL, apiPath string, query url.Values) *url.URL {
	rawQuery := ""
	if query != nil {
		rawQuery = query.Encode()
	}

	// baseUrl.JoinPath(path), only in go 19
	return &url.URL{
		Scheme:   baseUrl.Scheme,
		Opaque:   baseUrl.Opaque,
		User:     baseUrl.User,
		Host:     baseUrl.Host,
		Path:     path.Join(baseUrl.Path, "api", apiPath),
		RawQuery: rawQuery,
	}
}

func searchToQuery(searchQuery SearchQuery) url.Values {
	queryParams := url.Values{}
	if searchQuery.Offset > 0 {
		queryParams.Set("offset", strconv.FormatUint(uint64(searchQuery.Offset), 10))
	}
	if searchQuery.Limit > 0 {
		queryParams.Set("limit", strconv.FormatUint(uint64(searchQuery.Limit), 10))
	}
	if len(searchQuery.Labels) > 0 {
		queryParams.Set("labels", searchQuery.Labels)
	}

	return queryParams
}

// --------
// Runners API
// --------

type RunnersApiClient struct {
	RestApiClient
}

// List all resources matching given search query
func (c *RunnersApiClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources("v1/runners", searchQuery)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *RunnersApiClient) Get(ctx context.Context, id ResourceID) (resource Runner, exists bool, commError error) {
	var result Runner
	exists, err := c.getResource(fmt.Sprintf("v1/runners/%v", id), &result)
	return result, exists, err
}

func (c *RunnersApiClient) Create(ctx context.Context, newEntry CreateRunnerRequest) (CreatedResponse, error) {
	return c.createResource("v1/runners", &newEntry)
}

func (c *RunnersApiClient) Delete(ctx context.Context, id ResourceID) (bool, error) {
	return c.deleteResource(fmt.Sprintf("v1/runners/%v", id))
}

func (c *RunnersApiClient) Update(ctx context.Context, id VersionedResourceId, entry CreateRunnerRequest) (CreatedResponse, error) {
	var result CreatedResponse
	data, err := json.Marshal(entry)
	if err != nil {
		return result, err
	}

	queryParams := url.Values{}
	queryParams.Set("version", strconv.FormatInt(int64(id.Version), 10))

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/runners/%v", id), queryParams)
	resp, err := c.put(targetApi,
		http.Header{
			// "Authorization": []string{fmt.Sprintf("Bearer %s", token)},
			"If-Match": []string{id.String()},
		},
		bytes.NewReader(data),
	)
	if err != nil {
		return result, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (m *RunnersApiClient) Auth(ctx context.Context, token ApiToken, newEntry RunnerRegistration) (Runner, error) {
	var result Runner
	data, err := json.Marshal(newEntry)
	if err != nil {
		return result, err
	}

	targetApi := urlForPath(m.baseUrl, "v1/runners", nil)
	resp, err := m.putWithAuth(targetApi, string(token), nil, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

// --------
// Run Results API
// --------

type RunResultApiRestClient struct {
	RestApiClient

	ScenarioId ResourceID
}

// List all resources matching given search query
func (c *RunResultApiRestClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources(fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), searchQuery)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *RunResultApiRestClient) Get(ctx context.Context, id ResourceID) (ScenarioRunResults, bool, error) {
	var result ScenarioRunResults
	exists, err := c.getResource(fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioId, id), &result)
	return result, exists, err
}

func (c *RunResultApiRestClient) Create(ctx context.Context, runResults CreateScenarioRunResults) (CreatedRunResponse, error) {
	var result CreatedRunResponse
	data, err := json.Marshal(runResults)
	if err != nil {
		return result, err
	}

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), nil)
	resp, err := c.post(targetApi, bytes.NewReader(data))
	if err != nil {
		return result, err
	}

	defer resp.Body.Close()
	if (resp.StatusCode != http.StatusCreated) && (resp.StatusCode != http.StatusAccepted) && resp.StatusCode != http.StatusOK {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

func (c *RunResultApiRestClient) Update(ctx context.Context, id VersionedResourceId, token ApiToken, runResults FinalRunResults) (CreatedResponse, error) {
	var result CreatedResponse
	data, err := json.Marshal(runResults)
	if err != nil {
		return result, err
	}

	queryParams := url.Values{}
	queryParams.Set("version", strconv.FormatInt(int64(id.Version), 10))

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioId, id.ID), queryParams)
	resp, err := c.put(targetApi,
		http.Header{
			"Authorization": []string{fmt.Sprintf("Bearer %s", token)},
			"If-Match":      []string{id.String()},
		},
		bytes.NewReader(data),
	)
	if err != nil {
		return result, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

// --------
// Artifacts API
// --------

type artifactApiClient struct {
	RestApiClient
}

func (c *artifactApiClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources("v1/artifacts", searchQuery)
}

func (c *artifactApiClient) Create(ctx context.Context, entry CreateArtifactRequest) (CreatedResponse, error) {
	return c.createResource("v1/artifacts", &entry)
}

func (c *artifactApiClient) Get(ctx context.Context, id ResourceID) (Artifact, bool, error) {
	var result Artifact
	exists, err := c.getResource(fmt.Sprintf("v1/artifacts/%v", id), &result)
	return result, exists, err
}

func (c *artifactApiClient) Delete(ctx context.Context, id ResourceID) (bool, error) {
	return c.deleteResource(fmt.Sprintf("v1/artifacts/%v", id))
}

// --------
// Scenarios API
// --------

type scenariosApiClient struct {
	RestApiClient
}

func (c *scenariosApiClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources("v1/scenarios", searchQuery)
}

func (c *scenariosApiClient) Get(ctx context.Context, id ResourceID) (Scenario, bool, error) {
	var result Scenario
	exists, err := c.getResource(fmt.Sprintf("v1/scenarios/%v", id), &result)
	return result, exists, err
}

func (c *scenariosApiClient) Create(ctx context.Context, scenario CreateScenarioRequest) (CreatedResponse, error) {
	return c.createResource("v1/scenarios", &scenario)
}

// Delete a single resource identified by a unique ID
func (c *scenariosApiClient) Delete(ctx context.Context, id ResourceID) (bool, error) {
	return c.deleteResource(fmt.Sprintf("v1/scenarios/%v", id))
}

// Update a single resource identified by a unique ID
func (c *scenariosApiClient) Update(ctx context.Context, id ResourceID, scenario CreateScenario) (CreatedResponse, error) {
	return CreatedResponse{}, nil
}

// ClientAPI?
func (c *scenariosApiClient) ListRunnable(ctx context.Context, query SearchQuery) ([]Scenario, error) {
	return nil, nil
}

func (c *scenariosApiClient) UpdateScript(ctx context.Context, id ResourceID, script ScenarioScript) (VersionedResourceId, bool, error) {
	return VersionedResourceId{}, false, nil
}

// --------
// Labels API
// --------

type LabelsApiClient struct {
	RestApiClient
}

// List all resources matching given search query
func (c *LabelsApiClient) List(ctx context.Context, searchQuery SearchQuery) ([]ResourceLabel, error) {
	targetApi := urlForPath(c.baseUrl, "v1/labels", searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readApiError(resp)
	}

	var responseObject PaginatedResponse[ResourceLabel]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}
