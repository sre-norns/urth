package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type scenariosAPIClient struct {
	RestAPIClient
}

func (c *scenariosAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, manifest.Page, error) {
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
func (c *scenariosAPIClient) Update(ctx context.Context, id manifest.VersionedResourceID, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.updateResource(ctx, "scenarios", id, entry)
}

// ClientAPI?
func (c *scenariosAPIClient) ListRunnable(ctx context.Context, query manifest.SearchQuery) ([]urth.Scenario, error) {
	return nil, nil
}

func (c *scenariosAPIClient) UpdateScript(ctx context.Context, id manifest.VersionedResourceID, prob prob.Manifest) (bark.CreatedResponse, bool, error) {
	return bark.CreatedResponse{}, false, nil
}

// Placement implements ScenarioAPI.
func (c *scenariosAPIClient) Placement(ctx context.Context, id manifest.ResourceName) (result urth.PlacementPreview, exists bool, err error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/scenarios/%v/placement", id), nil)

	resp, err := c.get(ctx, targetAPI)
	if err != nil {
		return result, false, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		return result, true, json.NewDecoder(resp.Body).Decode(&result)
	case http.StatusNotFound:
		// A scenario that is not there is not an error for a caller asking
		// whether it could run.
		return result, false, nil
	default:
		return result, false, readAPIError(resp)
	}
}
