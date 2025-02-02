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
	resource, ok, err := apiClient.Runners().Get(ctx, id)
	if err != nil {
		return urth.Runner{}, fmt.Errorf("failed to fetch Runner %q: %w", id, err)
	} else if !ok {
		return urth.Runner{}, fmt.Errorf("%w: runnerId=%v", ErrResourceNotFound, id)
	}

	result, err := urth.NewRunner(resource)
	return result, err
}

func fetchRunners(ctx context.Context, apiClient *urth.RestApiClient, q manifest.SearchQuery) ([]urth.Runner, int64, error) {
	resources, total, err := apiClient.Runners().List(ctx, q)
	if err != nil {
		return nil, total, fmt.Errorf("failed to fetch batch: %w", err)
	}

	results := make([]urth.Runner, 0, len(resources))
	for _, resource := range resources {
		r, err := urth.NewRunner(resource)
		if err != nil {
			return results, total, fmt.Errorf("error while parsing batch results: %w", err)
		}
		results = append(results, r)
	}

	return results, total, nil
}

func fetchScenario(ctx context.Context, apiClient *urth.RestApiClient, id manifest.ResourceName) (urth.Scenario, error) {
	resource, ok, err := apiClient.Scenarios().Get(ctx, id)
	if err != nil {
		return urth.Scenario{}, fmt.Errorf("failed to fetch Scenario %q: %w", id, err)
	} else if !ok {
		return urth.Scenario{}, fmt.Errorf("%w: scenarioId=%v", ErrResourceNotFound, id)
	}

	result, err := urth.NewScenario(resource)
	return result, err
}

func fetchScenarios(ctx context.Context, apiClient *urth.RestApiClient, q manifest.SearchQuery) ([]urth.Scenario, int64, error) {
	resources, total, err := apiClient.Scenarios().List(ctx, q)
	if err != nil {
		return nil, total, fmt.Errorf("failed to fetch batch: %w", err)
	}

	results := make([]urth.Scenario, 0, len(resources))
	for _, resource := range resources {
		r, err := urth.NewScenario(resource)
		if err != nil {
			return results, total, fmt.Errorf("error while parsing batch results: %w", err)
		}
		results = append(results, r)
	}

	return results, total, nil
}

func fetchResults(ctx context.Context, apiClient *urth.RestApiClient, scenarioId manifest.ResourceName, q manifest.SearchQuery) ([]urth.Result, int64, error) {
	resources, total, err := apiClient.Results(scenarioId).List(ctx, q)
	if err != nil {
		return nil, total, fmt.Errorf("failed to fetch batch: %w", err)
	}

	return resources, total, err

	// results := make([]urth.Result, 0, len(resources))
	// for _, resource := range resources {
	// 	r, err := urth.NewResult(resource)
	// 	if err != nil {
	// 		return results, total, fmt.Errorf("error while parsing batch results: %w", err)
	// 	}
	// 	results = append(results, r)
	// }

	// return results, total, nil
}

func fetchArtifact(ctx context.Context, apiClient *urth.RestApiClient, id manifest.ResourceName) (urth.Artifact, error) {
	resource, ok, err := apiClient.Artifacts().Get(ctx, id)
	if err != nil {
		return urth.Artifact{}, fmt.Errorf("failed to fetch Artifact %q: %w", id, err)
	} else if !ok {
		return urth.Artifact{}, fmt.Errorf("%w: artifactId=%v", ErrResourceNotFound, id)
	}

	result, err := urth.NewArtifact(resource)
	return result, err
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

	resources, _, err := apiClient.Artifacts().List(ctx, manifest.SearchQuery{
		Selector: selector,
	})
	if err != nil {
		return nil, err
	}

	logStream := make(chan io.Reader)

	go func() {
		defer close(logStream)

		for _, resource := range resources {
			l, ok, err := apiClient.Artifacts().GetContent(ctx, resource.Metadata.Name)
			if !ok || err != nil {
				return
			}

			logStream <- bytes.NewReader(l.Content)
		}
	}()

	return logStream, err
}
