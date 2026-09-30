package client

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type runnersAPIClient struct {
	RestAPIClient
}

// List all resources matching given search query
func (c *runnersAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, manifest.Page, error) {
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

func (c *runnersAPIClient) Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.updateResource(ctx, "runners", id, entry)
}

func (c *runnersAPIClient) GetToken(ctx context.Context, runnerName manifest.ResourceName) (urth.APIToken, bool, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/runners/%v/tokens", runnerName), nil)
	resp, err := c.do(ctx, http.MethodPost, targetAPI, "", "", nil)
	if err != nil {
		return urth.APIToken(""), false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusAccepted, http.StatusCreated:
		token, err := io.ReadAll(resp.Body)
		return urth.APIToken(token), true, err
	case http.StatusNotFound:
		return urth.APIToken(""), false, nil
	default:
		return urth.APIToken(""), false, readAPIError(resp)
	}
}

func (c *runnersAPIClient) AuthWorker(ctx context.Context, token urth.APIToken, newEntry manifest.ResourceManifest) (urth.WorkerRegistrationResponse, error) {
	return send[urth.WorkerRegistrationResponse](ctx, &c.RestAPIClient, http.MethodPost, urlForPath(c.baseURL, "v1/auth/workers", nil), string(token), "", newEntry)
}
