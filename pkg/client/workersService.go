package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type workersAPIClient struct {
	RestAPIClient
}

// List all resources matching given search query
func (c *workersAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, manifest.Page, error) {
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
	data, err := json.Marshal(urth.SetPausedRequest{IsPaused: paused})
	if err != nil {
		return
	}

	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/workers/%v/paused", id), nil)
	result, _, err = c.resourceAPICall(ctx, http.MethodPut, targetAPI, data, "")

	return result, err == nil, err
}

func (c *workersAPIClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, fmt.Sprintf("v1/workers/%v", id.ID), id.Version)
}

// Heartbeat reports that this worker is still there.
//
// The session goes in the Authorization header and the worker's identity is read
// from it server-side, so there is no worker ID in the path or the body.
func (c *workersAPIClient) Heartbeat(ctx context.Context, session urth.APIToken, request urth.WorkerHeartbeatRequest) (urth.WorkerHeartbeatResponse, error) {
	return send[urth.WorkerHeartbeatResponse](ctx, &c.RestAPIClient, http.MethodPost, urlForPath(c.baseURL, "v1/auth/workers/heartbeat", nil), string(session), "", request)
}
