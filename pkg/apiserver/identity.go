package apiserver

import (
	"context"
	_ "embed"

	"github.com/sre-norns/urth/pkg/controllers"
	"github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/identity/httpapi"
	"github.com/sre-norns/wyrd/identity/pages"
)

// WebClientID is the OAuth client of Urth's web app. It binds stored browser
// sessions, so it does not change.
const WebClientID = "urth-web"

// CLIClientID is the OAuth client of urthctl's device login.
const CLIClientID = "urthctl"

// IdentityConfig is the identity configuration a Config describes: Urth's
// branded defaults, with the operator's identity options applied over them.
//
// A zero IdentityOptions means a Config built in code rather than parsed from
// flags -- kong always fills the issuer -- so the defaults stand alone. Config.
// Identity, when set, replaces all of it.
func IdentityConfig(cfg Config) (identity.Config, error) {
	if cfg.Identity != nil {
		return *cfg.Identity, nil
	}

	c := identity.DefaultConfig()
	c.ProductName, c.WebClientID = "Urth", WebClientID

	redirects := cfg.WebRedirectURIs
	if len(redirects) == 0 {
		redirects = []string{"http://localhost:8080/oauth/callback"}
	}
	c.Clients = map[string][]string{CLIClientID: {}, WebClientID: redirects}

	if cfg.IdentityOptions == (identity.Options{}) {
		return c, nil
	}

	return cfg.IdentityOptions.Apply(c)
}

// signInTheme is Urth's @sre-norns/components theme palette, for the sign-in
// pages the identity module serves. The pages read the kit's token names.
//
//go:embed signin-theme.css
var signInTheme string

// signInCopy is Urth's wording on the sign-in pages, in place of the
// defaults the pages were written with.
var signInCopy = pages.Copy{
	Tagline:  "Synthetic monitoring",
	Headline: "See it from inside.",
	// The headline rewords itself now and then, as Exp-Bench's does. The first
	// is the headline itself, so the page can come back to it.
	HeadlineVariants: []string{
		"See it from inside.",
		"See it from inside",
		"See it from inside…",
		"Probe it from inside.",
		"Watch it from inside.",
		"See it where it runs.",
		"Insight from inside.",
		"Inside, looking out.",
		"Check it from within.",
		"See it from the edge.",
		"Inside the network.",
		"Probe the process.",
		"See the inside story.",
	},
	AuthorizeHeadline:     "Connect to your runners.",
	Description:           "Probe services from the networks they live in.",
	InvitationDescription: "Join a monitoring account. Your other account memberships stay unchanged.",
	MachineTokens:         "runner tokens",
}

// SignInPages brands the sign-in, registration, recovery and invitation pages.
func SignInPages(privacyURL string) httpapi.Config {
	return httpapi.Config{
		ProductName: "Urth",
		PrivacyURL:  privacyURL,
		ThemeCSS:    signInTheme,
		Copy:        signInCopy,
	}
}

// mailLoop adapts an identity mail worker to a supervised loop. The workers
// return nil when stopped; a loop reports why it stopped, which is the
// context, so the manager can tell a shutdown from a crash.
func mailLoop(run func(context.Context)) controllers.Loop {
	return controllers.LoopFunc(func(ctx context.Context) error {
		run(ctx)
		return ctx.Err()
	})
}
