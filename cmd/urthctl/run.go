package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/probers/har"
	"github.com/sre-norns/urth/pkg/probers/http"
	"github.com/sre-norns/urth/pkg/probers/puppeteer"
	"github.com/sre-norns/urth/pkg/probers/pypuppeteer"
	"github.com/sre-norns/urth/pkg/probers/tcp"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

type RunCmd struct {
	runner.RunnerConfig `embed:"" prefix:"runner."`

	ScenarioId manifest.ResourceName `help:"Id of the scenario" name:"scenario" arg:"" optional:"" group:"scenario" xor:"file"`

	Files []string `name:"file" help:"A resource manifest file with the scenario" short:"f" type:"existingfile" group:"file" xor:"scenario"`
	Kind  string   `help:"The type of the scenario to run. Will try to guess if not specified" group:"file" `

	KeepTemp        bool `help:"If true, temporary work directory is kept after run is complete" prefix:"runner."`
	SaveHAR         bool `help:"If true, save HAR recording of the browser calls if applicable"`
	Headless        bool `help:"If true, puppeteer scripts are run in a headless mode" prefix:"puppeteer."`
	PageSlowSeconds int  `help:"For browser-based probs, slowdown page loads in seconds" prefix:"puppeteer."`
}

func (c *RunCmd) runScenario(cmdCtx context.Context, sourceName string, probSpec prob.Manifest, workingDir string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(cmdCtx, timeout)
	defer cancel()

	runResult, artifacts, err := runner.Play(ctx, probSpec, prob.RunOptions{
		Puppeteer: prob.PuppeteerOptions{
			Headless:         c.Headless,
			PageWaitSeconds:  c.PageSlowSeconds,
			WorkingDirectory: workingDir,
			TempDirPrefix:    fmt.Sprintf("%v-", sourceName), // Working dir name for the run
			KeepTempDir:      c.KeepTemp,
		},
		Http: prob.HttpOptions{
			CaptureResponseBody: false,
			CaptureRequestBody:  false,
			IgnoreRedirects:     false,
		},
	})
	if err != nil {
		return err
	}

	if runResult.Result == prob.RunFinishedSuccess {
		log.Print("artifacts produced: ", len(artifacts))
	}
	log.Printf("script finished: %q", runResult.Result)

	// Process artifacts produced by the local run - no uploading
	for _, artifact := range artifacts {
		log.Print("artifact: ", artifact.Artifact.Rel)

		if artifact.Artifact.Rel == "har" && c.SaveHAR {
			filename := fmt.Sprintf("%v.har", sourceName)
			if err := os.WriteFile(filename, artifact.Artifact.Content, 0644); err != nil {
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
			return content, "<stdin>", fmt.Errorf("failed to read content from STDIN: %w", err)
		}

		return content, "<stdin>", err
	}

	content, err := os.ReadFile(filename)
	return content, filepath.Ext(filename), err
}

func getScenarioProb(kind manifest.Kind, scenarioSpec any) (prob.Manifest, error) {
	if kind != urth.KindScenario {
		return prob.Manifest{}, fmt.Errorf("non-runnable manifest file of kind %q", kind)
	}

	spec, ok := scenarioSpec.(*urth.ScenarioSpec)
	if !ok {
		return prob.Manifest{}, fmt.Errorf("unexpected Spec type %q for the resource (kind=%q)", reflect.TypeOf(scenarioSpec), kind)
	}

	return spec.Prob, nil
}

func jobFromFile(filename string, kindHint string) (prob.Manifest, error) {
	if kindHint == "" {
		if scenario, ok, err := manifestFromFile(filename); err != nil {
			return prob.Manifest{}, err
		} else if ok {
			return getScenarioProb(scenario.Kind, scenario.Spec)
		}
	}

	content, ext, err := readContent(filename)
	if err != nil {
		return prob.Manifest{}, fmt.Errorf("failed to read content: %w", err)
	}

	kind := prob.Kind(kindHint)
	if kindHint == "" { // Kind guessing
		switch ext {
		case ".js", ".mjs":
			kind = puppeteer.Kind
		case ".py":
			kind = pypuppeteer.Kind
		case ".tcp":
			kind = tcp.Kind
		case ".http", ".rest":
			kind = http.Kind
		case ".har":
			kind = har.Kind
		}
	}

	// Did we guess anything?
	if string(kind) == "" {
		return prob.Manifest{}, fmt.Errorf("no kind provided for the input file (ext: %q)", ext)
	}

	switch kind {
	case puppeteer.Kind:
		return prob.Manifest{
			Kind: kind,
			Spec: &puppeteer.Spec{
				Script: string(content),
			},
		}, nil
	case http.Kind:
		return prob.Manifest{
			Kind: kind,
			Spec: &http.Spec{
				Script: string(content),
			},
		}, nil
	case har.Kind:
		return prob.Manifest{
			Kind: kind,
			Spec: &har.Spec{
				Script: string(content),
			},
		}, nil
	default:
		return prob.Manifest{}, fmt.Errorf("kind %v can not be run locally (yet)", kind)
	}
}

func (c *RunCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	// Check that either scenarioID or files is specified but not both
	if len(c.Files) == 0 && c.ScenarioId == "" {
		return fmt.Errorf("file or scenario Name must be provided")
	}
	if len(c.Files) != 0 && c.ScenarioId != "" {
		return fmt.Errorf("only file or scenario Name must be provided, but not both")
	}

	if c.ScenarioId != "" {
		resource, err := fetchScenario(cfg.Context, apiClient, c.ScenarioId)
		if err != nil {
			return err
		}

		return c.runScenario(cfg.Context, string(resource.Name), resource.Spec.Prob, c.WorkingDirectory, c.RunnerConfig.Timeout)
	}

	for _, filename := range c.Files {
		prob, err := jobFromFile(filename, c.Kind)
		if err != nil {
			return err
		}

		if err := c.runScenario(cfg.Context, strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)), prob, c.WorkingDirectory, c.RunnerConfig.Timeout); err != nil {
			return err
		}
	}

	return nil
}
