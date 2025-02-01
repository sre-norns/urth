package main

import (
	"fmt"
	"log"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

type createCmd struct {
	Filenames []string `help:"Manifest files(s) or directory to create resources on the server" arg:"" name:"file"`
}

func (c *createCmd) Run(cfg *commandContext) error {
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

		var resourceSpec manifest.ResourceManifest
		if err := yaml.Unmarshal(content, &resourceSpec); err != nil {
			return fmt.Errorf("failed parse manifest from %q: %w", filename, err)
		}

		// TODO: Use timeout!
		c, err := apiClient.CreateFromManifest(cfg.Context, resourceSpec)
		if err != nil {
			return fmt.Errorf("failed to create resource from %q: %w", filename, err)
		}

		log.Print("created ", c.Kind, ", name: ", c.Metadata.Name, ", UID: ", c.Metadata.UID)
	}

	return nil
}
