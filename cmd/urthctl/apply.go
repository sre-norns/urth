package main

import (
	"fmt"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
	"gopkg.in/yaml.v3"
)

type ApplyCmd struct {
	Filename string `help:"Filename of a resource to apply to the API server" arg:"" name:"file"`
}

func (c *ApplyCmd) Run(cfg *commandContext) error {
	content, _, err := readContent(c.Filename)
	if err != nil {
		return nil
	}

	// FIXME: Should be just a universal resource manifest file
	var resourceSpec wyrd.ResourceManifest
	if err := yaml.Unmarshal(content, &resourceSpec); err != nil {
		return err
	}

	_, err = urth.NewRestApiClient(cfg.ApiServerAddress)
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	// WIP
	// _, err = apiClient.ApplyObjectDefinition(cfg.Context, resourceSpec)
	// if err != nil {
	// 	return err
	// }

	return nil
}
