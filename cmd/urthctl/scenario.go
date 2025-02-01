package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

var ErrResourceNotFound = fmt.Errorf("requested resource not found")

func fetchRunner(ctx context.Context, apiClient *urth.RestApiClient, id manifest.ResourceName) (urth.Runner, error) {
	resource, ok, err := apiClient.GetRunnerAPI().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: runnerId=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchRunners(ctx context.Context, apiClient *urth.RestApiClient, q manifest.SearchQuery) ([]urth.Runner, error) {
	// TODO: Pagination
	resources, _, err := apiClient.GetRunnerAPI().List(ctx, q)
	if err != nil {
		return nil, err
	}

	return resources, nil
}

func fetchScenario(ctx context.Context, apiClient *urth.RestApiClient, id manifest.ResourceName) (urth.Scenario, error) {
	resource, ok, err := apiClient.GetScenarioAPI().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: scenarioId=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchScenarios(ctx context.Context, apiClient *urth.RestApiClient, q manifest.SearchQuery) ([]urth.Scenario, error) {
	// TODO: Pagination
	resources, _, err := apiClient.GetScenarioAPI().List(ctx, q)
	if err != nil {
		return nil, err
	}

	return resources, nil
}

func fetchResults(ctx context.Context, apiClient *urth.RestApiClient, scenarioId manifest.ResourceName, ids []manifest.ResourceName) ([]urth.Result, error) {
	if len(ids) == 0 {
		resources, _, err := apiClient.GetResultsAPI(scenarioId).List(ctx, manifest.SearchQuery{})
		if err != nil {
			return nil, err
		}

		for _, resource := range resources {
			ids = append(ids, resource.Name)
		}
	}

	results := make([]urth.Result, 0, len(ids))
	for _, rid := range ids {
		resource, ok, err := apiClient.GetResultsAPI(scenarioId).Get(ctx, rid)
		if !ok && err == nil {
			return nil, fmt.Errorf("%w: scenarioId=%v, runId=%v", ErrResourceNotFound, scenarioId, ids)
		}

		results = append(results, resource)
	}

	return results, nil
}

func fetchArtifact(ctx context.Context, apiClient *urth.RestApiClient, id manifest.ResourceName) (urth.Artifact, error) {
	resource, ok, err := apiClient.GetArtifactsApi().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: id=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchLogs(ctx context.Context, apiClient *urth.RestApiClient, resultsName manifest.ResourceName, customSelector string) (chan io.Reader, error) {
	labels := []string{}
	if resultsName != "" {
		labels = append(labels, fmt.Sprintf("%v=%v", urth.LabelRunResultsName, resultsName))
	}

	if !strings.Contains(customSelector, urth.LabelScenarioArtifactKind) {
		labels = append(labels, fmt.Sprintf("%v=%v", urth.LabelScenarioArtifactKind, "log"))
	}

	if customSelector != "" {
		labels = append(labels, customSelector)
	}

	selector, err := manifest.ParseSelector(strings.Join(labels, ","))
	if err != nil {
		return nil, fmt.Errorf("failed to parse labels selector: %w", err)
	}

	resources, _, err := apiClient.GetArtifactsApi().List(ctx, manifest.SearchQuery{
		Selector: selector,
	})
	if err != nil {
		return nil, err
	}

	logStream := make(chan io.Reader)

	go func() {
		defer close(logStream)

		for _, r := range resources {
			l, ok, err := apiClient.GetArtifactsApi().GetContent(ctx, r.Name)
			if !ok || err != nil {
				return
			}

			logStream <- bytes.NewReader(l.Content)
		}
	}()

	return logStream, err
}
