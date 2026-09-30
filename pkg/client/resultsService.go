package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// allResultsAPIClient reads runs across every scenario, rather than within one.
type allResultsAPIClient struct {
	RestAPIClient
}

// List all resources matching given search query
func (c *allResultsAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]urth.Result, manifest.Page, error) {
	targetAPI := urlForPath(c.baseURL, "v1/results", searchToQuery(searchQuery))
	return listResources[urth.Result](ctx, &c.RestAPIClient, targetAPI)
}

// Get a single run by name, without needing to know its scenario.
func (c *allResultsAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result urth.Result, exists bool, err error) {
	var resource manifest.ResourceManifest
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/results/%v", id), &resource)
	if !exists || err != nil {
		return
	}

	result, err = urth.NewResult(resource)

	return
}

type resultsAPIRestClient struct {
	RestAPIClient

	ScenarioID manifest.ResourceName
}

// List all resources matching given search query
func (c *resultsAPIRestClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]urth.Result, manifest.Page, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioID), searchToQuery(searchQuery))
	return listResources[urth.Result](ctx, &c.RestAPIClient, targetAPI)
}

// Get a single resource given its unique ID,
// Returns a resource if it exists, false, if resource doesn't exists
// error if there was communication error with the storage
func (c *resultsAPIRestClient) Get(ctx context.Context, id manifest.ResourceName) (result urth.Result, exists bool, err error) {
	var resource manifest.ResourceManifest
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/scenarios/%v/results/%v", c.ScenarioID, id), &resource)
	if !exists || err != nil {
		return
	}
	result, err = urth.NewResult(resource)
	return
}

func (c *resultsAPIRestClient) Create(ctx context.Context, newEntry manifest.ResourceManifest) (urth.Result, error) {
	resource, err := c.createResource(ctx, fmt.Sprintf("v1/scenarios/%v/results", c.ScenarioID), "", &newEntry)
	if err != nil {
		return urth.Result{}, err
	}

	return urth.NewResult(resource)
}

func (c *resultsAPIRestClient) ClaimRun(ctx context.Context, resultUID manifest.ResourceID, session urth.APIToken, request urth.ClaimJobRequest) (urth.AuthJobResponse, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/auth/runs/%v/claim", resultUID), nil)
	return send[urth.AuthJobResponse](ctx, &c.RestAPIClient, http.MethodPost, targetAPI, string(session), "", request)
}

func (c *resultsAPIRestClient) UpdateStatus(ctx context.Context, id manifest.VersionedResourceID, token urth.APIToken, runResults urth.ResultStatus) (bark.CreatedResponse, error) {
	queryParams := url.Values{}
	queryParams.Set("version", id.Version.String())

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/scenarios/%v/results/%v/status", c.ScenarioID, id.ID), queryParams)
	return send[bark.CreatedResponse](ctx, &c.RestAPIClient, http.MethodPut, targetAPI, string(token), id.String(), runResults)
}
