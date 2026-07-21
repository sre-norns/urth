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

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

var (
	ErrUnspecifiedAPIVersion = errors.New("resource has no specified API Version")
	ErrUnspecifiedAPIKind    = errors.New("resource has no specified API Kind")
)

type APIClientConfig struct {
	HTTPClient *http.Client `kong:"-"`

	Token            APIToken      `help:"API token to authenticate to the API server"`
	APIServerAddress string        `help:"URL of the API server" default:"http://localhost:8080/api"`
	Timeout          time.Duration `help:"Communication timeout for API server" default:"1m"`
}

func (c *APIClientConfig) NewClient() (*RestAPIClient, error) {
	return NewRestAPIClient(c.APIServerAddress, *c)
}

type RestAPIClient struct {
	baseURL *url.URL

	config APIClientConfig
}

type serverResourceAPIResponse struct {
	manifest.ResourceManifest

	// Code represents error ID from a relevant domain
	Code int

	// Message is a human readable representation of the error, suitable for display
	Message string
}

func (e *serverResourceAPIResponse) AsError() error {
	if e.Code == 0 {
		return nil
	}

	return fmt.Errorf("server error: %d %s", e.Code, e.Message)
}

func NewRestAPIClient(baseURL string, config APIClientConfig) (*RestAPIClient, error) {
	url, err := url.Parse(baseURL)

	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}

	return &RestAPIClient{
		baseURL: url,
		config:  config,
	}, err
}

// Labels implements the urth.Service interface.
func (c *RestAPIClient) Labels(k manifest.Kind) LabelsAPI {
	return &labelsAPIRestClient{
		RestAPIClient: *c,
		kind:          k,
	}
}

// Runners implements the urth.Service interface.
func (c *RestAPIClient) Runners() RunnersAPI {
	return &runnersAPIClient{
		RestAPIClient: *c,
	}
}

// Workers implements the urth.Service interface.
func (c *RestAPIClient) Workers() WorkersAPI {
	return &workersAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) Scenarios() ScenarioAPI {
	return &scenariosAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) Results(scenarioName manifest.ResourceName) RunResultAPI {
	return &resultsAPIRestClient{
		RestAPIClient: *c,
		ScenarioID:    scenarioName,
	}
}

func (c *RestAPIClient) Artifacts() ArtifactAPI {
	return &artifactAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) resourceAPICall(ctx context.Context, method string, targetAPI *url.URL, data []byte) (result manifest.ResourceManifest, created bool, err error) {
	request, err := c.requestWithAuth(ctx, method, targetAPI, "", nil, bytes.NewReader(data))
	if err != nil {
		return result, false, err
	}
	resp, err := c.config.HTTPClient.Do(request)
	if err != nil {
		return result, false, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return result, false, readAPIError(resp)
	}

	var serverResponse serverResourceAPIResponse
	err = json.NewDecoder(resp.Body).Decode(&serverResponse)
	if err != nil {
		return result, resp.StatusCode == http.StatusCreated, fmt.Errorf("RestApiClient response decoding error: %w", err)
	}

	if serverResponse.Code != 0 {
		return serverResponse.ResourceManifest, resp.StatusCode == http.StatusCreated, serverResponse.AsError()
	}

	return serverResponse.ResourceManifest, resp.StatusCode == http.StatusCreated, nil
}

func (c *RestAPIClient) ApplyObjectDefinition(ctx context.Context, spec manifest.ResourceManifest) (result manifest.ResourceManifest, created bool, err error) {
	targetAPI, err := apiURLForResource(c.baseURL, spec.TypeMeta, spec.Metadata.Name, nil)
	if err != nil {
		return result, created, err
	}

	data, err := json.Marshal(spec)
	if err != nil {
		return result, created, fmt.Errorf("RestApiClient manifest serialization error: %w", err)
	}

	return c.resourceAPICall(ctx, http.MethodPut, targetAPI, data)
}

func (c *RestAPIClient) CreateFromManifest(ctx context.Context, manifest manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	targetAPI, err := apiURLForResource(c.baseURL, manifest.TypeMeta, "", nil)
	if err != nil {
		return result, err
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return result, fmt.Errorf("RestApiClient manifest serialization error: %w", err)
	}

	result, _, err = c.resourceAPICall(ctx, http.MethodPost, targetAPI, data)

	return
}

// func (c *RestApiClient) ScheduleScenarioRun(ctx context.Context, id ResourceID, request CreateScenarioManualRunRequest) (ManualRunRequestResponse, bool, error) {
// 	return ManualRunRequestResponse{}, false, nil
// }

func (c *RestAPIClient) get(ctx context.Context, apiURL *url.URL) (*http.Response, error) {
	return c.getWithAuth(ctx, apiURL, "", nil)
}

func (c *RestAPIClient) post(ctx context.Context, apiURL *url.URL, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	return c.postWithAuth(ctx, apiURL, "", extraHeaders, body)
}

func (c *RestAPIClient) put(ctx context.Context, apiURL *url.URL, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	return c.putWithAuth(ctx, apiURL, "", extraHeaders, body)
}

func (c *RestAPIClient) delete(ctx context.Context, apiURL *url.URL, extraHeaders http.Header, version string) (*http.Response, error) {
	return c.deleteWithAuth(ctx, apiURL, "", extraHeaders, version)
}

func (c *RestAPIClient) requestWithAuth(ctx context.Context, method string, apiURL *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, method, apiURL.String(), body)
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

func (c *RestAPIClient) getWithAuth(ctx context.Context, apiURL *url.URL, token string, extraHeaders http.Header) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodGet, apiURL, token, extraHeaders, nil)
	if err != nil {
		return nil, err
	}

	return c.config.HTTPClient.Do(request)
}

func (c *RestAPIClient) postWithAuth(ctx context.Context, apiURL *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodPost, apiURL, token, extraHeaders, body)
	if err != nil {
		return nil, err
	}

	return c.config.HTTPClient.Do(request)
}

func (c *RestAPIClient) putWithAuth(ctx context.Context, apiURL *url.URL, token string, extraHeaders http.Header, body io.Reader) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodPut, apiURL, token, extraHeaders, body)
	if err != nil {
		return nil, err
	}

	return c.config.HTTPClient.Do(request)
}

func (c *RestAPIClient) deleteWithAuth(ctx context.Context, apiURL *url.URL, token string, extraHeaders http.Header, version string) (*http.Response, error) {
	request, err := c.requestWithAuth(ctx, http.MethodDelete, apiURL, token, extraHeaders, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("If-Match", version)

	return c.config.HTTPClient.Do(request)
}

func readAPIError(resp *http.Response) error {
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

func (c *RestAPIClient) deleteResource(ctx context.Context, uri string, version manifest.Version) (bool, error) {
	strVersion := version.String()
	queryParams := url.Values{}
	queryParams.Set("version", strVersion)

	targetAPI := urlForPath(c.baseURL, uri, queryParams)
	resp, err := c.delete(ctx, targetAPI, nil, strVersion)
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

func listResources[T any](ctx context.Context, c *RestAPIClient, targetAPI *url.URL) (results []T, total int64, err error) {
	resp, err := c.get(ctx, targetAPI)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, readAPIError(resp)
	}

	return readPaginatedResource[T](resp.Body)
}

func (c *RestAPIClient) listResources(ctx context.Context, targetAPI *url.URL) (results []manifest.ResourceManifest, total int64, err error) {
	resp, err := c.get(ctx, targetAPI)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, readAPIError(resp)
	}

	return readPaginatedResource[manifest.ResourceManifest](resp.Body)
}

func (c *RestAPIClient) getResource(ctx context.Context, uri string, dest *manifest.ResourceManifest) (bool, error) {
	targetAPI := urlForPath(c.baseURL, uri, nil)
	resp, err := c.get(ctx, targetAPI)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, readAPIError(resp)
	}

	return true, json.NewDecoder(resp.Body).Decode(dest)
}

func (c *RestAPIClient) getRawResource(ctx context.Context, uri string) (io.ReadCloser, bool, error) {
	targetAPI := urlForPath(c.baseURL, uri, nil)
	resp, err := c.get(ctx, targetAPI)
	if err != nil {
		return nil, false, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, false, readAPIError(resp)
	}

	return resp.Body, true, nil
}

func (c *RestAPIClient) createResource(ctx context.Context, uri string, token string, entry any) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	targetAPI := urlForPath(c.baseURL, uri, nil)
	resp, err := c.postWithAuth(ctx, targetAPI, token, nil, bytes.NewReader(data))
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusCreated {
		return result, readAPIError(resp)
	}

	err = json.NewDecoder(resp.Body).Decode(&result)
	return
}

func apiURLForResource(baseURL *url.URL, typeInfo manifest.TypeMeta, resourceName manifest.ResourceName, query url.Values) (*url.URL, error) {
	if typeInfo.APIVersion == "" {
		return nil, ErrUnspecifiedAPIVersion
	}

	collection := strings.ToLower(string(typeInfo.Kind)) // TODO: Ensure that type name is plural?
	if collection == "" {
		return nil, ErrUnspecifiedAPIKind
	}

	return urlForPath(baseURL, path.Join(typeInfo.APIVersion, collection, string(resourceName)), query), nil
}

func urlForPath(baseURL *url.URL, apiPath string, query url.Values) *url.URL {
	rawQuery := ""
	if len(query) > 0 {
		rawQuery = query.Encode()
	}

	targetPath := baseURL.JoinPath(apiPath)
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

type workersAPIClient struct {
	RestAPIClient
}

// List all resources matching given search query
func (c *workersAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetAPI := urlForPath(c.baseURL, "v1/workers", searchToQuery(searchQuery))
	return c.listResources(ctx, targetAPI)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *workersAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/workers/%v", id), &result)
	return
}

func (c *workersAPIClient) SetPaused(ctx context.Context, id manifest.ResourceName, paused bool) (result manifest.ResourceManifest, exists bool, err error) {
	data, err := json.Marshal(SetPausedRequest{IsPaused: paused})
	if err != nil {
		return
	}

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/workers/%v/paused", id), nil)
	result, _, err = c.resourceAPICall(ctx, http.MethodPut, targetAPI, data)

	return result, err == nil, err
}

func (c *workersAPIClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/workers/%v", id.ID), id.Version)
}

type runnersAPIClient struct {
	RestAPIClient
}

// List all resources matching given search query
func (c *runnersAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetAPI := urlForPath(c.baseURL, "v1/runners", searchToQuery(searchQuery))
	return c.listResources(ctx, targetAPI)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *runnersAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/runners/%v", id), &result)
	return
}

func (c *runnersAPIClient) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	return c.ApplyObjectDefinition(ctx, newEntry)
}

func (c *runnersAPIClient) Create(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/runners", "", &newEntry)
}

func (c *runnersAPIClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/runners/%v", id.ID), id.Version)
}

func (c *runnersAPIClient) Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/runners/%v", id), queryParams)
	resp, err := c.put(ctx,
		targetAPI,
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
		return result, readAPIError(resp)
	}
}

func (c *runnersAPIClient) GetToken(ctx context.Context, runnerName manifest.ResourceName) (APIToken, bool, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/auth/runners/%v", runnerName), nil)
	resp, err := c.getWithAuth(ctx, targetAPI, "", nil)
	if err != nil {
		return APIToken(""), false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		token, err := io.ReadAll(resp.Body)
		return APIToken(token), true, err
	case http.StatusNotFound:
		return APIToken(""), false, nil
	default:
		return APIToken(""), false, readAPIError(resp)
	}
}

func (c *runnersAPIClient) Auth(ctx context.Context, token APIToken, newEntry manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(newEntry)
	if err != nil {
		return result, err
	}

	targetAPI := urlForPath(c.baseURL, "v1/auth/runners", nil)
	resp, err := c.postWithAuth(ctx, targetAPI, string(token), nil, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readAPIError(resp)
	}
}

// --------
// Run Results API
// --------

type resultsAPIRestClient struct {
	RestAPIClient

	ScenarioID manifest.ResourceName
}

// List all resources matching given search query
func (c *resultsAPIRestClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]Result, int64, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioID), searchToQuery(searchQuery))
	return listResources[Result](ctx, &c.RestAPIClient, targetAPI)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *resultsAPIRestClient) Get(ctx context.Context, id manifest.ResourceName) (result Result, exists bool, err error) {
	var resource manifest.ResourceManifest
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioID, id), &resource)
	if !exists || err != nil {
		return
	}
	result, err = NewResult(resource)
	return
}

func (c *resultsAPIRestClient) Create(ctx context.Context, newEntry manifest.ResourceManifest) (Result, error) {
	resource, err := c.createResource(ctx, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioID), "", &newEntry)
	if err != nil {
		return Result{}, err
	}

	return NewResult(resource)
}

func (c *resultsAPIRestClient) Auth(ctx context.Context, resultName manifest.ResourceName, authRequest AuthJobRequest) (result AuthJobResponse, err error) {
	data, err := json.Marshal(authRequest)
	if err != nil {
		return result, err
	}

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/auth/scenarios/%v/%v", c.ScenarioID, resultName), nil)
	// TODO:require JWT to prevent replay attacks
	resp, err := c.postWithAuth(ctx, targetAPI, "", nil, bytes.NewReader(data))
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		err = json.NewDecoder(resp.Body).Decode(&result)
		return
	default:
		return result, readAPIError(resp)
	}
}

func (c *resultsAPIRestClient) UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token APIToken, runResults ResultStatus) (result bark.CreatedResponse, err error) {
	data, err := json.Marshal(runResults)
	if err != nil {
		return result, err
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/scenarios/%v/results/%v/status", c.ScenarioID, id.ID), queryParams)
	resp, err := c.putWithAuth(ctx,
		targetAPI,
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
		return result, readAPIError(resp)
	}
}

// --------
// Labels API
// --------

type labelsAPIRestClient struct {
	RestAPIClient

	kind manifest.Kind
}

func (m *labelsAPIRestClient) ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (manifest.StringSet, int64, error) {
	// "/search/:kind/names"
	kind := strings.ToLower(string(m.kind))
	targetAPI := urlForPath(m.baseURL, path.Join("v1", "search", kind, "names"), searchToQuery(searchQuery))
	// targetApi := apiUrlForPath(m.baseUrl, manifest.TypeMeta{APIVersion: "v1/search", Kind: m.kind}, "names", searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestAPIClient, targetAPI)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

func (m *labelsAPIRestClient) ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (manifest.StringSet, int64, error) {
	// "/search/:kind/labels"
	kind := strings.ToLower(string(m.kind))
	targetAPI := urlForPath(m.baseURL, path.Join("v1", "search", kind, "labels"), searchToQuery(searchQuery))
	// targetApi := apiUrlForPath(m.baseUrl, manifest.TypeMeta{APIVersion: "v1/search", Kind: m.kind}, "names", searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestAPIClient, targetAPI)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

func (m *labelsAPIRestClient) ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (manifest.StringSet, int64, error) {
	// "/search/:kind/labels/:id"
	kind := strings.ToLower(string(m.kind))
	targetAPI := urlForPath(m.baseURL, path.Join("v1", "search", kind, "labels", label), searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestAPIClient, targetAPI)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

// --------
// Artifacts API
// --------

type artifactAPIClient struct {
	RestAPIClient
}

func (c *artifactAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetAPI := urlForPath(c.baseURL, "v1/artifacts", searchToQuery(searchQuery))

	return c.listResources(ctx, targetAPI)
}

func (c *artifactAPIClient) Create(ctx context.Context, token APIToken, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/artifacts", string(token), &entry)
}

func (c *artifactAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/artifacts/%v", id), &result)
	return
}

func (c *artifactAPIClient) GetContent(ctx context.Context, id manifest.ResourceName) (resource ArtifactSpec, exists bool, err error) {
	body, exists, err := c.getRawResource(ctx, fmt.Sprintf("v1/artifacts/%v/content", id))
	if !exists || err != nil {
		return
	}
	defer body.Close()
	resource.Artifact.Content, err = io.ReadAll(body)

	return
}

func (c *artifactAPIClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/artifacts/%v", id.ID), id.Version)
}

// --------
// Scenarios API
// --------

type scenariosAPIClient struct {
	RestAPIClient
}

func (c *scenariosAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, int64, error) {
	targetAPI := urlForPath(c.baseURL, "v1/scenarios", searchToQuery(searchQuery))

	return c.listResources(ctx, targetAPI)
}

func (c *scenariosAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/scenarios/%v", id), &result)
	return
}

func (c *scenariosAPIClient) CreateOrUpdate(ctx context.Context, newEntry manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	return c.ApplyObjectDefinition(ctx, newEntry)
}

func (c *scenariosAPIClient) Create(ctx context.Context, scenario manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/scenarios", "", &scenario)
}

// Delete a single resource identified by a unique ID
func (c *scenariosAPIClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/scenarios/%v", id.ID), id.Version)
}

// Update a single resource identified by a unique ID
func (c *scenariosAPIClient) Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/scenarios/%v", id), queryParams)
	resp, err := c.put(ctx,
		targetAPI,
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
		return result, readAPIError(resp)
	}
}

// ClientAPI?
func (c *scenariosAPIClient) ListRunnable(ctx context.Context, query manifest.SearchQuery) ([]Scenario, error) {
	return nil, nil
}

func (c *scenariosAPIClient) UpdateScript(ctx context.Context, id manifest.VersionedResourceID, prob prob.Manifest) (bark.CreatedResponse, bool, error) {
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
