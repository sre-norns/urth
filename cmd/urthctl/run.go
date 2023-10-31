package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

type RunCmd struct {
	ScenarioId urth.ResourceID `help:"Id of the scenario" name:"scenario" xor:"file"`
	File       string          `help:"A script file to execute" short:"f" type:"existingfile" xor:"scenario"`
	Kind       string          `help:"The type of the scenario to run. Will try to guess if not specified"`
	RunTimeout time.Duration   `help:"Timeout duration alloted for scenario to run" default:"3m"`
	KeepTemp   bool            `help:"If true, temporary work directory is kept after run is complete"`
	Headless   bool            `help:"If true, puppeteer scripts are run in a headless mode"`
}

// TODO: Pass app context!
func (c *RunCmd) runScenario(script urth.ScenarioScript, workingDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.RunTimeout)
	defer cancel()

	runResult, err := runner.Play(ctx, script, runner.RunOptions{
		Puppeteer: runner.PuppeteerOptions{
			Headless:         c.Headless,
			WorkingDirectory: workingDir,
			TempDirPrefix:    fmt.Sprintf("run-%v-%v-", "local", 0),
			KeepTempDir:      c.KeepTemp,
		},
	})
	if err != nil {
		return err
	}

	log.Printf("script finished: %q", runResult.Result)
	for _, artifact := range runResult.Artifacts {
		log.Println("artifact: ", artifact.Rel)

		if artifact.Rel == "har" {
			filename := fmt.Sprintf("job-%v.har", "local")
			if err := ioutil.WriteFile(filename, artifact.Content, 0644); err != nil {
				return fmt.Errorf("failed to write HAR artifact: %w", err)
			}
		}
	}
	return nil
}

func readContent(filename string) ([]byte, string, error) {
	if filename == "-" {
		content, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			return content, "", fmt.Errorf("failed to read content from STDIN: %w", err)
		}

		return content, "", err
	}

	content, err := ioutil.ReadFile(filename)
	return content, filepath.Ext(filename), err
}

func jobFromFile(filename string, kindHint string) (urth.ScenarioScript, error) {
	content, ext, err := readContent(filename)
	if err != nil {
		return urth.ScenarioScript{}, fmt.Errorf("failed to read content: %w", err)
	}

	// if kindHint == "" {
	// 	if ext == ".yaml" || ext == ".yml" {
	// 		var scenario urth.Scenario
	// 		err := yaml.Unmarshal(content, &scenario)

	// 		return urth.ScenarioToRunnable(scenario), err
	// 	}
	// 	if ext == ".json" {
	// 		var scenario urth.Scenario
	// 		err := json.Unmarshal(content, &scenario)

	// 		return urth.ScenarioToRunnable(scenario), err
	// 	}
	// 	// Fallthrough: Not a recognized format for scenario file
	// }

	var kind urth.ScenarioKind
	// Kind guessing
	if kindHint != "" {
		kind = urth.ScenarioKind(kindHint)
	} else if ext == ".js" || ext == ".mjs" {
		kind = urth.PuppeteerKind
	} else if ext == ".py" {
		kind = urth.PyPuppeteerKind
	} else if ext == ".tcp" {
		kind = urth.TcpPortCheckKind
	} else if ext == ".http" {
		kind = urth.HttpGetKind
	} else if ext == ".har" {
		kind = urth.HarKind
	}

	if string(kind) == "" {
		return urth.ScenarioScript{}, fmt.Errorf("no script kind for content file (ext: %q)", ext)
	}

	return urth.ScenarioScript{
		Kind:    kind,
		Content: content,
	}, nil
}

// func scenarioFromServer(scenarioId urth.ResourceID, apiServerAddress string) (urth.RunScenarioJob, error) {
// 	resource, err := (scenarioId, apiServerAddress)
// 	return urth.ScenarioToRunnable(resource), err
// }

func (c *RunCmd) Run(cfg *commandContext) error {
	var err error
	var script urth.ScenarioScript
	if c.File != "" {
		script, err = jobFromFile(c.File, c.Kind)
		if err != nil {
			return err
		}
		// } else if c.ScenarioId != 0 {
		// 	scenario, err = fetchScenario(c.ScenarioId, cfg.ApiServerAddress)
		// 	if err != nil {
		// 		return err
		// 	}
		//  script = scenario.Script
	} else {
		return fmt.Errorf("file or resource ID must be provided")
	}

	return c.runScenario(script, cfg.WorkingDirectory)
}
