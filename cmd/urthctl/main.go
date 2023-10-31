package main

import (
	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/runner"
)

type commandContext struct {
	runner.RunnerConfig

	Format string `help:"Output format json/yaml" default:"json"`
}

var appCli struct {
	runner.RunnerConfig

	Format string `help:"Data output format json/yaml" default:"json"`

	Run     RunCmd     `cmd:"" help:"Run a scenario or a script locally"`
	Get     GetCmd     `cmd:""`
	Convert ConvertHar `cmd:"" help:"Convert HAR file into a plain .http list"`
}

func main() {
	appCtx := kong.Parse(&appCli,
		kong.Name("urthctl"),
		kong.Description("Urth Command line tool"),
	)

	appCtx.FatalIfErrorf(appCtx.Run(&commandContext{
		Format:       appCli.Format,
		RunnerConfig: appCli.RunnerConfig,
	}))
}
