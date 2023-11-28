package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"gopkg.in/yaml.v3"
)

type RunCmd struct {
	Files []string `arg:"" optional:"" name:"file" help:"A script file to execute" type:"existingfile" xor:"scenario"`
	Kind  string   `help:"The type of the scenario to run. Will try to guess if not specified"`

	ScenarioId urth.ResourceID `help:"Id of the scenario" name:"scenario" xor:"file"`

	KeepTemp bool `help:"If true, temporary work directory is kept after run is complete"`
	SaveHAR  bool `help:"If true, save HAR recording of HTTP scripts if applicable"`
	Headless bool `help:"If true, puppeteer scripts are run in a headless mode"`
}

func (c *RunCmd) runScenario(cmdCtx context.Context, sourceName string, script *urth.ScenarioScript, workingDir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(cmdCtx, timeout)
	defer cancel()

	runResult, artifacts, err := runner.Play(ctx, script, runner.RunOptions{
		Puppeteer: runner.PuppeteerOptions{
			Headless:         c.Headless,
			WorkingDirectory: workingDir,
			TempDirPrefix:    fmt.Sprintf("run-%v-%v-", sourceName, 0),
			KeepTempDir:      c.KeepTemp,
		},
		Http: runner.HttpOptions{
			CaptureResponseBody: false,
			CaptureRequestBody:  false,
			IgnoreRedirects:     false,
		},
	})
	if err != nil {
		return err
	}

	if runResult.Result == urth.RunFinishedSuccess {
		log.Print("artifacts produced: ", len(artifacts))
	}
	log.Printf("script finished: %q", runResult.Result)

	// Process artifacts produced by the local run
	for _, artifact := range artifacts {
		log.Print("artifact: ", artifact.Rel)

		if artifact.Rel == "har" && c.SaveHAR {
			filename := fmt.Sprintf("run-%v.har", sourceName)
			if err := os.WriteFile(filename, artifact.Content, 0644); err != nil {
				return fmt.Errorf("failed to write HAR artifact: %w", err)
			}
		}
	}

	return nil
}

func readContent(filename string) ([]byte, string, error) {
	if filename == "-" {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return content, "", fmt.Errorf("failed to read content from STDIN: %w", err)
		}

		return content, "", err
	}

	content, err := os.ReadFile(filename)
	return content, filepath.Ext(filename), err
}

func jobFromFile(filename string, kindHint string) (*urth.ScenarioScript, error) {
	content, ext, err := readContent(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read content: %w", err)
	}

	if kindHint == "" {
		switch ext {
		case ".yaml", ".yml":
			var scenario urth.Scenario
			err := yaml.Unmarshal(content, &scenario)
			return scenario.Script, err
		case ".json":
			var scenario urth.Scenario
			err := json.Unmarshal(content, &scenario)

			return scenario.Script, err
		}

		// Fallthrough: Not a recognized format for scenario file
	}

	var kind urth.ScenarioKind
	// Kind guessing
	if kindHint != "" {
		kind = urth.ScenarioKind(kindHint)
	} else {
		switch ext {
		case ".js", ".mjs":
			kind = urth.PuppeteerKind
		case ".py":
			kind = urth.PyPuppeteerKind
		case ".tcp":
			kind = urth.TcpPortCheckKind
		case ".http", ".rest":
			kind = urth.HttpGetKind
		case ".har":
			kind = urth.HarKind
		}
	}

	if string(kind) == "" {
		return nil, fmt.Errorf("no script kind for content file (ext: %q)", ext)
	}

	return &urth.ScenarioScript{
		Kind:    kind,
		Content: content,
	}, nil
}

func (c *RunCmd) Run(cfg *commandContext) error {
	// Check that either scenarioID or files is specified but not both
	if len(c.Files) == 0 && c.ScenarioId == urth.InvalidResourceID {
		return fmt.Errorf("file or scenario ID must be provided")
	}
	if len(c.Files) != 0 && c.ScenarioId != urth.InvalidResourceID {
		return fmt.Errorf("only file or scenario ID must be provided, but not both")
	}

	if c.ScenarioId != urth.InvalidResourceID {
		scenario, err := fetchScenario(cfg.Context, c.ScenarioId, cfg.ApiServerAddress)
		if err != nil {
			return err
		}
		return c.runScenario(cfg.Context, scenario.Name, scenario.Script, cfg.WorkingDirectory, cfg.Timeout)
	}

	for _, filename := range c.Files {
		script, err := jobFromFile(filename, c.Kind)
		if err != nil {
			return err
		}

		if err := c.runScenario(cfg.Context, strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), script, cfg.WorkingDirectory, cfg.Timeout); err != nil {
			return err
		}
	}

	return nil
}
