package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

type (
	Scenario struct {
		ScenarioId wyrd.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Script struct {
		ScenarioId wyrd.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Results struct {
		ScenarioId wyrd.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
		RunId      wyrd.ResourceID `help:"Id of the run results" arg:"" name:"result" `
	}

	Runner struct {
		Id wyrd.ResourceID `help:"Id of the runner" arg:"" name:"scenario" `
	}

	Artifact struct {
		Id       wyrd.ResourceID `help:"Id of the artifact to get" arg:"" name:"artifact" `
		ShowMeta bool            `help:"Show artifact meta information instead of content" name:"meta"`
	}

	Labels struct {
		Selector string `help:"Keys to match" optional:"" name:"selector" short:"l"`
	}

	GetCmd struct {
		Scenario Scenario `cmd:"" help:"Get scenario object from the server"`
		Script   Script   `cmd:"" help:"Get a script data for a given scenario"`
		Results  Results  `cmd:"" help:"Get a run result"`
		Artifact Artifact `cmd:"" help:"Get artifact produced during a scenario execution"`
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

func getFormatter(formatName outputFormat) (formatter, error) {
	switch formatName {
	case "yaml", "yml":
		return yamlFormatter, nil
	case "json":
		return jsonFormatter, nil
	}

	return nil, fmt.Errorf("unexpected output format %q", formatName)
}

func (c *Scenario) Run(cfg *commandContext) error {
	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchScenario(ctx, c.ScenarioId, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	return cfg.OutputFormatter(&resource)
}

func (c *Runner) Run(cfg *commandContext) error {
	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchRunner(ctx, c.Id, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	return cfg.OutputFormatter(&resource)
}

func (c *Results) Run(cfg *commandContext) error {
	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchResults(ctx, c.ScenarioId, c.RunId, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	return cfg.OutputFormatter(&resource)
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

func (c *Artifact) Run(cfg *commandContext) error {
	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	resource, err := fetchArtifact(ctx, c.Id, cfg.ApiServerAddress)
	if err != nil {
		return err
	}

	if c.ShowMeta {
		return cfg.OutputFormatter(&resource)
	}

	artifact, ok := resource.Spec.(*urth.ArtifactSpec)
	if !ok {
		return fmt.Errorf("unexpected type of Spec for the resource %q (kind=%q)", resource.Name, resource.Kind)
	}

	_, err = os.Stdout.Write(artifact.Content)

	return err
}
