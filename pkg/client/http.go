package client

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

	"github.com/google/uuid"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

var (
	ErrUnspecifiedAPIVersion = errors.New("resource has no specified API Version")
	ErrUnspecifiedAPIKind    = errors.New("resource has no specified API Kind")
)

// RequestOptions are the controls of one request that are not part of any
// resource body.
type RequestOptions struct {
	// IfMatch replaces the If-Match a write would otherwise derive from the
	// version the resource carries: a caller holding an ETag from an earlier
	// read, or `*` to write unconditionally.
	IfMatch string
	// IdempotencyKey names a POST so that its retry is recognised as the same
	// operation. A POST without one gets a fresh key: retrying it is a new
	// operation. Reuse the key to retry the same one.
	IdempotencyKey string
}

type optionsKey struct{}

// WithRequestOptions attaches options to every request made with ctx.
func WithRequestOptions(ctx context.Context, options RequestOptions) context.Context {
	return context.WithValue(ctx, optionsKey{}, options)
}

func requestOptions(ctx context.Context) RequestOptions {
	options, _ := ctx.Value(optionsKey{}).(RequestOptions)
	return options
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

// do sends one request. token, when set, replaces the configured one: a
// worker's session authenticates its own calls. ifMatch is the precondition the
// operation derives; RequestOptions.IfMatch overrides it.
func (c *RestAPIClient) do(ctx context.Context, method string, apiURL *url.URL, token string, ifMatch string, body io.Reader) (*http.Response, error) {
	scopedURL, err := c.scopedURL(ctx, method, apiURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, scopedURL.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	if token == "" {
		token = string(c.config.Token)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	options := requestOptions(ctx)
	if options.IfMatch != "" && ifMatch != "" {
		ifMatch = options.IfMatch
	}
	if ifMatch != "" {
		request.Header.Set(bark.HTTPHeaderIfMatch, ifMatch)
	}
	if method == http.MethodPost {
		key := options.IdempotencyKey
		if key == "" {
			key = uuid.NewString()
		}
		request.Header.Set(bark.HTTPHeaderIdempotencyKey, key)
	}

	return c.config.HTTPClient.Do(request)
}

func (c *RestAPIClient) get(ctx context.Context, apiURL *url.URL) (*http.Response, error) {
	return c.do(ctx, http.MethodGet, apiURL, "", "", nil)
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

// decodeAccepted decodes a successful response into result, or returns the
// server's error.
func decodeAccepted[T any](resp *http.Response, result *T) error {
	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		return json.NewDecoder(resp.Body).Decode(result)
	default:
		return readAPIError(resp)
	}
}

// send marshals request, sends it, and decodes the response into result.
func send[T any](ctx context.Context, c *RestAPIClient, method string, apiURL *url.URL, token string, ifMatch string, request any) (result T, err error) {
	var body io.Reader
	if request != nil {
		data, err := json.Marshal(request)
		if err != nil {
			return result, err
		}
		body = bytes.NewReader(data)
	}

	resp, err := c.do(ctx, method, apiURL, token, ifMatch, body)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()

	return result, decodeAccepted(resp, &result)
}

func (c *RestAPIClient) resourceAPICall(ctx context.Context, method string, targetAPI *url.URL, data []byte, ifMatch string) (result manifest.ResourceManifest, created bool, err error) {
	resp, err := c.do(ctx, method, targetAPI, "", ifMatch, bytes.NewReader(data))
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

func (c *RestAPIClient) deleteResource(ctx context.Context, uri string, version manifest.Version) (bool, error) {
	strVersion := version.String()
	queryParams := url.Values{}
	queryParams.Set("version", strVersion)

	targetAPI := urlForPath(c.baseURL, uri, queryParams)
	resp, err := c.do(ctx, http.MethodDelete, targetAPI, "", strVersion, nil)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		// A refused delete -- a stale version, no permission -- is an error,
		// not "nothing was there".
		return false, readAPIError(resp)
	}
}

func readPaginatedResource[T any](reader io.Reader) (results []T, page manifest.Page, err error) {
	var responseObject struct {
		Items []T `json:"items"`
		manifest.Page
	}
	err = json.NewDecoder(reader).Decode(&responseObject)
	if err != nil {
		return
	}

	return responseObject.Items, responseObject.Page, err
}

func listResources[T any](ctx context.Context, c *RestAPIClient, targetAPI *url.URL) (results []T, page manifest.Page, err error) {
	resp, err := c.get(ctx, targetAPI)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, manifest.Page{}, readAPIError(resp)
	}

	return readPaginatedResource[T](resp.Body)
}

func (c *RestAPIClient) listResources(ctx context.Context, targetAPI *url.URL) (results []manifest.ResourceManifest, page manifest.Page, err error) {
	return listResources[manifest.ResourceManifest](ctx, c, targetAPI)
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
		defer resp.Body.Close()
		return nil, false, readAPIError(resp)
	}

	return resp.Body, true, nil
}

func (c *RestAPIClient) createResource(ctx context.Context, uri string, token string, entry any) (result manifest.ResourceManifest, err error) {
	return send[manifest.ResourceManifest](ctx, c, http.MethodPost, urlForPath(c.baseURL, uri, nil), token, "", entry)
}

// updateResource replaces a named resource. PUT addresses the resource by name;
// If-Match carries the version the update is based on.
func (c *RestAPIClient) updateResource(ctx context.Context, collection string, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/%s/%v", collection, url.PathEscape(string(entry.Metadata.Name))), nil)
	return send[manifest.ResourceManifest](ctx, c, http.MethodPut, targetAPI, "", bark.ETag(id.Version), entry)
}

func apiURLForResource(baseURL *url.URL, typeInfo manifest.TypeMeta, resourceName manifest.ResourceName, query url.Values) (*url.URL, error) {
	if typeInfo.APIVersion == "" {
		return nil, ErrUnspecifiedAPIVersion
	}

	if !urth.AcceptsAPIVersion(typeInfo.APIVersion) {
		return nil, fmt.Errorf("unsupported apiVersion %q", typeInfo.APIVersion)
	}

	collection := strings.ToLower(string(typeInfo.Kind)) // TODO: Ensure that type name is plural?
	if collection == "" {
		return nil, ErrUnspecifiedAPIKind
	}

	return urlForPath(baseURL, path.Join("v1", collection, string(resourceName)), query), nil
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
	if searchQuery.Name != "" {
		queryParams.Set("name", searchQuery.Name)
	}
	if !searchQuery.FromTime.IsZero() {
		queryParams.Set("from", searchQuery.FromTime.Format(time.RFC3339Nano))
	}
	if !searchQuery.TillTime.IsZero() {
		queryParams.Set("till", searchQuery.TillTime.Format(time.RFC3339Nano))
	}
	if searchQuery.Fields != nil && !searchQuery.Fields.Empty() {
		queryParams.Set("fields", searchQuery.Fields.String())
	}
	if searchQuery.Cursor != "" {
		queryParams.Set("cursor", searchQuery.Cursor)
	}
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

// scopedURL places a collection path under the account or project it belongs
// to: runners and workers are an account's, everything a project runs is the
// project's. The scope comes from the request's context, else the client's
// configuration. Worker calls authenticated by a session are not scoped; the
// session says whose they are.
func (c *RestAPIClient) scopedURL(ctx context.Context, method string, original *url.URL) (*url.URL, error) {
	prefix, rest, found := strings.Cut("/"+strings.TrimPrefix(original.Path, "/"), "/v1/")
	if !found || strings.HasPrefix(rest, "auth/") || strings.HasSuffix(rest, "/status") || (method == http.MethodPost && (rest == "artifacts" || rest == "dispatch-failures")) {
		return original, nil
	}
	scope := urth.RequestScope(ctx)
	if scope.Account == "" {
		scope.Account = c.config.Account
	}
	if scope.Project == "" {
		scope.Project = c.config.Project
	}
	collection := strings.Split(rest, "/")[0]
	if strings.HasPrefix(rest, "search/runners/") || strings.HasPrefix(rest, "search/workers/") {
		collection = "runners"
	}
	if collection == "dispatch-failures" && scope.Project == "" {
		collection = "runners"
	}
	scopedPath := ""
	switch collection {
	case "runners", "workers":
		if scope.Account == "" {
			return nil, fmt.Errorf("an account is required")
		}
		scopedPath = "accounts/" + url.PathEscape(string(scope.Account)) + "/"
	case "scenarios", "results", "artifacts", "dispatch-failures", "runner-authorizations", "search":
		if scope.Project == "" {
			return nil, fmt.Errorf("a project is required")
		}
		scopedPath = "projects/" + url.PathEscape(string(scope.Project)) + "/"
	default:
		return original, nil
	}
	copy := *original
	copy.Path = prefix + "/v1/" + scopedPath + rest
	copy.RawPath = ""
	return &copy, nil
}
