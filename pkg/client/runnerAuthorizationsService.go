package client

import (
	"context"
	"net/url"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type runnerGrantsClient struct{ RestAPIClient }

func (c *runnerGrantsClient) List(ctx context.Context, q manifest.SearchQuery) ([]manifest.ResourceManifest, manifest.Page, error) {
	return c.listResources(ctx, urlForPath(c.baseURL, "v1/runner-authorizations", searchToQuery(q)))
}
func (c *runnerGrantsClient) Get(ctx context.Context, name manifest.ResourceName) (manifest.ResourceManifest, bool, error) {
	var r manifest.ResourceManifest
	found, err := c.getResource(ctx, "v1/runner-authorizations/"+url.PathEscape(string(name)), &r)
	return r, found, err
}
func (c *runnerGrantsClient) Create(ctx context.Context, m manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return c.CreateFromManifest(ctx, m)
}
func (c *runnerGrantsClient) CreateOrUpdate(ctx context.Context, m manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	return c.ApplyObjectDefinition(ctx, m)
}
func (c *runnerGrantsClient) Update(ctx context.Context, id manifest.VersionedResourceID, m manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	m.Metadata.Version = id.Version
	r, _, err := c.ApplyObjectDefinition(ctx, m)
	return r, err
}
func (c *runnerGrantsClient) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	return c.deleteResource(ctx, "v1/runner-authorizations/"+url.PathEscape(string(id.ID)), id.Version)
}
