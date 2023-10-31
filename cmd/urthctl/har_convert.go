package main

import (
	"fmt"
	"os"

	httpparser "github.com/sre-norns/urth/pkg/http-parser"
	"github.com/sre-norns/urth/pkg/runner"
)

type ConvertHar struct {
	// File string `help:"A HAR file to convert" short:"f" type:"existingfile"`
	Files []string `arg:"" optional:"" name:"path" help:"HAR file(s) to convert" type:"existingfile"`
	Out   string   `help:"Output file name to write to" short:"o" type:"file"`
}

func (c *ConvertHar) Run(cfg *commandContext) error {
	for _, file := range c.Files {
		file, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("failed to open input HAR file: %w", err)
		}
		defer file.Close()

		output := os.Stdout
		if c.Out != "" && c.Out != "-" {
			output, err = os.OpenFile(c.Out, os.O_CREATE|os.O_WRONLY, 0766)
			if err != nil {
				return fmt.Errorf("failed to open output file: %w", err)
			}
			defer output.Close()
		}

		harLog, err := runner.UnmarshalHAR(file)
		if err != nil {
			return fmt.Errorf("failed to deserialize HAR file: %w", err)
		}

		requests, err := runner.ConvertHarToHttpTester(harLog.Log.Entries)
		if err != nil {
			return fmt.Errorf("failed to convert HAR: %w", err)
		}

		if err := httpparser.Marshal(output, requests); err != nil {
			return err
		}
	}

	return nil
}
