package client

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/google/uuid"
	identityclient "github.com/sre-norns/wyrd/identity/client"
	"github.com/sre-norns/wyrd/identity/model"
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
	// Resolve the product's name to the paired identity UID; the shared API
	// owns credential creation, transactional replay and operation-only secrets.
	runner, found, err := c.Get(ctx, runnerName)
	if err != nil || !found {
		return "", found, err
	}
	api, err := identityclient.New(c.baseURL.String(), identityclient.Config{HTTPClient: c.config.HTTPClient, Token: string(c.config.Token), Timeout: c.config.Timeout})
	if err != nil {
		return "", true, err
	}
	key := requestOptions(ctx).IdempotencyKey
	if key == "" {
		key = uuid.NewString()
	}
	ctx = identityclient.WithRequestOptions(ctx, identityclient.RequestOptions{IdempotencyKey: key})
	// A retry must retain both its key and body, including the generated name.
	sum := sha256.Sum256([]byte(string(runner.Metadata.UID) + "\x00" + key))
	token, err := api.AgentIdentityTokens().Create(ctx, model.AgentIdentityID(runner.Metadata.UID), model.AgentIdentityToken{Resource: model.Resource{Name: fmt.Sprintf("enrolment-%x", sum[:16])}})
	if err != nil {
		return "", true, err
	}
	if token.Token == "" {
		return "", true, fmt.Errorf("token already created; its one-time secret is no longer available")
	}
	return urth.APIToken(token.Token), true, nil
}

func (c *runnersAPIClient) AuthWorker(ctx context.Context, token urth.APIToken, newEntry manifest.ResourceManifest) (urth.WorkerRegistrationResponse, error) {
	return send[urth.WorkerRegistrationResponse](ctx, &c.RestAPIClient, http.MethodPost, urlForPath(c.baseURL, "v1/auth/workers", nil), string(token), "", newEntry)
}
