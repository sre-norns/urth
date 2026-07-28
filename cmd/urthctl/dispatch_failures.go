package main

import (
	"fmt"
	"os"
	"time"

	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/sre-norns/urth/pkg/urth"
)

// The dead-letter surface for the CLI.
//
// It exists here because CONTEXT.md makes the Web UI and urthctl co-equal:
// diagnosing why a run never started is work an operator does at whichever
// surface they are already in, and a capability that lives in only one of them
// sends them to psql instead.

type (
	// DeadLetters lists dispatches that stopped making progress.
	DeadLetters struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
		Output   string `help:"Output format" enum:"wide,short" default:"short" name:"output" short:"o"`

		// Unresolved is the default view for a reason: the question being asked
		// is almost always "what is still broken", and a list that fills up with
		// failures somebody already dealt with stops being read.
		All bool `help:"Include failures that have already been resolved" name:"all"`
	}

	// DeadLetter shows one dispatch failure in full.
	DeadLetter struct {
		ID manifest.ResourceName `help:"Name of the dispatch failure" arg:"" name:"name"`
	}

	// RetryCmd asks for a new run for a stranded dispatch.
	RetryCmd struct {
		ID manifest.ResourceName `help:"Name of the dispatch failure to retry" arg:"" name:"name"`

		KeepOpen bool `help:"Leave the failure unresolved after retrying" name:"keep-open"`
	}

	// ResolveCmd closes a failure without retrying it.
	ResolveCmd struct {
		ID manifest.ResourceName `help:"Name of the dispatch failure to resolve" arg:"" name:"name"`
	}
)

// unresolvedSelector limits a listing to failures nobody has dealt with.
const unresolvedSelector = urth.LabelDispatchFailureResolved + "=false"

func (c *DeadLetters) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	query := c.Selector
	if !c.All {
		if query == "" {
			query = unresolvedSelector
		} else {
			query = fmt.Sprintf("%s,%s", query, unresolvedSelector)
		}
	}

	selector, err := manifest.ParseSelector(query)
	if err != nil {
		return fmt.Errorf("failed to parse labels selector: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resources, _, err := apiClient.DispatchFailures().List(ctx, manifest.SearchQuery{Selector: selector})
	if err != nil {
		return err
	}

	t := table.NewWriter()
	t.Style().Options = table.OptionsNoBordersAndSeparators
	t.Style().Format.HeaderAlign = text.AlignLeft
	t.Style().Format.RowAlign = text.AlignLeft

	t.SetOutputMirror(os.Stdout)
	header := table.Row{"Name", "Reason", "Scenario", "Runner", "Resolved", "Age"}
	if c.Output == "wide" {
		header = append(header, "Reporter", "Deliveries", "Retry", "Detail")
	}
	t.AppendHeader(header)

	for _, failure := range resources {
		row := table.Row{
			failure.Name,
			failure.Spec.Reason,
			orDash(string(failure.Spec.ScenarioName)),
			orDash(failure.Labels[urth.LabelRunnerName]),
			resolvedLabel(failure),
			failureAge(failure),
		}

		if c.Output == "wide" {
			row = append(row,
				failure.Spec.ReportedBy,
				failure.Spec.Deliveries,
				orDash(string(failure.Status.RetryResultName)),
				failure.Spec.Detail,
			)
		}

		t.AppendRow(row)
	}

	t.Render()

	if len(resources) == 0 && !c.All {
		// Said explicitly, because an empty table and "there are none
		// outstanding" look identical, and only one of them is good news.
		fmt.Fprintln(os.Stderr, "No unresolved dispatch failures. Use --all to include resolved ones.")
	}

	return nil
}

func (c *DeadLetter) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	failure, exists, err := apiClient.DispatchFailures().Get(ctx, c.ID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("dispatch failure %q not found", c.ID)
	}

	return cfg.OutputFormatter(failure.ToManifest())
}

func (c *RetryCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	resolve := !c.KeepOpen
	failure, retry, err := apiClient.DispatchFailures().Retry(ctx, c.ID,
		urth.RetryDispatchFailureRequest{Resolve: &resolve})
	if err != nil {
		return err
	}

	// Named rather than merely confirmed: the retry is a *new* run, and an
	// operator who is not told its name has no way to follow what they just
	// started -- the failed run's name will not find it.
	fmt.Printf("Retried %q as run %q (of scenario %q).\n",
		failure.Name, retry.Name, failure.Spec.ScenarioName)

	if !failure.Status.Resolved {
		fmt.Println("The failure was left open.")
	}

	return nil
}

func (c *ResolveCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	failure, err := apiClient.DispatchFailures().Resolve(ctx, c.ID)
	if err != nil {
		return err
	}

	fmt.Printf("Resolved %q.\n", failure.Name)

	return nil
}

// resolvedLabel renders what has been done about a failure.
//
// "retried" rather than a bare "yes", because those are different states to an
// operator scanning the list: one was re-run, the other was judged not to need
// it, and the difference is the first thing they will want.
func resolvedLabel(failure urth.DispatchFailure) string {
	switch {
	case failure.Status.RetryResultUID != "":
		return "retried"
	case failure.Status.Resolved:
		return "resolved"
	default:
		return "no"
	}
}

// failureAge reports how long ago the dispatch actually failed.
//
// From OccurredAt rather than the record's timestamps: a worker that could not
// reach the API reports late, and the age an operator cares about is the age of
// the problem.
func failureAge(failure urth.DispatchFailure) time.Duration {
	if failure.Spec.OccurredAt.IsZero() {
		return resourceAge(failure.ObjectMeta)
	}

	return time.Since(failure.Spec.OccurredAt).Round(time.Second)
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}

	return value
}
