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
	"strings"

	"github.com/sre-norns/urth/pkg/bark"
	"github.com/sre-norns/urth/pkg/wyrd"
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
	return &runnersApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) GetScenarioAPI() ScenarioApi {
	return &scenariosApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) GetResultsAPI(id wyrd.ResourceID) RunResultApi {
	return &resultsApiRestClient{
		RestApiClient: *c,
		ScenarioId:    id,
	}
}

func (c *RestApiClient) GetLabels() LabelsApi {
	return &LabelsApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) GetArtifactsApi() ArtifactApi {
	return &artifactApiClient{
		RestApiClient: *c,
	}
}

// func (c *RestApiClient) ApplyObjectDefinition(ctx context.Context, spec wyrd.ResourceManifest) (result PartialObjectMetadata, err error) {
// 	data, err := json.Marshal(spec)
// 	if err != nil {
// 		return
// 	}

// 	// queryParams := url.Values{}
// 	// queryParams.Set("name", spec.Metadata.Name)

// 	targetApi := apiUrlForPath(c.baseUrl, spec.TypeMeta, spec.Metadata.Name, nil)
// 	resp, err := c.post(targetApi, bytes.NewReader(data))
// 	if err != nil {
// 		return result, err
// 	}

// 	defer resp.Body.Close()

// 	err = json.NewDecoder(resp.Body).Decode(&result)
// 	return result, err
// }

func (c *RestApiClient) CreateFromManifest(ctx context.Context, manifest wyrd.ResourceManifest) (result PartialObjectMetadata, err error) {
	data, err := json.Marshal(manifest)
	if err != nil {
		return result, err
	}

	// queryParams := url.Values{}
	// queryParams.Set("name", spec.Metadata.Name)

	targetApi := apiUrlForPath(c.baseUrl, manifest.TypeMeta, "", nil)
	resp, err := c.post(targetApi, bytes.NewReader(data))
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return
}

// func (c *RestApiClient) ScheduleScenarioRun(ctx context.Context, id ResourceID, request CreateScenarioManualRunRequest) (ManualRunRequestResponse, bool, error) {
// 	return ManualRunRequestResponse{}, false, nil
// }

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

func (c *RestApiClient) delete(apiUrl *url.URL, version string) (*http.Response, error) {
	request, err := http.NewRequest("DELETE", apiUrl.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept", "application/json")
	request.Header.Add("If-Match", version)

	return c.httpClient.Do(request)
}

func readApiError(resp *http.Response) error {
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		errorResponse := &bark.ErrorResponse{
			Code:    resp.StatusCode,
			Message: resp.Status,
		}
		err := json.NewDecoder(resp.Body).Decode(errorResponse)
		if err != nil {
			// Failed to unmarshal error message, fallback to HTTP status code
			return errorResponse
		}

		return fmt.Errorf(errorResponse.Message)
	}

	return fmt.Errorf(resp.Status)
}

func (c *RestApiClient) deleteResource(uri string, version wyrd.Version) (bool, error) {
	strVersion := version.String()
	queryParams := url.Values{}
	queryParams.Set("version", strVersion)

	targetApi := urlForPath(c.baseUrl, uri, queryParams)
	resp, err := c.delete(targetApi, strVersion)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK, err
}

func (c *RestApiClient) listResources(uri string, searchQuery bark.SearchQuery) ([]PartialObjectMetadata, error) {
	targetApi := urlForPath(c.baseUrl, uri, searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readApiError(resp)
	}

	var responseObject bark.PaginatedResponse[PartialObjectMetadata]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}

func (c *RestApiClient) getResource(uri string, dest *wyrd.ResourceManifest) (bool, error) {
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

func (c *RestApiClient) getRawResource(uri string) (io.ReadCloser, bool, error) {
	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, false, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, readApiError(resp)
	}

	return resp.Body, true, nil
}

func (c *RestApiClient) createResource(uri string, entry any) (result PartialObjectMetadata, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.post(targetApi, bytes.NewReader(data))
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return
}

func apiUrlForPath(baseUrl *url.URL, typeInfo wyrd.TypeMeta, element string, query url.Values) *url.URL {
	collection := strings.ToLower(string(typeInfo.Kind))
	// TODO: Make plural
	return urlForPath(baseUrl, path.Join(typeInfo.APIVersion, collection, element), query)
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

func searchToQuery(searchQuery bark.SearchQuery) url.Values {
	queryParams := url.Values{}
	if searchQuery.Offset > 0 {
		queryParams.Set("offset", strconv.FormatUint(uint64(searchQuery.Offset), 10))
	}
	if searchQuery.Limit > 0 {
		queryParams.Set("limit", strconv.FormatUint(uint64(searchQuery.Limit), 10))
	}
	if len(searchQuery.Filter) > 0 {
		queryParams.Set("labels", searchQuery.Filter)
	}

	return queryParams
}

// --------
// Runners API
// --------

type runnersApiClient struct {
	RestApiClient
}

// List all resources matching given search query
func (c *runnersApiClient) List(ctx context.Context, searchQuery bark.SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources("v1/runners", searchQuery)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *runnersApiClient) Get(ctx context.Context, id wyrd.ResourceID) (resource wyrd.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(fmt.Sprintf("v1/runners/%v", id), &resource)
	return
}

func (c *runnersApiClient) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	return c.createResource("v1/runners", &newEntry)
}

func (c *runnersApiClient) Delete(ctx context.Context, id wyrd.VersionedResourceId) (bool, error) {
	return c.deleteResource(fmt.Sprintf("v1/runners/%v", id.ID), id.Version)
}

func (c *runnersApiClient) Update(ctx context.Context, id wyrd.VersionedResourceId, entry wyrd.ResourceManifest) (result PartialObjectMetadata, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/runners/%v", id), queryParams)
	resp, err := c.put(targetApi,
		http.Header{
			// "Authorization": []string{fmt.Sprintf("Bearer %s", token)},
			"If-Match": []string{id.String()},
		},
		bytes.NewReader(data),
	)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return
}

func (m *runnersApiClient) Auth(ctx context.Context, token ApiToken, newEntry RunnerRegistration) (Runner, error) {
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

type resultsApiRestClient struct {
	RestApiClient

	ScenarioId wyrd.ResourceID
}

// List all resources matching given search query
func (c *resultsApiRestClient) List(ctx context.Context, searchQuery bark.SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources(fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), searchQuery)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *resultsApiRestClient) Get(ctx context.Context, id wyrd.ResourceID) (result wyrd.ResourceManifest, exist bool, err error) {
	exist, err = c.getResource(fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioId, id), &result)
	return
}

func (c *resultsApiRestClient) Create(ctx context.Context, newEntry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	return c.createResource(fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), &newEntry)
}

func (c *resultsApiRestClient) Auth(ctx context.Context, id wyrd.VersionedResourceId, authRequest AuthJobRequest) (AuthJobResponse, error) {
	var result AuthJobResponse
	data, err := json.Marshal(authRequest)
	if err != nil {
		return result, err
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results/%v/auth", c.ScenarioId, id.ID), queryParams)
	resp, err := c.post(targetApi,
		// http.Header{
		// 	// "Authorization": []string{fmt.Sprintf("Bearer %s", token)},
		// 	"If-Match": []string{id.String()},
		// },
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

func (c *resultsApiRestClient) Update(ctx context.Context, id wyrd.VersionedResourceId, token ApiToken, runResults FinalRunResults) (bark.CreatedResponse, error) {
	var result bark.CreatedResponse
	data, err := json.Marshal(runResults)
	if err != nil {
		return result, err
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

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

func (c *artifactApiClient) List(ctx context.Context, searchQuery bark.SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources("v1/artifacts", searchQuery)
}

func (c *artifactApiClient) Create(ctx context.Context, entry wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	return c.createResource("v1/artifacts", &entry)
}

func (c *artifactApiClient) Get(ctx context.Context, id wyrd.ResourceID) (result wyrd.ResourceManifest, exist bool, err error) {
	exist, err = c.getResource(fmt.Sprintf("v1/artifacts/%v", id), &result)
	return
}

func (c *artifactApiClient) GetContent(ctx context.Context, id wyrd.ResourceID) (resource ArtifactSpec, exists bool, err error) {
	body, exists, err := c.getRawResource(fmt.Sprintf("v1/artifacts/%v/content", id))
	if !exists || err != nil {
		return
	}
	defer body.Close()
	resource.Content, err = io.ReadAll(body)

	return
}

func (c *artifactApiClient) Delete(ctx context.Context, id wyrd.VersionedResourceId) (bool, error) {
	return c.deleteResource(fmt.Sprintf("v1/artifacts/%v", id.ID), id.Version)
}

// --------
// Scenarios API
// --------

type scenariosApiClient struct {
	RestApiClient
}

func (c *scenariosApiClient) List(ctx context.Context, searchQuery bark.SearchQuery) ([]PartialObjectMetadata, error) {
	return c.listResources("v1/scenarios", searchQuery)
}

func (c *scenariosApiClient) Get(ctx context.Context, id wyrd.ResourceID) (result wyrd.ResourceManifest, exist bool, err error) {
	exist, err = c.getResource(fmt.Sprintf("v1/scenarios/%v", id), &result)
	return
}

func (c *scenariosApiClient) Create(ctx context.Context, scenario wyrd.ResourceManifest) (PartialObjectMetadata, error) {
	return c.createResource("v1/scenarios", &scenario)
}

// Delete a single resource identified by a unique ID
func (c *scenariosApiClient) Delete(ctx context.Context, id wyrd.VersionedResourceId) (bool, error) {
	return c.deleteResource(fmt.Sprintf("v1/scenarios/%v", id.ID), id.Version)
}

// Update a single resource identified by a unique ID
func (c *scenariosApiClient) Update(ctx context.Context, id wyrd.VersionedResourceId, entry wyrd.ResourceManifest) (result PartialObjectMetadata, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v", id), queryParams)
	resp, err := c.put(targetApi,
		http.Header{
			// "Authorization": []string{fmt.Sprintf("Bearer %s", token)},
			"If-Match": []string{id.String()},
		},
		bytes.NewReader(data),
	)
	if err != nil {
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return result, readApiError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return
}

// ClientAPI?
func (c *scenariosApiClient) ListRunnable(ctx context.Context, query bark.SearchQuery) ([]Scenario, error) {
	return nil, nil
}

func (c *scenariosApiClient) UpdateScript(ctx context.Context, id wyrd.VersionedResourceId, prob ProbManifest) (bark.CreatedResponse, bool, error) {
	return bark.CreatedResponse{}, false, nil
}

// --------
// Labels API
// --------

type LabelsApiClient struct {
	RestApiClient
}

// List all resources matching given search query
func (c *LabelsApiClient) List(ctx context.Context, searchQuery bark.SearchQuery) ([]ResourceLabel, error) {
	targetApi := urlForPath(c.baseUrl, "v1/labels", searchToQuery(searchQuery))
	resp, err := c.get(targetApi)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, readApiError(resp)
	}

	var responseObject bark.PaginatedResponse[ResourceLabel]
	err = json.NewDecoder(resp.Body).Decode(&responseObject)
	if err != nil {
		return nil, err
	}

	return responseObject.Data, err
}
