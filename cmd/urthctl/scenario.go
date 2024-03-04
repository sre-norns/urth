package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sre-norns/urth/pkg/bark"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
)

var ErrResourceNotFound = fmt.Errorf("requested resource not found")

func fetchRunner(ctx context.Context, id wyrd.ResourceID, apiServerAddress string) (wyrd.ResourceManifest, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return wyrd.ResourceManifest{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetRunnerAPI().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: runnerId=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchScenario(ctx context.Context, id wyrd.ResourceID, apiServerAddress string) (wyrd.ResourceManifest, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return wyrd.ResourceManifest{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetScenarioAPI().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: scenarioId=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchResults(ctx context.Context, scenarioId wyrd.ResourceID, ids []wyrd.ResourceID, apiServerAddress string) ([]wyrd.ResourceManifest, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	if len(ids) == 0 {
		resources, err := apiClient.GetResultsAPI(scenarioId).List(ctx, bark.SearchQuery{})
		if err != nil {
			return nil, err
		}

		for _, resource := range resources {
			ids = append(ids, resource.GetID())
		}
	}

	results := make([]wyrd.ResourceManifest, 0, len(ids))
	for _, rid := range ids {
		resource, ok, err := apiClient.GetResultsAPI(scenarioId).Get(ctx, rid)
		if !ok && err == nil {
			return nil, fmt.Errorf("%w: scenarioId=%v, runId=%v", ErrResourceNotFound, scenarioId, ids)
		}

		results = append(results, resource)
	}

	return results, err
}

func fetchArtifact(ctx context.Context, id wyrd.ResourceID, apiServerAddress string) (wyrd.ResourceManifest, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return wyrd.ResourceManifest{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetArtifactsApi().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: id=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchLogs(ctx context.Context, apiServerAddress string, id wyrd.ResourceID, customSelector string) (chan io.Reader, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	labels := []string{}
	if id != wyrd.InvalidResourceID {
		labels = append(labels, fmt.Sprintf("%v=%v", urth.LabelScenarioRunId, id))
	}

	if !strings.Contains(customSelector, urth.LabelScenarioArtifactKind) {
		labels = append(labels, fmt.Sprintf("%v=%v", urth.LabelScenarioArtifactKind, "log"))
	}

	if customSelector != "" {
		labels = append(labels, customSelector)
	}

	resources, err := apiClient.GetArtifactsApi().List(ctx, bark.SearchQuery{
		Filter: strings.Join(labels, ","),
	})
	if err != nil {
		return nil, err
	}

	logStream := make(chan io.Reader)

	go func() {
		defer close(logStream)

		for _, r := range resources {
			l, ok, err := apiClient.GetArtifactsApi().GetContent(ctx, r.GetID())
			if !ok || err != nil {
				return
			}

			logStream <- bytes.NewReader(l.Content)
		}
	}()

	return logStream, err
}
