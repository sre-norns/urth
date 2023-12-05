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

	"gopkg.in/yaml.v3"

	"github.com/sre-norns/urth/pkg/probers/har_prob"
	"github.com/sre-norns/urth/pkg/probers/http_prob"
	"github.com/sre-norns/urth/pkg/probers/puppeteer_prob"
	"github.com/sre-norns/urth/pkg/probers/pypuppeteer_prob"
	"github.com/sre-norns/urth/pkg/probers/tcp_prob"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
)

type RunCmd struct {
	Files []string `arg:"" optional:"" name:"file" help:"A script file to execute" type:"existingfile" xor:"scenario"`
	Kind  string   `help:"The type of the scenario to run. Will try to guess if not specified"`

	ScenarioId wyrd.ResourceID `help:"Id of the scenario" name:"scenario" xor:"file"`

	KeepTemp bool `help:"If true, temporary work directory is kept after run is complete"`
	SaveHAR  bool `help:"If true, save HAR recording of HTTP scripts if applicable"`
	Headless bool `help:"If true, puppeteer scripts are run in a headless mode"`
}

func (c *RunCmd) runScenario(cmdCtx context.Context, sourceName string, prob *urth.ProbManifest, workingDir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(cmdCtx, timeout)
	defer cancel()

	runResult, artifacts, err := runner.Play(ctx, *prob, runner.RunOptions{
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

	// Process artifacts produced by the local run - no uploading
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

func jobFromFile(filename string, kindHint string) (*urth.ProbManifest, error) {
	content, ext, err := readContent(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read content: %w", err)
	}

	if kindHint == "" {
		switch ext {
		case ".yaml", ".yml":
			var scenario wyrd.ResourceManifest
			err := yaml.Unmarshal(content, &scenario)
			if err != nil {
				return nil, err
			}

			if scenario.Kind != urth.KindScenario {
				return nil, fmt.Errorf("non-runnable manifest file of kind %q", scenario.Kind)
			}

			return &scenario.Spec.(*urth.ScenarioSpec).Prob, err
		case ".json":
			var scenario wyrd.ResourceManifest
			err := json.Unmarshal(content, &scenario)
			if err != nil {
				return nil, err
			}

			if scenario.Kind != urth.KindScenario {
				return nil, fmt.Errorf("non-runnable manifest file of kind %q", scenario.Kind)
			}

			return &scenario.Spec.(*urth.ScenarioSpec).Prob, err
		}

		// Fallthrough: Not a recognized format for scenario file
	}

	var kind urth.ProbKind
	// Kind guessing
	if kindHint != "" {
		kind = urth.ProbKind(kindHint)
	} else {
		switch ext {
		case ".js", ".mjs":
			kind = puppeteer_prob.Kind
		case ".py":
			kind = pypuppeteer_prob.Kind
		case ".tcp":
			kind = tcp_prob.Kind
		case ".http", ".rest":
			kind = http_prob.Kind
		case ".har":
			kind = har_prob.Kind
		}
	}

	if string(kind) == "" {
		return nil, fmt.Errorf("no kind provided for the input file (ext: %q)", ext)
	}

	switch kind {
	case puppeteer_prob.Kind:
		return &urth.ProbManifest{
			Kind: kind,
			Spec: puppeteer_prob.Spec{
				Script: string(content),
			},
		}, nil
	case http_prob.Kind:
		return &urth.ProbManifest{
			Kind: kind,
			Spec: http_prob.Spec{
				Script: string(content),
			},
		}, nil
	case har_prob.Kind:
		return &urth.ProbManifest{
			Kind: kind,
			Spec: har_prob.Spec{
				Script: string(content),
			},
		}, nil
	default:
		return nil, fmt.Errorf("kind %v can not be run locally (yet)", kind)
	}
}

func (c *RunCmd) Run(cfg *commandContext) error {
	// Check that either scenarioID or files is specified but not both
	if len(c.Files) == 0 && c.ScenarioId == wyrd.InvalidResourceID {
		return fmt.Errorf("file or scenario ID must be provided")
	}
	if len(c.Files) != 0 && c.ScenarioId != wyrd.InvalidResourceID {
		return fmt.Errorf("only file or scenario ID must be provided, but not both")
	}

	if c.ScenarioId != wyrd.InvalidResourceID {
		resource, err := fetchScenario(cfg.Context, c.ScenarioId, cfg.ApiServerAddress)
		if err != nil {
			return err
		}
		prob := resource.Spec.(*urth.ScenarioSpec).Prob
		return c.runScenario(cfg.Context, resource.Name, &prob, cfg.WorkingDirectory, cfg.Timeout)
	}

	for _, filename := range c.Files {
		prob, err := jobFromFile(filename, c.Kind)
		if err != nil {
			return err
		}

		if err := c.runScenario(cfg.Context, strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), prob, cfg.WorkingDirectory, cfg.Timeout); err != nil {
			return err
		}
	}

	return nil
}
