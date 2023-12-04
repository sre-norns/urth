package main

import (
	"fmt"
	"log"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

type createCmd struct {
	Filenames []string `help:"Manifest files(s) or directory to create resources on the server" arg:"" name:"file"`
}

func (c *createCmd) Run(cfg *commandContext) error {
	apiClient, err := urth.NewRestApiClient(cfg.ApiServerAddress)
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	for _, filename := range c.Filenames {
		// TODO: Check stats for isDir!
		content, _, err := readContent(filename)
		if err != nil {
			return fmt.Errorf("failed read content from %q: %w", filename, err)
		}

		var resourceSpec wyrd.ResourceManifest
		if err := yaml.Unmarshal(content, &resourceSpec); err != nil {
			return fmt.Errorf("failed parse manifest from %q: %w", filename, err)
		}

		// TODO: Use timeout!
		c, err := apiClient.CreateFromManifest(cfg.Context, resourceSpec)
		if err != nil {
			return fmt.Errorf("failed to create resource from %q: %w", filename, err)
		}

		log.Print("created ", c.Kind, ", ID: ", c.GetVersionedID())
	}

	return nil
}
