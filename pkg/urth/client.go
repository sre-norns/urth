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

func NewRestApiClient(baseUrl string) (*RestApiClient, error) {
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

func (c *RestApiClient) post(apiUrl *url.URL, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest("POST", apiUrl.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Content-Type", "application/json")

	return c.httpClient.Do(request)
}

func (c *RestApiClient) delete(apiUrl *url.URL) (*http.Response, error) {
	request, err := http.NewRequest("DELETE", apiUrl.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")

	return c.httpClient.Do(request)
}

func (c *RestApiClient) put(apiUrl *url.URL, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequest("PUT", apiUrl.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("Content-Type", "application/json")
	for k, values := range extraHeaders {
		for _, v := range values {
			request.Header.Add(k, v)
		}
	}

	return c.httpClient.Do(request)
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

type RunnersApiClient struct {
	RestApiClient
}

// List all resources matching given search query
func (c *RunnersApiClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	targetApi := urlForPath(c.baseUrl, "v1/runners", searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var responseObject PaginatedResponse[PartialObjectMetadata]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *RunnersApiClient) Get(ctx context.Context, id ResourceID) (resource Runner, exists bool, commError error) {
	var result Runner
	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/runners/%v", id), nil)
	resp, err := c.get(targetApi)
	if err != nil {
		return result, false, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return result, true, err
	}

	return result, true, nil
}

type RunResultApiRestClient struct {
	RestApiClient

	ScenarioId ResourceID
}

// List all resources matching given search query
func (c *RunResultApiRestClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var responseObject PaginatedResponse[PartialObjectMetadata]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *RunResultApiRestClient) Get(ctx context.Context, id ResourceID) (ScenarioRunResults, bool, error) {
	var result ScenarioRunResults
	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioId, id), nil)
	resp, err := c.get(targetApi)
	if err != nil {
		return result, false, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, true, err
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

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

type scenariosApiClient struct {
	RestApiClient
}

func (c *scenariosApiClient) List(ctx context.Context, searchQuery SearchQuery) ([]PartialObjectMetadata, error) {
	targetApi := urlForPath(c.baseUrl, "v1/scenarios", searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var responseObject PaginatedResponse[PartialObjectMetadata]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}

func (c *scenariosApiClient) Get(ctx context.Context, id ResourceID) (Scenario, bool, error) {
	var result Scenario
	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v", id), nil)
	resp, err := c.get(targetApi)
	if err != nil {
		return result, false, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, true, err
}

func (c *scenariosApiClient) Create(ctx context.Context, scenario CreateScenarioRequest) (CreatedResponse, error) {
	var result CreatedResponse
	data, err := json.Marshal(scenario)
	if err != nil {
		return result, err
	}

	targetApi := urlForPath(c.baseUrl, "v1/scenarios", nil)
	resp, err := c.post(targetApi, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	err = json.NewDecoder(resp.Body).Decode(&result)
	return result, err
}

// Delete a single resource identified by a unique ID
func (c *scenariosApiClient) Delete(ctx context.Context, id ResourceID) (bool, error) {
	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v", id), nil)
	resp, err := c.delete(targetApi)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK, err
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

	var responseObject PaginatedResponse[ResourceLabel]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}

// // Get a single resource given its unique ID,
// // Returns a resource if it exists, false, if resource doesn't exists
// // error if there was communication error with the storage
// func (c *LabelsApiClient) Get(ctx context.Context, id ResourceID) (resource Runner, exists bool, commError error) {
// 	var result Runner
// 	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("runners/%v", id), nil)
// 	resp, err := c.get(targetApi)
// 	if err != nil {
// 		return result, false, err
// 	}
// 	defer resp.Body.Close()

// 	err = json.NewDecoder(resp.Body).Decode(&result)
// 	if err != nil {
// 		return result, true, err
// 	}

// 	return result, true, nil
// }
