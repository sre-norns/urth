package main

import (
	"context"

	// TODO: Add dotenv autoloader

	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/grace"
)

type commandContext struct {
	*urth.ApiClientConfig
	*runner.RunnerConfig

	OutputFormatter formatter
	Context         context.Context
}

type outputFormat string

func (c *commandContext) ClientCallContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Context, c.ApiClientConfig.Timeout)
}

func (f outputFormat) AfterApply(cfg *commandContext) (err error) {
	cfg.OutputFormatter, err = getFormatter(f)
	return err
}

var appCli struct {
	urth.ApiClientConfig
	runner.RunnerConfig `embed:"" prefix:"runner."`

	// short:"o"
	Format outputFormat `enum:"yaml,yml,json" help:"Data output format" default:"yml"`

	Run    RunCmd    `cmd:"" help:"Run a scenario or a script locally"`
	Get    GetCmd    `cmd:"" help:"Get and display a managed resource(s) from the server"`
	Apply  ApplyCmd  `cmd:"" help:"Apply a new configuration to a resource"`
	Logs   getLogs   `cmd:"" help:"Show logs for a scenario run"`
	Create createCmd `cmd:"" help:"Create a resource on the server form a manifest"`

	Convert ConvertHar `cmd:"" help:"Convert HAR file into a .http file format"`
}

func main() {
	mainContext := grace.NewSignalHandlingContext()
	cfg := &commandContext{
		Context:         mainContext,
		OutputFormatter: yamlFormatter,
		ApiClientConfig: &appCli.ApiClientConfig,
		RunnerConfig:    &appCli.RunnerConfig,
	}
	appCtx := kong.Parse(&appCli,
		kong.Name("urthctl"),
		kong.Description("Urth Command line tool"),
		kong.Bind(cfg),
	)

	appCtx.FatalIfErrorf(appCtx.Run(cfg))
}
