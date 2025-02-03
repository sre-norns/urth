package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type getLogs struct {
	Selector string `help:"Keys to match" optional:"" name:"selector" short:"l"`

	RunID manifest.ResourceName `arg:"" optional:"" help:"Name of scenario run to show logs for"`
}

func (c *getLogs) Run(cfg *commandContext) error {
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

	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	logStream, err := fetchLogs(ctx, apiClient, c.RunID, q)
	if err != nil {
		return err
	}

	for logs := range logStream {
		if _, err = io.Copy(os.Stdout, logs); err != nil {
			return err
		}
	}

	return nil
}
