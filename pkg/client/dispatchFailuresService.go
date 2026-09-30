package client

import (
	"context"
	"fmt"
	"net/http"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// dispatchFailuresAPIClient is the REST client for the dead-letter surface.
type dispatchFailuresAPIClient struct {
	RestAPIClient
}

// List all dead-lettered dispatches matching the query.
//
// The server lists manifests, like every other resource, so these are converted
// rather than decoded straight into the model -- which would silently produce
// empty names and labels, since a manifest keeps both under `metadata`.
func (c *dispatchFailuresAPIClient) List(ctx context.Context, searchQuery manifest.SearchQuery) ([]urth.DispatchFailure, manifest.Page, error) {
	targetAPI := urlForPath(c.baseURL, "v1/dispatch-failures", searchToQuery(searchQuery))

	resources, total, err := c.listResources(ctx, targetAPI)
	if err != nil {
		return nil, total, err
	}

	failures := make([]urth.DispatchFailure, 0, len(resources))
	for _, resource := range resources {
		failure, err := urth.NewDispatchFailure(resource)
		if err != nil {
			return nil, total, err
		}

		failures = append(failures, failure)
	}

	return failures, total, nil
}

// Get a single dead-lettered dispatch by name.
func (c *dispatchFailuresAPIClient) Get(ctx context.Context, id manifest.ResourceName) (result urth.DispatchFailure, exists bool, err error) {
	var resource manifest.ResourceManifest
	exists, err = c.getResource(ctx, fmt.Sprintf("v1/dispatch-failures/%v", id), &resource)
	if !exists || err != nil {
		return
	}

	result, err = urth.NewDispatchFailure(resource)

	return
}

// Report a dispatch this worker cannot make progress on.
//
// Authenticated with the worker's session, like a claim: the server takes the
// reporter's identity from the credential and ignores anything the body says
// about who is reporting.
func (c *dispatchFailuresAPIClient) Report(ctx context.Context, session urth.APIToken, request urth.ReportDispatchFailureRequest) (urth.DispatchFailure, error) {
	targetAPI := urlForPath(c.baseURL, "v1/dispatch-failures", nil)
	resource, err := send[manifest.ResourceManifest](ctx, &c.RestAPIClient, http.MethodPost, targetAPI, string(session), "", request)
	if err != nil {
		return urth.DispatchFailure{}, err
	}
	return urth.NewDispatchFailure(resource)
}

// Retry asks for a new run for a stranded dispatch.
func (c *dispatchFailuresAPIClient) Retry(ctx context.Context, id manifest.ResourceName, request urth.RetryDispatchFailureRequest) (urth.DispatchFailure, urth.Result, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/dispatch-failures/%v/retry", id), nil)
	response, err := send[urth.RetryDispatchFailureResponse](ctx, &c.RestAPIClient, http.MethodPost, targetAPI, "", "", request)
	if err != nil {
		return urth.DispatchFailure{}, urth.Result{}, err
	}

	failure, err := urth.NewDispatchFailure(response.Failure)
	if err != nil {
		return urth.DispatchFailure{}, urth.Result{}, err
	}

	retry, err := urth.NewResult(response.Retry)
	if err != nil {
		return failure, urth.Result{}, err
	}

	return failure, retry, nil
}

// Resolve closes a failure without retrying it.
func (c *dispatchFailuresAPIClient) Resolve(ctx context.Context, id manifest.ResourceName) (urth.DispatchFailure, error) {
	targetAPI := urlForPath(c.baseURL, fmt.Sprintf("v1/dispatch-failures/%v/resolve", id), nil)
	resource, err := send[manifest.ResourceManifest](ctx, &c.RestAPIClient, http.MethodPost, targetAPI, "", "", nil)
	if err != nil {
		return urth.DispatchFailure{}, err
	}
	return urth.NewDispatchFailure(resource)
}
