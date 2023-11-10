package main

import (
	"context"
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
)

var ErrResourceNotFound = fmt.Errorf("requested resource not found")

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

func fetchResults(ctx context.Context, scenarioId, id urth.ResourceID, apiServerAddress string) (urth.ScenarioRunResults, error) {
	apiClient, err := urth.NewRestApiClient(apiServerAddress)
	if err != nil {
		return urth.ScenarioRunResults{}, fmt.Errorf("failed to initialize API Client: %w", err)
	}

	resource, ok, err := apiClient.GetResultsAPI(scenarioId).Get(ctx, id)
	if !ok && err == nil {
		err = fmt.Errorf("%w: scenarioId=%v, runId=%v", ErrResourceNotFound, scenarioId, id)
	}

	return resource, err
}
