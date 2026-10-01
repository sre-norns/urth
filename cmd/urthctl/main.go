package main

import (
	"context"
	"strings"

	"github.com/alecthomas/kong"
	_ "github.com/joho/godotenv/autoload"
	"github.com/sre-norns/urth/pkg/client"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity/cli"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// app is urthctl as the shared CLI kit knows it. ClientID must be the OAuth
// client the api-server registers (apiserver.CLIClientID).
var app = cli.App{
	Name:        "urthctl",
	ClientID:    "urthctl",
	ConfigDir:   "urth",
	ProfilesEnv: "URTH_PROFILES",
}

type commandContext struct {
	*client.APIClientConfig

	Env     *cli.Env
	Context context.Context
}

func (c *commandContext) ClientCallContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Context, c.APIClientConfig.Timeout)
}

type CLI struct {
	client.APIClientConfig
	cli.OutputFlag

	Profile string `help:"Name of the profile for this invocation. Does not change the default profile"`

	Auth       cli.AuthCmd    `cmd:"" help:"Sign in and out"`
	ProfileCmd cli.ProfileCmd `cmd:"" name:"profile" help:"Manage local CLI profiles"`
	Context    cli.ContextCmd `cmd:"" help:"Show or set the project commands address"`

	Accounts             AccountsCmd             `cmd:"" help:"Accounts you belong to"`
	Projects             ProjectsCmd             `cmd:"" help:"The account's projects"`
	Members              MembersCmd              `cmd:"" help:"Who may work in the context's project"`
	Invitations          InvitationsCmd          `cmd:"" help:"Invite people to the account"`
	Sessions             SessionsCmd             `cmd:"" help:"Where you are signed in"`
	Runners              RunnersCmd              `cmd:"" help:"Runner operations"`
	RunnerAuthorizations RunnerAuthorizationsCmd `cmd:"" name:"runner-authorizations" help:"Which runners may run the project's scenarios"`

	Create createCmd `cmd:"" help:"Create a resource on the server form a manifest"`
	Apply  ApplyCmd  `cmd:"" help:"Apply a new configuration to a resource"`

	Run     RunCmd     `cmd:"" help:"Run a scenario or a script locally"`
	Trigger TriggerCmd `cmd:"" help:"Start a run of a scenario on the server now"`
	Get     GetCmd     `cmd:"" help:"Get and display a managed resource(s) from the server"`
	Logs    getLogs    `cmd:"" help:"Show logs for a scenario run"`

	// Top-level rather than under `get`, because they are actions rather than
	// reads: a retry schedules a new run.
	Retry   RetryCmd   `cmd:"" help:"Retry a dispatch that stopped making progress"`
	Resolve ResolveCmd `cmd:"" help:"Close a dispatch failure without retrying it"`

	Convert ConvertHar `cmd:"" help:"Convert HAR file into a .http file format"`
}

func main() {
	var appCli CLI
	cfg := &commandContext{
		Context:         grace.NewSignalHandlingContext(),
		APIClientConfig: &appCli.APIClientConfig,
		Env:             &cli.Env{App: app},
	}
	appCtx := kong.Parse(&appCli,
		kong.Name("urthctl"),
		kong.Description("Urth Command line tool"),
		kong.Bind(cfg, cfg.Env),
	)

	appCtx.FatalIfErrorf(prepareCommand(appCtx, &appCli, cfg))
	appCtx.FatalIfErrorf(appCtx.Run(cfg))
}

// prepareCommand resolves who and where a command runs as. Flags win; a
// profile fills in the rest: its endpoint and token, the account it signed in
// to, and the project its context names.
func prepareCommand(parsed *kong.Context, appCli *CLI, cfg *commandContext) error {
	endpointExplicit := false
	for _, node := range parsed.Path {
		if node.Flag != nil && node.Flag.Name == "api-server-address" {
			endpointExplicit = true
		}
	}
	env := cfg.Env
	env.Context = cfg.Context
	env.Endpoint, env.EndpointExplicit = cfg.APIServerAddress, endpointExplicit
	env.Token, env.ProfileName = string(cfg.Token), appCli.Profile
	env.Timeout, env.HTTPClient = cfg.Timeout, cfg.HTTPClient
	if env.Output.Stdout == nil {
		env.Output = cli.NewOutput(appCli.Output)
	}

	switch strings.Fields(parsed.Command())[0] {
	case "auth", "profile", "context", "convert":
		return nil
	case "run":
		// A file runs locally. Only a scenario fetched by name needs the
		// remote profile, which may be signed out or unable to refresh.
		if len(appCli.Run.Files) > 0 {
			return nil
		}
	}
	selected, err := env.Select()
	if err != nil || !selected.Found {
		return err
	}
	p := selected.Profile
	cfg.APIServerAddress, cfg.Token = p.Endpoint, urth.APIToken(p.Token)
	if cfg.Account == "" {
		cfg.Account = manifest.ResourceID(p.AccountID)
	}
	if cfg.Project == "" {
		cfg.Project = manifest.ResourceID(p.ProjectID)
	}
	return nil
}
