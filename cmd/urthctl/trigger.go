package main

import (
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity/cli"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// TriggerCmd starts a run of a scenario on the server now -- the Web UI's
// "Run now". `run` is different: it runs a scenario here, locally.
type TriggerCmd struct {
	Scenario manifest.ResourceName `arg:"" help:"Name of the scenario"`
}

func (c *TriggerCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	run, err := apiClient.Results(c.Scenario).Create(ctx, manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindResult},
	})
	if err != nil {
		return err
	}
	// A run that cannot be placed is created already errored, not refused:
	// say so here, or the next `get results` is the first anyone hears of it.
	if reason := run.Labels[urth.LabelResultUnschedulable]; reason != "" {
		return fmt.Errorf("run %q of %q cannot be placed: %s", run.Name, c.Scenario, reason)
	}
	return cli.RenderResource(cfg.Env.Output, resultView{run}, nil)
}
