package main

import (
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

type ApplyCmd struct {
	Filenames []string `help:"Manifest files(s) to apply changes to on the server" arg:"" name:"file"`

	DryRun bool `help:"If true, don't apply changes but print what actions would have been taken"`
}

func (c *ApplyCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	for _, filename := range c.Filenames {
		// TODO: Check stats for isDir!
		content, fname, err := readContent(filename)
		if err != nil {
			return fmt.Errorf("failed read content from %q: %w", fname, err)
		}

		// FIXME: Should be just a universal resource manifest file
		var resourceSpec manifest.ResourceManifest
		if err := yaml.Unmarshal(content, &resourceSpec); err != nil {
			return fmt.Errorf("client: %w", err)
		}

		if !c.DryRun {
			_, _, err = apiClient.ApplyObjectDefinition(cfg.Context, resourceSpec)
			if err != nil {
				return fmt.Errorf("server: %w", err)
			}
		}
	}

	return nil
}
