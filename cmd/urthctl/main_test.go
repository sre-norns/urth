package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/apiserver"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity/cli"
	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// parse builds urthctl's parser as main does and parses args. Building it is
// itself the first check: kong refuses two flags with one short name only at
// run time, and `convert -o` against the global -o was exactly that.
func parse(t *testing.T, args ...string) (*kong.Context, *CLI, *commandContext) {
	t.Helper()
	var appCli CLI
	cfg := &commandContext{Context: context.Background(), APIClientConfig: &appCli.APIClientConfig, Env: &cli.Env{App: app}}
	parser, err := kong.New(&appCli, kong.Bind(cfg, cfg.Env), kong.Writers(io.Discard, io.Discard))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parser.Parse(args)
	if err != nil {
		t.Fatal(err)
	}
	return parsed, &appCli, cfg
}

func TestTheCLIClientIsTheOneTheServerRegisters(t *testing.T) {
	if app.ClientID != apiserver.CLIClientID {
		t.Fatalf("urthctl signs in as %q; the api-server registers %q", app.ClientID, apiserver.CLIClientID)
	}
}

func TestAProfileSuppliesWhereAndAsWhom(t *testing.T) {
	t.Setenv("URTH_PROFILES", filepath.Join(t.TempDir(), "profiles.json"))
	store := cli.Profiles{Default: "work", Profiles: map[string]cli.Profile{
		"work": {Endpoint: "https://urth.example", Type: cli.PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", Token: "t", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour), ProjectID: "web"},
	}}
	if err := app.SaveProfiles(store); err != nil {
		t.Fatal(err)
	}

	parsed, appCli, cfg := parse(t, "get", "scenarios")
	if err := prepareCommand(parsed, appCli, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.APIServerAddress != "https://urth.example" || cfg.Token != "t" || cfg.Account != "acme" || cfg.Project != "web" {
		t.Fatalf("profile not applied: %+v", *cfg.APIClientConfig)
	}

	// Flags name another project or account for one command.
	parsed, appCli, cfg = parse(t, "get", "scenarios", "--project=api", "--account=other")
	if err := prepareCommand(parsed, appCli, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Project != "api" || cfg.Account != "other" || cfg.Token != "t" {
		t.Fatalf("flags did not win: %+v", *cfg.APIClientConfig)
	}

	// A profile's token is never sent to an endpoint it was not issued by.
	parsed, appCli, cfg = parse(t, "get", "scenarios", "--api-server-address=https://elsewhere.example")
	if err := prepareCommand(parsed, appCli, cfg); err == nil || !strings.Contains(err.Error(), "endpoint differs") {
		t.Fatalf("profile credential redirected: %v", err)
	}

	// An explicit token bypasses profiles altogether.
	parsed, appCli, cfg = parse(t, "get", "scenarios", "--token=explicit", "--project=p", "--account=a")
	if err := prepareCommand(parsed, appCli, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "explicit" || cfg.APIServerAddress != "http://localhost:8080" || cfg.Project != "p" {
		t.Fatalf("explicit token: %+v", *cfg.APIClientConfig)
	}
}

// `get -o yaml` prints manifests, so what it prints can be edited and applied.
func TestStructuredOutputIsTheManifest(t *testing.T) {
	scenario := urth.Scenario{
		ObjectMeta: manifest.ObjectMeta{Name: "probe", Version: 3},
		Spec:       urth.ScenarioSpec{IsActive: true},
	}
	var out bytes.Buffer
	if err := cli.RenderResource(cli.Output{Format: cli.FormatYAML, Stdout: &out}, scenarioView{scenario}, nil); err != nil {
		t.Fatal(err)
	}
	m, _, err := cli.DecodeObject[manifest.ResourceManifest](out.Bytes())
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if m.Kind != urth.KindScenario || m.Metadata.Name != "probe" || m.Metadata.Version != 3 {
		t.Fatalf("not a scenario manifest: %+v\n%s", m, out.String())
	}

	out.Reset()
	if err := cli.RenderList(cli.Output{Format: cli.FormatWide, Stdout: &out}, []scenarioView{{scenario}}, manifest.Page{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "SCHEDULE", "probe", "true"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("table lacks %q:\n%s", want, out.String())
		}
	}
}
