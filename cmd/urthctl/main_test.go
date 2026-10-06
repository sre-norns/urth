package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
	if cfg.Token != "explicit" || cfg.APIServerAddress != "https://urth.sre-norns.com" || cfg.Project != "p" {
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

// Someone whose project access was removed still has a membership: adding
// them again restores it, guarded by its revision, rather than creating a
// second one the server refuses with already-exists.
func TestMembersAddRestoresARemovedMembership(t *testing.T) {
	var patched string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		inactive := `{"apiVersion":"identity.sre-norns.com/v1","kind":"project-memberships","metadata":{"uid":"m2","name":"","version":3},"spec":{"userId":"u2"},"status":{"email":"bob@example.test","phase":"inactive","displayName":null}}`
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/projects/p/member-candidates":
			fmt.Fprint(w, `{"items":[{"user_id":"u2","email":"bob@example.test","project_membership_status":"inactive"}]}`)
		case r.Method == "GET" && r.URL.Path == "/v1/projects/p/memberships":
			fmt.Fprintf(w, `{"items":[%s]}`, inactive)
		case r.Method == "GET" && r.URL.Path == "/v1/project-memberships/m2":
			fmt.Fprint(w, inactive)
		case r.Method == "PATCH" && r.URL.Path == "/v1/project-memberships/m2":
			body, _ := io.ReadAll(r.Body)
			patched = r.Header.Get("If-Match") + " " + string(body)
			fmt.Fprint(w, strings.ReplaceAll(strings.ReplaceAll(inactive, "inactive", "active"), `"version":3`, `"version":4`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	parsed, appCli, cfg := parse(t, "members", "add", "bob@example.test", "--token=t", "--project=p", "--api-server-address="+server.URL)
	var out bytes.Buffer
	cfg.Env.Output = cli.Output{Format: cli.FormatJSON, Stdout: &out}
	if err := prepareCommand(parsed, appCli, cfg); err != nil {
		t.Fatal(err)
	}
	if err := parsed.Run(cfg); err != nil {
		t.Fatal(err)
	}
	if patched != `"3" {"operation":"activate"}` {
		t.Fatalf("restored with %q", patched)
	}
	if !strings.Contains(out.String(), `"phase": "active"`) {
		t.Fatalf("printed %s", out.String())
	}
}

func TestLocalFileRunDoesNotSelectAProfile(t *testing.T) {
	for _, state := range []string{"signed-out", "expired"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv("URTH_PROFILES", filepath.Join(t.TempDir(), "profiles.json"))
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "remote authentication is unavailable", http.StatusServiceUnavailable)
			}))
			defer server.Close()
			profile := cli.Profile{Endpoint: server.URL, Type: cli.PrincipalUser, Scope: model.ScopeAccount, AccountID: "acme", ProjectID: "web"}
			if state == "expired" {
				profile.Token, profile.RefreshToken = "expired", "refresh"
				profile.ExpiresAt = time.Now().Add(-time.Hour)
			}
			if err := app.SaveProfiles(cli.Profiles{Default: "work", Profiles: map[string]cli.Profile{"work": profile}}); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "check.http")
			if err := os.WriteFile(file, []byte("GET http://localhost/\n"), 0600); err != nil {
				t.Fatal(err)
			}
			parsed, appCli, cfg := parse(t, "run", "-f", file, "--runner.working-directory="+t.TempDir())
			if err := prepareCommand(parsed, appCli, cfg); err != nil {
				t.Errorf("local execution must not require sign-in: %v", err)
			}
			if requests.Load() != 0 {
				t.Errorf("local execution made %d authentication requests", requests.Load())
			}
			if cfg.Token != "" || cfg.Project != "" || cfg.Account != "" {
				t.Error("local execution inherited a remote profile")
			}
			// Running the server's scenario by name still needs that profile.
			parsed, appCli, cfg = parse(t, "run", "server-scenario", "--runner.working-directory="+t.TempDir())
			if err := prepareCommand(parsed, appCli, cfg); err == nil {
				t.Error("server scenario execution accepted an unusable profile")
			}
		})
	}
}

func TestIdentityViewsUseCanonicalJSONAndYAML(t *testing.T) {
	for _, format := range []string{cli.FormatJSON, cli.FormatYAML} {
		for _, value := range []any{
			projectView{model.Project{Resource: model.Resource{ID: "p", Name: "project", Revision: 2}}},
			memberView{model.ProjectMembership{Resource: model.Resource{ID: "m", Revision: 2}, UserID: "u"}},
			invitationView{model.AccountInvitation{Resource: model.Resource{ID: "i", Revision: 2}, Token: "do-not-print"}},
			sessionView{Session: model.Session{Resource: model.Resource{ID: "s", Revision: 2}}},
		} {
			var out bytes.Buffer
			if err := (cli.Output{Format: format, Stdout: &out}).Encode(value); err != nil {
				t.Fatal(err)
			}
			text := out.String()
			for _, want := range []string{"identity.sre-norns.com/v1", "metadata", "spec", "status"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %s: %s", want, text)
				}
			}
			if strings.Contains(text, "do-not-print") {
				t.Fatal("read view leaked credential")
			}
		}
	}
}

func TestCurrentExampleManifests(t *testing.T) {
	files, err := filepath.Glob("../../examples/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		switch filepath.Ext(path) {
		case ".yaml", ".yml", ".json":
		default:
			continue
		}
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := decodeManifest(data)
			if err != nil {
				t.Fatal(err)
			}
			if doc.APIVersion != urth.APIVersion || doc.Kind == "" || doc.Spec == nil {
				t.Fatalf("not a canonical create/apply document: %+v", doc)
			}
		})
	}
}
