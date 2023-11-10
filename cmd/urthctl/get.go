package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
	"gopkg.in/yaml.v3"
)

type (
	Scenario struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Script struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Results struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
		RunId      urth.ResourceID `help:"Id of the run results" arg:"" name:"result" `
	}

	Runner struct {
		Id urth.ResourceID `help:"Id of the runner" arg:"" name:"scenario" `
	}

	Labels struct {
		Selector string `help:"Keys to match" optional:"" name:"selector" short:"l"`
	}

	GetCmd struct {
		Scenario Scenario `cmd:"" help:"Get scenario object from the server"`
		Script   Script   `cmd:"" help:"Get a script data for a given scenario"`
		Results  Results  `cmd:"" help:"Get a run result"`
		Runner   Runner   `cmd:"" help:"Get a runner object from the server"`
		Labels   Labels   `cmd:"" help:"Get labels"`

		// TODO: Add explicit timeout
	}
)

type formatter func(any) error

func yamlFormatter(resource any) error {
	data, err := yaml.Marshal(resource)
	if err != nil {
		return err
	}
	fmt.Print(string(data))

	return nil
}

func jsonFormatter(resource any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "\t")

	err := encoder.Encode(resource)
	if err != nil {
		return err
	}

	return nil
}

func getFormatter(formatName string) (formatter, error) {
	switch formatName {
	case "yaml", "yml":
		return yamlFormatter, nil
	case "json":
		return jsonFormatter, nil
	}

	return nil, fmt.Errorf("unexpected output format %q", formatName)
}

func (c *Scenario) Run(cfg *commandContext) error {
	format, err := getFormatter(cfg.Format)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchScenario(ctx, c.ScenarioId, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	return format(&resource)
}

func (c *Runner) Run(cfg *commandContext) error {
	format, err := getFormatter(cfg.Format)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchRunner(ctx, c.Id, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	return format(&resource)
}

func (c *Results) Run(cfg *commandContext) error {
	format, err := getFormatter(cfg.Format)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchResults(ctx, c.ScenarioId, c.RunId, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	return format(&resource)
}

func (c *Labels) Run(cfg *commandContext) error {
	apiClient, err := urth.NewRestApiClient(cfg.ApiServerAddress)
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	var query urth.SearchQuery
	query.Labels = c.Selector

	labels, err := apiClient.GetLabels().List(cfg.Context, query)
	if err != nil {
		return err
	}

	for _, kv := range labels {
		fmt.Printf("%v=%v\n", kv.Key, kv.Value)
	}

	return nil
}
