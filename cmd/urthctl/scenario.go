package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sre-norns/urth/pkg/urth"
)

var ErrResourceNotFound = fmt.Errorf("requested resource not found")

func fetchRunner(ctx context.Context, id urth.ResourceID, apiServerAddress string) (urth.Runner, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return urth.Runner{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetRunnerAPI().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: runnerId=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchScenario(ctx context.Context, id urth.ResourceID, apiServerAddress string) (urth.Scenario, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return urth.Scenario{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetScenarioAPI().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: scenarioId=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchResults(ctx context.Context, scenarioId, id urth.ResourceID, apiServerAddress string) (urth.Result, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return urth.Result{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetResultsAPI(scenarioId).Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: scenarioId=%v, runId=%v", ErrResourceNotFound, scenarioId, id)
	}

	return resource, err
}

func fetchArtifact(ctx context.Context, id urth.ResourceID, apiServerAddress string) (urth.Artifact, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return urth.Artifact{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetArtifactsApi().Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: id=%v", ErrResourceNotFound, id)
	}

	return resource, err
}

func fetchLogs(ctx context.Context, apiServerAddress string, id urth.ResourceID, customSelector string) (chan io.Reader, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	labels := []string{}
	if id != urth.InvalidResourceID {
		labels = append(labels, fmt.Sprintf("%v=%v", urth.LabelScenarioRunId, id))
	}

	if !strings.Contains(customSelector, urth.LabelScenarioArtifactKind) {
		labels = append(labels, fmt.Sprintf("%v=%v", urth.LabelScenarioArtifactKind, "log"))
	}

	if customSelector != "" {
		labels = append(labels, customSelector)
	}

	resources, err := apiClient.GetArtifactsApi().List(ctx, urth.SearchQuery{
		Labels: strings.Join(labels, ","),
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
