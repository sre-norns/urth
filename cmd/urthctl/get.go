package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sre-norns/urth/pkg/urth"
	"gopkg.in/yaml.v3"
)

type (
	Scenario struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Script struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	Results struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
		RunId      urth.ResourceID `help:"Id of the run results" arg:"" name:"result" `
	}

	Runner struct {
		ScenarioId urth.ResourceID `help:"Id of the scenario" arg:"" name:"scenario" `
	}

	GetCmd struct {
		Scenario Scenario `cmd:"" help:"Get scenario object from the server"`
		Script   Script   `cmd:"" help:"Get a script data for a given scenario"`
		Results  Results  `cmd:"" help:"Get a run result"`
		Runner   Runner   `cmd:"" help:"Get a runner object from the server"`
	}
)

type formatter func(any) error

func yamlFormatter(resource any) error {
	data, err := yaml.Marshal(resource)
	if err != nil {
		return err
	}
	fmt.Print(string(data))

	return nil
}

func jsonFormatter(resource any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "\t")

	err := encoder.Encode(resource)
	if err != nil {
		return err
	}
	// fmt.Print(string(data))

	return nil
}

func getFormatter(formatName string) (formatter, error) {
	switch formatName {
	case "yaml":
		return yamlFormatter, nil
	case "json":
		return jsonFormatter, nil
	}

	return nil, fmt.Errorf("unexpected output format %q", formatName)
}

// func (c *Scenario) Run(cfg *commandContext) error {
// 	format, err := getFormatter(cfg.Format)
// 	if err != nil {
// 		return err
// 	}

// 	resource, err := fetchScenario(c.ScenarioId, cfg.ApiServerAddress)
// 	if err != nil {
// 		return err
// 	}

// 	return format(&resource)
// }

// func (c *Results) Run(cfg *commandContext) error {
// 	format, err := getFormatter(cfg.Format)
// 	if err != nil {
// 		return err
// 	}

// 	resource, err := fetchResults(c.ScenarioId, c.RunId, cfg.ApiServerAddress)
// 	if err != nil {
// 		return err
// 	}

// 	return format(&resource)
// }
