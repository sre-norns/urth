package main

import (
	"context"

	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/grace"
	"github.com/sre-norns/urth/pkg/runner"
)

type commandContext struct {
	*runner.RunnerConfig

	OutputFormatter formatter
	Context         context.Context
}

type outputFormat string

func (f outputFormat) AfterApply(cfg *commandContext) (err error) {
	cfg.OutputFormatter, err = getFormatter(f)
	return err
}

var appCli struct {
	runner.RunnerConfig

	// short:"o"
	Format outputFormat `enum:"yaml,yml,json" help:"Data output format" default:"yml"`

	Run   RunCmd   `cmd:"" help:"Run a scenario or a script locally"`
	Get   GetCmd   `cmd:"" help:"Get and display a managed resource(s) from the server"`
	Apply ApplyCmd `cmd:"" help:"Apply a new configuration to a resource"`

	Convert ConvertHar `cmd:"" help:"Convert HAR file into a .http file format"`
}

func main() {
	mainContext := grace.SetupSignalHandler()
	cfg := &commandContext{
		Context:         mainContext,
		OutputFormatter: yamlFormatter,
		RunnerConfig:    &appCli.RunnerConfig,
	}
	appCtx := kong.Parse(&appCli,
		kong.Name("urthctl"),
		kong.Description("Urth Command line tool"),
		kong.Bind(cfg),
	)

	appCtx.FatalIfErrorf(appCtx.Run(cfg))
}
