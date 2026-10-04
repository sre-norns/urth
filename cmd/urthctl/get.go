package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity/cli"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type (
	Scenario struct {
		ScenarioID manifest.ResourceName `help:"Name of the scenario resource" arg:"" name:"name" `
	}

	Scenarios struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
	}

	Script struct {
		ScenarioID manifest.ResourceID `help:"Name of the scenario" arg:"" name:"name" `
	}

	Results struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`

		ScenarioID manifest.ResourceName `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Runner struct {
		ID manifest.ResourceName `help:"Name of the Runner resource" arg:"" name:"name"`
	}

	Runners struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
	}

	Artifact struct {
		ID       manifest.ResourceName `help:"Id of the artifact to get" arg:"" name:"artifact" `
		ShowMeta bool                  `help:"Show artifact meta information instead of content" name:"meta"`
	}

	Artifacts struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
	}

	Labels struct {
		Kind     manifest.Kind `help:"Kind of object that we want to query labels for" arg:""`
		Selector string        `help:"Keys to match" optional:"" name:"selector" short:"l"`
	}

	Worker struct {
		Name manifest.ResourceName `arg:"" help:"Worker name"`
	}
	Workers struct {
		Selector string `help:"Label selector" short:"l"`
	}

	GetCmd struct {
		Worker    Worker    `cmd:"" help:"Inspect a worker and stored capabilities"`
		Workers   Workers   `cmd:"" help:"List workers"`
		Scenario  Scenario  `cmd:"" help:"Get scenario object from the server"`
		Scenarios Scenarios `cmd:"" help:"List all scenarios"`
		Script    Script    `cmd:"" help:"Get a script data for a given scenario"`
		Results   Results   `cmd:"" help:"Get a run result"`
		Artifact  Artifact  `cmd:"" help:"Get artifact produced during a scenario execution"`
		Artifacts Artifacts `cmd:"" help:"List artifacts; select them by label, e.g. -l urth/artifact.may-contain-secrets=true"`
		Runner    Runner    `cmd:"" help:"Get a runner object from the server"`
		Runners   Runners   `cmd:"" help:"List all runners"`
		Labels    Labels    `cmd:"" help:"Get labels"`

		DeadLetter  DeadLetter  `cmd:"" name:"dead-letter" help:"Get one dispatch failure in full"`
		DeadLetters DeadLetters `cmd:"" name:"dead-letters" help:"List dispatches that stopped making progress"`
	}
)

func (c *Scenario) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resource, err := fetchScenario(ctx, apiClient, c.ScenarioID)
	return cli.RenderResource(cfg.Env.Output, scenarioView{resource}, err)
}

func (c *Scenarios) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	q, err := cli.SearchQuery(c.Selector)
	if err != nil {
		return err
	}
	resources, page, err := fetchScenarios(cfg.Context, apiClient, q)
	if err != nil {
		return err
	}

	return cli.RenderList(cfg.Env.Output, views(resources, func(r urth.Scenario) scenarioView { return scenarioView{r} }), page)
}

func (c *Runners) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	q, err := cli.SearchQuery(c.Selector)
	if err != nil {
		return err
	}
	resources, page, err := fetchRunners(cfg.Context, apiClient, q)
	if err != nil {
		return err
	}

	return cli.RenderList(cfg.Env.Output, views(resources, func(r urth.Runner) runnerView { return runnerView{r} }), page)
}

func (c *Runner) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resource, err := fetchRunner(ctx, apiClient, c.ID)
	return cli.RenderResource(cfg.Env.Output, runnerView{resource}, err)
}

func (c *Results) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	q, err := cli.SearchQuery(c.Selector)
	if err != nil {
		return err
	}
	resources, page, err := fetchResults(ctx, apiClient, c.ScenarioID, q)
	if err != nil {
		return err
	}

	return cli.RenderList(cfg.Env.Output, views(resources, func(r urth.Result) resultView { return resultView{r} }), page)
}

func (c *Labels) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	var kind manifest.Kind
	switch c.Kind {
	case urth.KindArtifact:
		kind = urth.KindArtifact
	case urth.KindScenario:
		kind = urth.KindScenario
	case urth.KindResult:
		kind = urth.KindResult
	case urth.KindRunner:
		kind = urth.KindRunner
	default:
		return fmt.Errorf("unknown Kind: %v", c.Kind)
	}

	selector, err := manifest.ParseSelector(c.Selector)
	if err != nil {
		return fmt.Errorf("failed to parse labels selector: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	labels, _, err := collectPages(ctx, manifest.SearchQuery{Selector: selector}, func(ctx context.Context, q manifest.SearchQuery) ([]string, manifest.Page, error) {
		values, page, err := apiClient.Labels(kind).ListLabels(ctx, q)
		return values.Slice(), page, err
	})
	if err != nil {
		return err
	}

	for _, kv := range labels {
		fmt.Println(kv)
		// fmt.Printf("%v=%v\n", kv.Key, kv.Value)
	}

	return nil
}

func (c *Artifact) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resource, err := fetchArtifact(ctx, apiClient, c.ID)
	if err != nil {
		return err
	}

	if c.ShowMeta {
		return cli.RenderResource(cfg.Env.Output, artifactView{resource}, nil)
	}

	// FIXME: Broken!
	_, err = os.Stdout.Write(resource.Spec.Artifact.Content)

	return err
}

func (c *Artifacts) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	q, err := cli.SearchQuery(c.Selector)
	if err != nil {
		return err
	}
	resources, page, err := collectPages(cfg.Context, q, apiClient.Artifacts().List)
	if err != nil {
		return err
	}
	artifacts := make([]artifactView, 0, len(resources))
	for _, resource := range resources {
		artifact, err := urth.NewArtifact(resource)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifactView{artifact})
	}

	return cli.RenderList(cfg.Env.Output, artifacts, page)
}

func (c *Worker) Run(cfg *commandContext) error {
	api, err := cfg.NewClient()
	if err != nil {
		return err
	}
	m, found, err := api.Workers().Get(cfg.Context, c.Name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("worker %q not found", c.Name)
	}
	w, err := urth.NewWorkerInstance(m)
	return cli.RenderResource(cfg.Env.Output, workerView{w}, err)
}
func (c *Workers) Run(cfg *commandContext) error {
	api, err := cfg.NewClient()
	if err != nil {
		return err
	}
	q, err := cli.SearchQuery(c.Selector)
	if err != nil {
		return err
	}
	resources, page, err := collectPages(cfg.Context, q, api.Workers().List)
	if err != nil {
		return err
	}
	workers := make([]workerView, 0, len(resources))
	for _, m := range resources {
		w, err := urth.NewWorkerInstance(m)
		if err != nil {
			return err
		}
		workers = append(workers, workerView{w})
	}
	return cli.RenderList(cfg.Env.Output, workers, page)
}
