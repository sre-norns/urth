package main

import (
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// RunnersCmd groups runner operations.
type RunnersCmd struct {
	Token RunnerTokenCmd `cmd:"" help:"Issue an enrolment token a runner's workers register with"`
}

// RunnerTokenCmd issues the token a runner's workers start with (RUNNER_TOKEN).
// It replaces `auth-worker`: issuing one is an account administrator's act, so
// it takes a signed-in profile like any other command.
type RunnerTokenCmd struct {
	Name manifest.ResourceName `help:"Name of the runner" arg:"" optional:"" xor:"source"`
	File string                `help:"A runner manifest; its name is used" short:"f" type:"existingfile" xor:"source"`
}

func (c *RunnerTokenCmd) Run(cfg *commandContext) error {
	if c.Name == "" && c.File == "" {
		return fmt.Errorf("name a runner, or a runner manifest with -f")
	}
	if c.File != "" {
		resource, ok, err := manifestFromFile(c.File)
		if err != nil {
			return fmt.Errorf("failed to read content: %w", err)
		} else if !ok {
			return fmt.Errorf("no manifest found in %q", c.File)
		}
		if resource.Kind != urth.KindRunner {
			return fmt.Errorf("file %q defines %q kind, while %q manifest is required", c.File, resource.Kind, urth.KindRunner)
		}
		c.Name = resource.Metadata.Name
	}

	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	token, exist, err := apiClient.Runners().GetToken(ctx, c.Name)
	if err != nil {
		return err
	} else if !exist {
		return fmt.Errorf("runner %q does not exist", c.Name)
	}

	fmt.Println(token)
	return nil
}
