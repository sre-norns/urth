package apiserver

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/sre-norns/wyrd/identity"
	"github.com/stretchr/testify/require"
)

// parseConfig parses a Config exactly as cmd/api-server does, from arguments
// and whatever the environment holds.
func parseConfig(t *testing.T, args ...string) (Config, *kong.Kong) {
	t.Helper()
	var cfg Config
	parser, err := kong.New(&cfg, kong.Name("urthd"))
	require.NoError(t, err)
	_, err = parser.Parse(args)
	require.NoError(t, err)
	return cfg, parser
}

func TestTrustedProxyEnvironmentAndFlags(t *testing.T) {
	t.Setenv("URTH_TRUSTED_PROXIES", "192.0.2.0/24,203.0.113.8")
	cfg, _ := parseConfig(t)
	require.Equal(t, []string{"192.0.2.0/24", "203.0.113.8"}, cfg.TrustedProxies)
	cfg, _ = parseConfig(t, "--http.trusted-proxy=198.51.100.1", "--http.trusted-proxy=198.51.100.2")
	require.Equal(t, []string{"198.51.100.1", "198.51.100.2"}, cfg.TrustedProxies)
}

// makefileIdentityVariables reads the URTH_ variables the run targets export.
func makefileIdentityVariables(t *testing.T) map[string]string {
	t.Helper()
	makefile, err := os.ReadFile("../../Makefile")
	require.NoError(t, err)
	variables := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^run-api-server[a-z-]*: export (URTH_[A-Z_]+) \?= (.*)$`).FindAllStringSubmatch(string(makefile), -1) {
		variables[m[1]] = strings.TrimSpace(m[2])
	}
	require.NotEmpty(t, variables, "the Makefile's run targets export no URTH_ variables; the pattern above is out of date")
	return variables
}

// Every variable the Makefile sets must be one the api-server reads, and must
// arrive in the field it is meant for. A misspelt or renamed variable is not an
// error anywhere else: kong ignores environment it does not know, and the
// server silently starts with its defaults.
func TestMakefileIdentityVariablesReachTheirFlags(t *testing.T) {
	variables := makefileIdentityVariables(t)
	for name, value := range variables {
		t.Setenv(name, strings.ReplaceAll(value, "$(CURDIR)", t.TempDir()))
	}
	cfg, parser := parseConfig(t)

	var known []string
	for _, flag := range parser.Model.Flags {
		known = append(known, flag.Envs...)
	}
	fields := map[string]func() any{
		"URTH_DEVELOPMENT":        func() any { return cfg.IdentityOptions.Development },
		"URTH_ISSUER":             func() any { return cfg.IdentityOptions.Issuer },
		"URTH_WEB_REDIRECT_URI":   func() any { return cfg.WebRedirectURIs },
		"URTH_MAIL_PROVIDER":      func() any { return cfg.IdentityOptions.MailProvider },
		"URTH_AUTH_MAIL_DIR":      func() any { return cfg.IdentityOptions.MailDirectory },
		"URTH_BOOTSTRAP_EMAIL":    func() any { return cfg.Bootstrap.Email },
		"URTH_BOOTSTRAP_PASSWORD": func() any { return cfg.Bootstrap.Password },
		"URTH_OIDC_ISSUER_URL":    func() any { return cfg.IdentityOptions.OIDCIssuerURL },
		"URTH_OIDC_CLIENT_ID":     func() any { return cfg.IdentityOptions.OIDCClientID },
		"URTH_OIDC_CLIENT_SECRET": func() any { return cfg.IdentityOptions.OIDCClientSecret },
	}
	for name := range variables {
		require.Contains(t, known, name, "the Makefile sets %s, which no flag reads", name)
		field, ok := fields[name]
		require.True(t, ok, "add %s to this test's field table", name)
		require.NotEmpty(t, field(), "%s did not reach its field", name)
	}
	require.Equal(t, []string{variables["URTH_WEB_REDIRECT_URI"]}, cfg.WebRedirectURIs)
	require.Equal(t, variables["URTH_ISSUER"], cfg.IdentityOptions.Issuer)
	require.True(t, cfg.IdentityOptions.Development)
}

func TestIdentityConfigFromFlags(t *testing.T) {
	// The development mailer creates its directory 0700 and refuses a shared one.
	mail := filepath.Join(t.TempDir(), "mail")
	cfg, _ := parseConfig(t,
		"--identity.issuer=http://localhost:3001",
		"--identity.development",
		"--identity.mail-provider=development",
		"--identity.mail-directory="+mail,
		"--identity.web-redirect-uri=http://localhost:3001/oauth/callback",
		"--identity.web-redirect-uri=http://localhost:8080/oauth/callback",
	)
	c, err := IdentityConfig(cfg)
	require.NoError(t, err)
	require.Equal(t, "http://localhost:3001", c.Issuer)
	require.Equal(t, "Urth", c.ProductName)
	require.Equal(t, WebClientID, c.WebClientID)
	require.Equal(t, []string{"http://localhost:3001/oauth/callback", "http://localhost:8080/oauth/callback"}, c.Clients[WebClientID])
	require.Contains(t, c.Clients, CLIClientID)
	require.NotNil(t, c.SendIdentityMail, "a configured mail provider must produce a sender")
	require.NoError(t, identity.NewService(nil).Configure(c), "the parsed configuration must be one identity accepts")
}

// A Config built in code -- the integration harness, an embedded host -- has no
// identity options at all. It gets the branded defaults and no mail, rather
// than an empty issuer that Configure would refuse.
func TestIdentityConfigWithoutOptionsKeepsTheDefaults(t *testing.T) {
	c, err := IdentityConfig(Config{})
	require.NoError(t, err)
	require.Equal(t, identity.DefaultConfig().Issuer, c.Issuer)
	require.Equal(t, []string{"http://localhost:8080/oauth/callback"}, c.Clients[WebClientID])
	require.Nil(t, c.SendIdentityMail)

	override := identity.DefaultConfig()
	override.ProductName = "Embedded"
	c, err = IdentityConfig(Config{Identity: &override, IdentityOptions: identity.Options{Issuer: "https://ignored.example"}})
	require.NoError(t, err)
	require.Equal(t, "Embedded", c.ProductName, "Config.Identity replaces the flags entirely")
}

func TestSignInPagesCarryUrthsBrand(t *testing.T) {
	pages := SignInPages("")
	require.Equal(t, "Urth", pages.ProductName)
	require.Empty(t, pages.PrivacyURL, "Urth serves no privacy page, so the link is off unless configured")
	require.Contains(t, pages.ThemeCSS, "--primary-container: #22c55e;")
	for _, token := range []string{"--page-glow", "--page-field-outline", "--page-success", "--page-success-container", "--page-error-container"} {
		require.True(t, slices.ContainsFunc(strings.Split(pages.ThemeCSS, "\n"), func(line string) bool {
			return strings.HasPrefix(strings.TrimSpace(line), token+":")
		}), "the theme leaves %s at the default theme's cyan-tinted value", token)
	}
}
