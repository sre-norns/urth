package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ApplyObjectDefinition creates or updates the resource a manifest describes,
// at the collection its kind names.
func (c *RestAPIClient) ApplyObjectDefinition(ctx context.Context, spec manifest.ResourceManifest) (result manifest.ResourceManifest, created bool, err error) {
	targetAPI, err := apiURLForResource(c.baseURL, spec.TypeMeta, spec.Metadata.Name, nil)
	if err != nil {
		return result, created, err
	}

	data, err := json.Marshal(spec)
	if err != nil {
		return result, created, fmt.Errorf("RestApiClient manifest serialization error: %w", err)
	}

	return c.resourceAPICall(ctx, http.MethodPut, targetAPI, data, applyIfMatch(spec))
}

// applyIfMatch is the If-Match of applying a manifest. A manifest that carries
// its version -- one read back with `get` and edited -- updates only that
// version, so applying a stale copy is refused rather than undoing someone
// else's change. One without is applied unconditionally, as `apply` always was.
func applyIfMatch(spec manifest.ResourceManifest) string {
	if spec.Metadata.Version != 0 {
		return bark.ETag(spec.Metadata.Version)
	}
	return "*"
}

// CreateFromManifest creates the resource a manifest describes.
func (c *RestAPIClient) CreateFromManifest(ctx context.Context, manifest manifest.ResourceManifest) (result manifest.ResourceManifest, err error) {
	targetAPI, err := apiURLForResource(c.baseURL, manifest.TypeMeta, "", nil)
	if err != nil {
		return result, err
	}

	data, err := json.Marshal(manifest)
	if err != nil {
		return result, fmt.Errorf("RestApiClient manifest serialization error: %w", err)
	}

	result, _, err = c.resourceAPICall(ctx, http.MethodPost, targetAPI, data, "")

	return
}
