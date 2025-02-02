package main

import (
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type AuthWorkerCmd struct {
	RunnerId manifest.ResourceName `help:"Id of the Runner" name:"runner" arg:"" optional:"" group:"RunnerId" xor:"file"`
	File     string                `name:"file" help:"A resource manifest file with the runner" short:"f" type:"existingfile" group:"file" xor:"RunnerId"`
}

func (c *AuthWorkerCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	if c.RunnerId != "" {

	} else {
		manifest, ok, err := manifestFromFile(c.File)
		if err != nil {
			return fmt.Errorf("failed to read content: %w", err)
		} else if !ok {
			return fmt.Errorf("no manifest found in %q", c.File)
		}

		if manifest.Kind != urth.KindRunner {
			return fmt.Errorf("file %q defines %q kind, while %q manifest is required", c.File, manifest.Kind, urth.KindRunner)
		}

		c.RunnerId = manifest.Metadata.Name
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	token, exist, err := apiClient.Runners().GetToken(ctx, c.RunnerId)
	if err != nil {
		return err
	} else if !exist {
		fmt.Printf("Runner %q does not exists on the server\n", c.RunnerId)
		return nil
	}

	fmt.Printf("%s\n", token)
	return nil
}
