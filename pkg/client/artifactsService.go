package client

import (
	"context"
	"fmt"
	"io"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type artifactAPIClient struct {
	RestAPIClient
}

func (c *artifactAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]manifest.ResourceManifest, manifest.Page, error) {
	targetAPI := urlForPath(c.baseURL, "v1/artifacts", searchToQuery(searchQuery))

	return c.listResources(ctx, targetAPI)
}

func (c *artifactAPIClient) Create(ctx context.Context, token urth.APIToken, entry manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.createResource(ctx, "v1/artifacts", string(token), &entry)
}

func (c *artifactAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result manifest.ResourceManifest, exists bool, err error) {
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/artifacts/%v", id), &result)
	return
}

func (c *artifactAPIClient) GetContent(ctx context.Context, id manifest.ResourceName) (resource urth.ArtifactSpec, exists bool, err error) {
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
