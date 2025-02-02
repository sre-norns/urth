package urth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

var (
	ErrUnspecifiedApiVersion = errors.New("resource has no specified API Version")
	ErrUnspecifiedApiKind    = errors.New("resource has no specified API Kind")
)

type ApiClientConfig struct {
	HttpClient *http.Client `kong:"-"`

	Token            ApiToken      `help:"API token to register this runner instance"`
	ApiServerAddress string        `help:"URL address of the API server" default:"http://localhost:8080/api"`
	Timeout          time.Duration `help:"Communication timeout for API server" default:"1m"`
}

func (c *ApiClientConfig) NewClient() (*RestApiClient, error) {
	return NewRestApiClient(c.ApiServerAddress, *c)
}

type RestApiClient struct {
	baseUrl *url.URL

	config ApiClientConfig
}

type serverResourceAPIResponse struct {
	bark.ErrorResponse
	manifest.ResourceManifest
}

func (e *serverResourceAPIResponse) AsError() error {
	return nil
}

type serverPaginatedAPIResponse struct {
	bark.ErrorResponse
	manifest.ResourceManifest
}

func NewRestApiClient(baseUrl string, config ApiClientConfig) (*RestApiClient, error) {
	url, err := url.Parse(baseUrl)

	if config.HttpClient == nil {
		config.HttpClient = http.DefaultClient
	}

	return &RestApiClient{
		baseUrl: url,
		config:  config,
	}, err
}

// Implementation of urth.Service interface
func (c *RestApiClient) Labels(k manifest.Kind) LabelsApi {
	return &labelsApiRestClient{
		RestApiClient: *c,
		kind:          k,
	}
}

// Implementation of urth.Service interface
func (c *RestApiClient) Runners() RunnersApi {
	return &runnersApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) Scenarios() ScenarioApi {
	return &scenariosApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) Results(scenarioName manifest.ResourceName) RunResultApi {
	return &resultsApiRestClient{
		RestApiClient: *c,
		ScenarioId:    scenarioName,
	}
}

func (c *RestApiClient) Artifacts() ArtifactApi {
	return &artifactApiClient{
		RestApiClient: *c,
	}
}

func (c *RestApiClient) resourceAPICall(ctx context.Context, method string, targetApi *url.URL, data []byte) (result manifest.ResourceManifest, created bool, err error) {
	request, err := c.requestWithAuth(ctx, method, targetApi, "", nil, bytes.NewReader(data))
	if err != nil {
		return result, false, err
	}
	resp, err := c.config.HttpClient.Do(request)
	if err != nil {
		return result, false, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return result, false, readApiError(resp)
	}

	var serverResponse serverResourceAPIResponse
	err = json.NewDecoder(resp.Body).Decode(&serverResponse)
	if err != nil {
		return result, resp.StatusCode == http.StatusCreated, fmt.Errorf("RestApiClient response decoding error: %w", err)
	}

	if serverResponse.Code != 0 {
		return serverResponse.ResourceManifest, resp.StatusCode == http.StatusCreated, &serverResponse
	}

	return serverResponse.ResourceManifest, resp.StatusCode == http.StatusCreated, nil
}

func (c *RestApiClient) ApplyObjectDefinition(ctx context.Context, spec manifest.ResourceManifest) (result manifest.ResourceManifest, created bool, err error) {
	targetApi, err := apiUrlForResource(c.baseUrl, spec.TypeMeta, spec.Metadata.Name, nil)
	if err != nil {
		return result, created, err
	}

	data, err := json.Marshal(spec)
	if err != nil {
		return result, created, fmt.Errorf("RestApiClient manifest serialization error: %w", err)
	}

	return c.resourceAPICall(ctx, http.MethodPut, targetApi, data)
}

func (c *RestApiClient) CreateFromManifest(ctx context.Context, manifest manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	targetApi, err := apiUrlForResource(c.baseUrl, manifest.TypeMeta, "", nil)
	if err != nil {
		return result, err
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return result, fmt.Errorf("RestApiClient manifest serialization error: %w", err)
	}

	result, _, err = c.resourceAPICall(ctx, http.MethodPost, targetApi, data)

	return
}

// func (c *RestApiClient) ScheduleScenarioRun(ctx context.Context, id ResourceID, request CreateScenarioManualRunRequest) (ManualRunRequestResponse, bool, error) {
// 	return ManualRunRequestResponse{}, false, nil
// }

func (c *RestApiClient) get(ctx context.Context, apiUrl *url.URL) (*http.Response, error) {
	return c.getWithAuth(ctx, apiUrl, "", nil)
}

func (c *RestApiClient) post(ctx context.Context, apiUrl *url.URL, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	return c.postWithAuth(ctx, apiUrl, "", extraHeaders, body)
}

func (c *RestApiClient) put(ctx context.Context, apiUrl *url.URL, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	return c.putWithAuth(ctx, apiUrl, "", extraHeaders, body)
}

func (c *RestApiClient) delete(ctx context.Context, apiUrl *url.URL, extraHeaders http.Header, version string) (*http.Response, error) {
	return c.deleteWithAuth(ctx, apiUrl, "", extraHeaders, version)
}

func (c *RestApiClient) requestWithAuth(ctx context.Context, method string, apiUrl *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, apiUrl.String(), body)
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

	return request, nil
}

func (c *RestApiClient) getWithAuth(ctx context.Context, apiUrl *url.URL, token string, extraHeaders http.Header) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodGet, apiUrl, token, extraHeaders, nil)
	if err != nil {
		return nil, err
	}

	return c.config.HttpClient.Do(request)
}

func (c *RestApiClient) postWithAuth(ctx context.Context, apiUrl *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodPost, apiUrl, token, extraHeaders, body)
	if err != nil {
		return nil, err
	}

	return c.config.HttpClient.Do(request)
}

func (c *RestApiClient) putWithAuth(ctx context.Context, apiUrl *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodPut, apiUrl, token, extraHeaders, body)
	if err != nil {
		return nil, err
	}

	return c.config.HttpClient.Do(request)
}

func (c *RestApiClient) deleteWithAuth(ctx context.Context, apiUrl *url.URL, token string, extraHeaders http.Header, version string) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodDelete, apiUrl, token, extraHeaders, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("If-Match", version)

	return c.config.HttpClient.Do(request)
}

func readApiError(resp *http.Response) error {
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		errorResponse := &bark.ErrorResponse{
			Code:    resp.StatusCode,
			Message: resp.Status,
		}

		// Try to read error response body, if any
		if err := json.NewDecoder(resp.Body).Decode(errorResponse); err != nil {
			return fmt.Errorf("non-specific api response: %s", resp.Status)
		}

		return errorResponse
	}

	return fmt.Errorf("non-specific api response: %s", resp.Status)
}

func (c *RestApiClient) deleteResource(ctx context.Context, uri string, version manifest.Version) (bool, error) {
	strVersion := version.String()
	queryParams := url.Values{}
	queryParams.Set("version", strVersion)

	targetApi := urlForPath(c.baseUrl, uri, queryParams)
	resp, err := c.delete(ctx, targetApi, nil, strVersion)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK, err
}

func readPaginatedResource[T any](reader io.Reader) (results []T, total int64, err error) {
	var responseObject bark.PaginatedResponse[T]
	err = json.NewDecoder(reader).Decode(&responseObject)
	if err != nil {
		return
	}

	return responseObject.Data, responseObject.Total, err
}

func listResources[T any](ctx context.Context, c *RestApiClient, targetApi *url.URL) (results []T, total int64, err error) {
	resp, err := c.get(ctx, targetApi)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, readApiError(resp)
	}

	return readPaginatedResource[T](resp.Body)
}

func (c *RestApiClient) listResources(ctx context.Context, targetApi *url.URL) (results []manifest.ResourceManifest, total int64, err error) {
	resp, err := c.get(ctx, targetApi)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, readApiError(resp)
	}

	return readPaginatedResource[manifest.ResourceManifest](resp.Body)
}

func (c *RestApiClient) getResource(ctx context.Context, uri string, dest *manifest.ResourceManifest) (bool, error) {
	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.get(ctx, targetApi)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, readApiError(resp)
	}

	return true, json.NewDecoder(resp.Body).Decode(dest)
}

func (c *RestApiClient) getRawResource(ctx context.Context, uri string) (io.ReadCloser, bool, error) {
	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.get(ctx, targetApi)
	if err != nil {
		return nil, false, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, readApiError(resp)
	}

	return resp.Body, true, nil
}

func (c *RestApiClient) createResource(ctx context.Context, uri string, token string, entry any) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	targetApi := urlForPath(c.baseUrl, uri, nil)
	resp, err := c.postWithAuth(ctx, targetApi, token, nil, bytes.NewReader(data))
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

func apiUrlForResource(baseUrl *url.URL, typeInfo manifest.TypeMeta, resourceName manifest.ResourceName, query url.Values) (*url.URL, error) {
	if typeInfo.APIVersion == "" {
		return nil, ErrUnspecifiedApiVersion
	}

	collection := strings.ToLower(string(typeInfo.Kind)) // TODO: Ensure that type name is plural?
	if collection == "" {
		return nil, ErrUnspecifiedApiKind
	}

	return urlForPath(baseUrl, path.Join(typeInfo.APIVersion, collection, string(resourceName)), query), nil
}

func urlForPath(baseUrl *url.URL, apiPath string, query url.Values) *url.URL {
	rawQuery := ""
	if len(query) > 0 {
		rawQuery = query.Encode()
	}

	targetPath := baseUrl.JoinPath(apiPath)
	targetPath.RawQuery = rawQuery

	return targetPath
}

func searchToQuery(searchQuery manifest.SearchQuery) url.Values {
	queryParams := url.Values{}
	if searchQuery.Offset > 0 {
		queryParams.Set("offset", strconv.FormatUint(uint64(searchQuery.Offset), 10))
	}
	if searchQuery.Limit > 0 {
		queryParams.Set("limit", strconv.FormatUint(uint64(searchQuery.Limit), 10))
	}
	if searchQuery.Selector != nil && !searchQuery.Selector.Empty() {
		queryParams.Set("labels", searchQuery.Selector.String())
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
func (c *runnersApiClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetApi := urlForPath(c.baseUrl, "v1/runners", searchToQuery(searchQuery))
	return c.listResources(ctx, targetApi)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *runnersApiClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/runners/%v", id), &result)
	return
}

func (c *runnersApiClient) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	return c.ApplyObjectDefinition(ctx, newEntry)
}

func (c *runnersApiClient) Create(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/runners", "", &newEntry)
}

func (c *runnersApiClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/runners/%v", id.ID), id.Version)
}

func (c *runnersApiClient) Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/runners/%v", id), queryParams)
	resp, err := c.put(ctx,
		targetApi,
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

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readApiError(resp)
	}
}

func (m *runnersApiClient) Auth(ctx context.Context, token ApiToken, newEntry manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(newEntry)
	if err != nil {
		return result, err
	}

	targetApi := urlForPath(m.baseUrl, "v1/auth/runners", nil)
	resp, err := m.postWithAuth(ctx, targetApi, string(token), nil, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readApiError(resp)
	}
}

// --------
// Run Results API
// --------

type resultsApiRestClient struct {
	RestApiClient

	ScenarioId manifest.ResourceName
}

// List all resources matching given search query
func (c *resultsApiRestClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]Result, int64, error) {
	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), searchToQuery(searchQuery))
	return listResources[Result](ctx, &c.RestApiClient, targetApi)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *resultsApiRestClient) Get(ctx context.Context, id manifest.ResourceName) (result Result, exists bool, err error) {
	var resource manifest.ResourceManifest
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioId, id), &resource)
	if !exists || err != nil {
		return
	}
	result, err = NewResult(resource)
	return
}

func (c *resultsApiRestClient) Create(ctx context.Context, newEntry manifest.ResourceManifest) (Result, error) {
	resource, err := c.createResource(ctx, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioId), "", &newEntry)
	if err != nil {
		return Result{}, err
	}

	return NewResult(resource)
}

func (c *resultsApiRestClient) Auth(ctx context.Context, resultName manifest.ResourceName, authRequest AuthJobRequest) (result AuthJobResponse, err error) {
	data, err := json.Marshal(authRequest)
	if err != nil {
		return result, err
	}

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/auth/scenarios/%v/%v", c.ScenarioId, resultName), nil)
	// TODO:require JWT to prevent replay attacks
	resp, err := c.postWithAuth(ctx, targetApi, "", nil, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readApiError(resp)
	}
}

func (c *resultsApiRestClient) UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token ApiToken, runResults ResultStatus) (result bark.CreatedResponse, err error) {
	data, err := json.Marshal(runResults)
	if err != nil {
		return result, err
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v/results/%v/status", c.ScenarioId, id.ID), queryParams)
	resp, err := c.putWithAuth(ctx,
		targetApi,
		string(token),
		http.Header{
			"If-Match": []string{id.String()},
		},
		bytes.NewReader(data),
	)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readApiError(resp)
	}
}

// --------
// Labels API
// --------

type labelsApiRestClient struct {
	RestApiClient

	kind manifest.Kind
}

func (m *labelsApiRestClient) ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (manifest.StringSet, int64, error) {
	// "/search/:kind/names"
	kind := strings.ToLower(string(m.kind))
	targetApi := urlForPath(m.baseUrl, path.Join("v1", "search", kind, "names"), searchToQuery(searchQuery))
	// targetApi := apiUrlForPath(m.baseUrl, manifest.TypeMeta{APIVersion: "v1/search", Kind: m.kind}, "names", searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestApiClient, targetApi)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

func (m *labelsApiRestClient) ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (manifest.StringSet, int64, error) {
	// "/search/:kind/labels"
	kind := strings.ToLower(string(m.kind))
	targetApi := urlForPath(m.baseUrl, path.Join("v1", "search", kind, "labels"), searchToQuery(searchQuery))
	// targetApi := apiUrlForPath(m.baseUrl, manifest.TypeMeta{APIVersion: "v1/search", Kind: m.kind}, "names", searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestApiClient, targetApi)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

func (m *labelsApiRestClient) ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (manifest.StringSet, int64, error) {
	// "/search/:kind/labels/:id"
	kind := strings.ToLower(string(m.kind))
	targetApi := urlForPath(m.baseUrl, path.Join("v1", "search", kind, "labels", label), searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestApiClient, targetApi)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

// --------
// Artifacts API
// --------

type artifactApiClient struct {
	RestApiClient
}

func (c *artifactApiClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetApi := urlForPath(c.baseUrl, "v1/artifacts", searchToQuery(searchQuery))

	return c.listResources(ctx, targetApi)
}

func (c *artifactApiClient) Create(ctx context.Context, token ApiToken, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/artifacts", string(token), &entry)
}

func (c *artifactApiClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/artifacts/%v", id), &result)
	return
}

func (c *artifactApiClient) GetContent(ctx context.Context, id manifest.ResourceName) (resource ArtifactSpec, exists bool, err error) {
	body, exists, err := c.getRawResource(ctx, fmt.Sprintf("v1/artifacts/%v/content", id))
	if !exists || err != nil {
		return
	}
	defer body.Close()
	resource.Content, err = io.ReadAll(body)

	return
}

func (c *artifactApiClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/artifacts/%v", id.ID), id.Version)
}

// --------
// Scenarios API
// --------

type scenariosApiClient struct {
	RestApiClient
}

func (c *scenariosApiClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetApi := urlForPath(c.baseUrl, "v1/scenarios", searchToQuery(searchQuery))

	return c.listResources(ctx, targetApi)
}

func (c *scenariosApiClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/scenarios/%v", id), &result)
	return
}

func (m *scenariosApiClient) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	return m.ApplyObjectDefinition(ctx, newEntry)
}

func (c *scenariosApiClient) Create(ctx context.Context, scenario manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/scenarios", "", &scenario)
}

// Delete a single resource identified by a unique ID
func (c *scenariosApiClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/scenarios/%v", id.ID), id.Version)
}

// Update a single resource identified by a unique ID
func (c *scenariosApiClient) Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetApi := urlForPath(c.baseUrl, fmt.Sprintf("v1/scenarios/%v", id), queryParams)
	resp, err := c.put(ctx,
		targetApi,
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

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readApiError(resp)
	}
}

// ClientAPI?
func (c *scenariosApiClient) ListRunnable(ctx context.Context, query manifest.SearchQuery) ([]Scenario, error) {
	return nil, nil
}

func (c *scenariosApiClient) UpdateScript(ctx context.Context, id manifest.VersionedResourceID, prob ProbManifest) (bark.CreatedResponse, bool, error) {
	return bark.CreatedResponse{}, false, nil
}

// --------
// Labels API
// --------
/*
type LabelsApiClient struct {
	RestApiClient
}

// List all resources matching given search query
func (c *LabelsApiClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]ResourceLabel, error) {
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
*/
