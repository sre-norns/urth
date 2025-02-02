package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/sre-norns/urth/pkg/urth"
)

type (
	Scenario struct {
		ScenarioId manifest.ResourceName `help:"Name of the scenario resource" arg:"" name:"name" `
	}

	Scenarios struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
		Output   string `help:"Output format" enum:"wide,short" default:"short" name:"output" short:"o"`
	}

	Script struct {
		ScenarioId manifest.ResourceID `help:"Name of the scenario" arg:"" name:"name" `
	}

	Results struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
		Output   string `help:"Output format" enum:"wide,short" default:"short" name:"output" short:"o"`

		ScenarioId manifest.ResourceName `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Runner struct {
		Id manifest.ResourceName `help:"Name of the Runner resource" arg:"" name:"name"`
	}

	Runners struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
		Output   string `help:"Output format" enum:"wide,short" default:"short" name:"output" short:"o"`
	}

	Artifact struct {
		Id       manifest.ResourceName `help:"Id of the artifact to get" arg:"" name:"artifact" `
		ShowMeta bool                  `help:"Show artifact meta information instead of content" name:"meta"`
	}

	Labels struct {
		Kind     string `help:"Kind of object that we want to query labels for"`
		Selector string `help:"Keys to match" optional:"" name:"selector" short:"l"`
	}

	GetCmd struct {
		Scenario  Scenario  `cmd:"" help:"Get scenario object from the server"`
		Scenarios Scenarios `cmd:"" help:"List all scenarios"`
		Script    Script    `cmd:"" help:"Get a script data for a given scenario"`
		Results   Results   `cmd:"" help:"Get a run result"`
		Artifact  Artifact  `cmd:"" help:"Get artifact produced during a scenario execution"`
		Runner    Runner    `cmd:"" help:"Get a runner object from the server"`
		Runners   Runners   `cmd:"" help:"List all runners"`
		Labels    Labels    `cmd:"" help:"Get labels"`
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

func resourceAge(meta manifest.ObjectMeta) time.Duration {
	if meta.UpdatedAt == nil {
		return 0
	}

	return time.Since(*meta.UpdatedAt).Round(time.Second)
}

func maybeDuration(start *time.Time, end *time.Time) string {
	if start == nil {
		return "not-started"
	}
	if end == nil {
		return fmt.Sprintf("pending: %v", time.Since(*start).Round(time.Second))
	}

	return end.Sub(*start).Round(time.Second).String()
}

func (c *Scenario) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resource, err := fetchScenario(ctx, apiClient, c.ScenarioId)
	if err != nil {
		return err
	}

	return cfg.OutputFormatter(&resource)
}

func (c *Scenarios) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	selector, err := manifest.ParseSelector(c.Selector)
	if err != nil {
		return fmt.Errorf("failed to parse labels selector: %w", err)
	}
	q := manifest.SearchQuery{
		Selector: selector,
	}
	// TODO: Pagination
	resources, _, err := fetchScenarios(cfg.Context, apiClient, q)
	if err != nil {
		return err
	}

	t := table.NewWriter()

	t.Style().Options = table.OptionsNoBordersAndSeparators
	t.Style().Format.HeaderAlign = text.AlignLeft
	t.Style().Format.RowAlign = text.AlignLeft

	t.SetOutputMirror(os.Stdout)
	header := table.Row{"Name", "Enabled", "Type", "Status", "Age"}
	if c.Output == "wide" {
		header = append(header, "Schedule", "Requirements", "NextRun", "LastRun.Duration")
	}
	t.AppendHeader(header)

	// r.Status.LastStatus

	for _, r := range resources {
		lastStatus := "unknown"
		if len(r.Status.Results) > 0 {
			lastStatus = fmt.Sprintf("%v/%v", r.Status.Results[0].Status.Status, r.Status.Results[0].Status.Result)
		}

		probType := "<unknown>"
		if r.Spec.Prob.Kind != "" {
			probType = string(r.Spec.Prob.Kind)
		}

		row := table.Row{r.Name, r.Spec.IsActive, probType, lastStatus, resourceAge(r.ObjectMeta)}

		if c.Output == "wide" {
			lastRunDuration := ""

			if len(r.Status.Results) > 0 && r.Status.Results[0].Spec.TimeStarted != nil {
				latestResult := r.Status.Results[0].Spec
				lastRunDuration = maybeDuration(latestResult.TimeStarted, latestResult.TimeEnded)
			}

			nextRunScheduled := ""
			if r.Status.NextRun != nil {
				nextRunScheduled = r.Status.NextRun.String()
			}

			row = append(row,
				r.Spec.RunSchedule,
				// r.Spec.Description,
				r.Spec.Requirements,
				nextRunScheduled,
				lastRunDuration,
			)
		}

		t.AppendRow(row)
	}

	t.Render()
	return nil
}

func (c *Runners) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	selector, err := manifest.ParseSelector(c.Selector)
	if err != nil {
		return fmt.Errorf("failed to parse labels selector: %w", err)
	}
	q := manifest.SearchQuery{
		Selector: selector,
	}
	// TODO: Pagination
	resources, _, err := fetchRunners(cfg.Context, apiClient, q)
	if err != nil {
		return err
	}

	t := table.NewWriter()
	t.Style().Options = table.OptionsNoBordersAndSeparators
	t.Style().Format.HeaderAlign = text.AlignLeft
	t.Style().Format.RowAlign = text.AlignLeft

	t.SetOutputMirror(os.Stdout)
	header := table.Row{"Name", "Enabled", "Online", "Age"}
	if c.Output == "wide" {
		header = append(header, "Requirements", "Description")
	}
	t.AppendHeader(header)

	for _, r := range resources {
		nActive := strconv.FormatUint(r.Status.NumberInstances, 10)
		if r.Spec.MaxInstances > 0 {
			nActive = fmt.Sprintf("%d/%d", r.Status.NumberInstances, r.Spec.MaxInstances)
		}

		row := table.Row{r.Name, r.Spec.IsActive, nActive, resourceAge(r.ObjectMeta)}

		if c.Output == "wide" {
			row = append(row,
				r.Spec.Requirements,
				r.Spec.Description,
			)
		}

		t.AppendRow(row)
	}

	t.Render()
	return nil
}

func (c *Runner) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resource, err := fetchRunner(ctx, apiClient, c.Id)
	if err != nil {
		return err
	}

	return cfg.OutputFormatter(&resource)
}

func (c *Results) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	selector, err := manifest.ParseSelector(c.Selector)
	if err != nil {
		return fmt.Errorf("failed to parse labels selector: %w", err)
	}
	q := manifest.SearchQuery{
		Selector: selector,
	}
	// TODO: Pagination
	resources, _, err := fetchResults(ctx, apiClient, c.ScenarioId, q)
	if err != nil {
		return err
	}

	t := table.NewWriter()
	t.Style().Options = table.OptionsNoBordersAndSeparators
	t.Style().Format.HeaderAlign = text.AlignLeft
	t.Style().Format.RowAlign = text.AlignLeft
	t.SetOutputMirror(os.Stdout)

	header := table.Row{"Name", "Duration", "Status", "Age"}
	if c.Output == "wide" {
		header = append(header, "Results", "Kind", "Artifacts")
	}
	t.AppendHeader(header)

	for _, resource := range resources {
		row := table.Row{resource.Name, maybeDuration(resource.Spec.TimeStarted, resource.Spec.TimeEnded), resource.Status.Status, resourceAge(resource.ObjectMeta)}

		if c.Output == "wide" {
			row = append(row,
				resource.Status.Result,
				resource.Spec.ProbKind,
				strconv.FormatUint(resource.Status.NumberArtifacts, 10),
			)
		}

		t.AppendRow(row)
	}

	t.Render()
	return nil
}

func (c *Labels) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	var kind manifest.Kind
	switch c.Kind {
	case string(urth.KindArtifact):
		kind = urth.KindArtifact
	case string(urth.KindScenario):
		kind = urth.KindScenario
	case string(urth.KindResult):
		kind = urth.KindResult
	case string(urth.KindRunner):
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

	labels, _, err := apiClient.Labels(kind).ListLabels(ctx, manifest.SearchQuery{
		Selector: selector,
	})
	if err != nil {
		return err
	}

	for kv := range labels {
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

	resource, err := fetchArtifact(ctx, apiClient, c.Id)
	if err != nil {
		return err
	}

	if c.ShowMeta {
		return cfg.OutputFormatter(&resource)
	}

	// FIXME: Broken!
	_, err = os.Stdout.Write(resource.Spec.Content)

	return err
}
