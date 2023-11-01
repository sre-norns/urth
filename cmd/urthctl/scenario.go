package main

// func fetchScenario(cmdCtx context.Context, id urth.ResourceID, apiServerAddress string) (urth.Scenario, error) {
// 	apiClient, err := urth.NewRestApiClient(apiServerAddress)
// 	if err != nil {
// 		return urth.Scenario{}, fmt.Errorf("failed to initialize API Client: %w", err)
// 	}

// 	ctx, cancel := context.WithTimeout(cmdCtx, 30*time.Second)
// 	defer cancel()

// 	resource, ok, err := apiClient.GetScenarioAPI().Get(ctx, id)
// 	if !ok && err == nil {
// 		err = fmt.Errorf("requested scenario not found")
// 	}

// 	return resource, err
// }

// func fetchResults(cmdCtx context.Context, scenarioId, id urth.ResourceID, apiServerAddress string) (urth.ScenarioRunResults, error) {
// 	apiClient, err := urth.NewRestApiClient(apiServerAddress)
// 	if err != nil {
// 		return urth.ScenarioRunResults{}, fmt.Errorf("failed to initialize API Client: %w", err)
// 	}

// 	ctx, cancel := context.WithTimeout(cmdCtx, 30*time.Second)
// 	defer cancel()

// 	resource, ok, err := apiClient.GetResultsAPI(scenarioId).Get(ctx, id)
// 	if !ok && err == nil {
// 		err = fmt.Errorf("requested scenario results not found")
// 	}

// 	return resource, err
// }
