package main

import (
	"fmt"
	"os"

	httpparser "github.com/sre-norns/urth/pkg/http-parser"
	"github.com/sre-norns/urth/pkg/probers/har"
)

type ConvertHar struct {
	Files []string `arg:"" optional:"" name:"path" help:"HAR file(s) to convert" type:"existingfile"`
	Out   string   `help:"Name of the output file to write to. Default output is STDOUT" short:"o" type:"file"`
}

func (c *ConvertHar) Run(cfg *commandContext) error {
	var err error

	output := os.Stdout
	if c.Out != "" && c.Out != "-" {
		output, err = os.OpenFile(c.Out, os.O_CREATE|os.O_WRONLY, 0766)
		if err != nil {
			return fmt.Errorf("failed to open output file: %w", err)
		}
		defer output.Close()
	}

	for _, filename := range c.Files {
		var file *os.File

		if filename == "-" {
			file = os.Stdin
		} else {
			file, err := os.Open(filename)
			if err != nil {
				return fmt.Errorf("failed to open input HAR %q file: %w", filename, err)
			}
			defer file.Close()
		}

		harLog, err := har.UnmarshalHAR(file)
		if err != nil {
			return fmt.Errorf("failed to deserialize HAR file: %w", err)
		}

		requests, err := har.ConvertHarToHttpTester(harLog.Log.Entries)
		if err != nil {
			return fmt.Errorf("failed to convert HAR: %w", err)
		}

		if err := httpparser.Marshal(output, requests); err != nil {
			return err
		}
	}

	return nil
}
