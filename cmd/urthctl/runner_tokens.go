package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sre-norns/wyrd/identity/cli"
	identityclient "github.com/sre-norns/wyrd/identity/client"
	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/identity/resource"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

type RunnerTokensCmd struct {
	Issue  RunnerTokensIssueCmd  `cmd:"" help:"Issue a token and show its secret once"`
	List   RunnerTokensListCmd   `cmd:"" help:"List token metadata without secrets"`
	Get    RunnerTokensGetCmd    `cmd:"" help:"Show token metadata without its secret"`
	Revoke RunnerTokensRevokeCmd `cmd:"" help:"Revoke one token using its current version"`
}

type RunnerTokensIssueCmd struct {
	Runner         manifest.ResourceName `arg:"" help:"Runner name"`
	Name           string                `arg:"" help:"Token name"`
	ExpiresAt      string                `help:"Optional token expiration in RFC3339 format"`
	IdempotencyKey string                `help:"Reuse this key only when retrying the same issuance"`
}

type RunnerTokensListCmd struct {
	Runner manifest.ResourceName `arg:"" help:"Runner name"`
	cli.ListFlags
}

type RunnerTokensGetCmd struct {
	ID model.AgentIdentityTokenID `arg:"" help:"Token ID"`
}

type RunnerTokensRevokeCmd struct {
	ID model.AgentIdentityTokenID `arg:"" help:"Token ID"`
}

func runnerIdentityID(cfg *commandContext, name manifest.ResourceName) (model.AgentIdentityID, error) {
	api, err := cfg.NewClient()
	if err != nil {
		return "", err
	}
	runner, found, err := api.Runners().Get(cfg.Context, name)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("runner %q not found", name)
	}
	return model.AgentIdentityID(runner.Metadata.UID), nil
}

func (c *RunnerTokensIssueCmd) Run(cfg *commandContext) error {
	var expiresAt *time.Time
	if c.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, c.ExpiresAt)
		if err != nil {
			return fmt.Errorf("expires-at must use RFC3339: %w", err)
		}
		expiresAt = &expiry
	}
	id, err := runnerIdentityID(cfg, c.Runner)
	if err != nil {
		return err
	}
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	ctx := cfg.Context
	if c.IdempotencyKey != "" {
		ctx = identityclient.WithRequestOptions(ctx, identityclient.RequestOptions{IdempotencyKey: c.IdempotencyKey})
	}
	token, err := api.AgentIdentityTokens().Create(ctx, id, model.AgentIdentityToken{Resource: model.Resource{Name: c.Name}, ExpiresAt: expiresAt})
	if err != nil {
		return err
	}
	if cfg.Env.Output.Structured() {
		result, err := resource.EncodeResult(token)
		if err != nil {
			return err
		}
		return cfg.Env.Output.Encode(result)
	}
	if token.Token == "" {
		return fmt.Errorf("token %q already exists; its one-time secret is unavailable", token.ID)
	}
	out := cfg.Env.Output.Stdout
	if out == nil {
		out = os.Stdout
	}
	_, err = fmt.Fprintln(out, token.Token)
	return err
}

func (c *RunnerTokensListCmd) Run(cfg *commandContext) error {
	id, err := runnerIdentityID(cfg, c.Runner)
	if err != nil {
		return err
	}
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	return cli.ListPages(cfg.Context, cfg.Env.Output, c.ListFlags, func(ctx context.Context, q manifest.SearchQuery) ([]runnerTokenView, manifest.Page, error) {
		tokens, page, err := api.AgentIdentityTokens().List(ctx, id, q)
		return views(tokens, func(t model.AgentIdentityToken) runnerTokenView { return runnerTokenView{t} }), page, err
	})
}

func (c *RunnerTokensGetCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	token, found, err := api.AgentIdentityTokens().Get(cfg.Context, c.ID)
	return cli.RenderFound(cfg.Env.Output, runnerTokenView{token}, found, err)
}

func (c *RunnerTokensRevokeCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	token, err := setStatus(cfg.Context, c.ID, "revoked", api.AgentIdentityTokens().Get, api.AgentIdentityTokens().CreateOrUpdate)
	return cli.RenderResource(cfg.Env.Output, runnerTokenView{token}, err)
}

type runnerTokenView struct{ model.AgentIdentityToken }

func (v runnerTokenView) MarshalJSON() ([]byte, error) { return identityJSON(v.AgentIdentityToken) }

func (runnerTokenView) TableHeader(bool) []string {
	return []string{"NAME", "ID", "STATUS", "EXPIRES"}
}

func (v runnerTokenView) TableRow(bool) []any {
	expires := "No expiration"
	if v.ExpiresAt != nil {
		expires = v.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
	}
	return []any{v.Name, v.ID, v.Status, expires}
}
