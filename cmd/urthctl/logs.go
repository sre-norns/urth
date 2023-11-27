package main

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
)

type getLogs struct {
	Selector string `help:"Keys to match" optional:"" name:"selector" short:"l"`

	RunID urth.ResourceID `arg:"" optional:"" help:"Name of scenario run to show logs for"`
}

func (c *getLogs) Run(cfg *commandContext) error {
	ctx, cancel := context.WithTimeout(cfg.Context, 30*time.Second)
	defer cancel()

	logStream, err := fetchLogs(ctx, cfg.ApiServerAddress, c.RunID, c.Selector)
	if err != nil {
		return err
	}

	for logs := range logStream {
		_, err = io.Copy(os.Stdout, logs)
		if err != nil {
			return err
		}
	}

	return nil
}
