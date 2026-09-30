package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sre-norns/wyrd/identity/cli"
	identityclient "github.com/sre-norns/wyrd/identity/client"
	"github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Accounts, projects, members, invitations and sessions are the identity
// routes every product mounts; these commands are their urthctl face, over the
// shared identity client.

func (c *commandContext) identity() (*identityclient.Client, error) {
	return identityclient.New(c.APIServerAddress, identityclient.Config{HTTPClient: c.HTTPClient, Token: string(c.Token), Timeout: c.Timeout})
}

func (c *commandContext) account() (model.AccountID, error) {
	if c.Account == "" {
		return "", fmt.Errorf("an account is required: sign in with `urthctl auth login`, or pass --account")
	}
	return model.AccountID(c.Account), nil
}

func (c *commandContext) project() (model.ProjectID, error) {
	if c.Project == "" {
		return "", fmt.Errorf("a project is required: set one with `urthctl context use PROJECT`, or pass --project")
	}
	return model.ProjectID(c.Project), nil
}

// setStatus moves an identity resource to status, the way the identity routes
// retire and restore things: a revision-guarded PATCH of the status alone.
func setStatus[T any, ID ~string](ctx context.Context, id ID, status string,
	get func(context.Context, ID) (T, bool, error),
	update func(context.Context, T) (T, bool, error)) (T, error) {
	value, found, err := get(ctx, id)
	if err != nil {
		return value, err
	}
	if !found {
		return value, fmt.Errorf("%q not found", id)
	}
	encoded, _ := json.Marshal(status)
	ctx = identityclient.WithRequestOptions(ctx, identityclient.RequestOptions{Patch: map[string]json.RawMessage{"status": encoded}})
	value, _, err = update(ctx, value)
	return value, err
}

type (
	AccountsCmd struct {
		List AccountsListCmd `cmd:"" help:"List the accounts you belong to"`
		Get  AccountsGetCmd  `cmd:"" help:"Show an account"`
	}
	AccountsListCmd struct{ cli.ListFlags }
	AccountsGetCmd  struct {
		ID string `arg:"" optional:"" help:"Account ID; the profile's account if omitted"`
	}
)

func (c *AccountsListCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	return cli.ListPages(cfg.Context, cfg.Env.Output, c.ListFlags, api.PersonalProfile().AccessibleAccounts)
}

func (c *AccountsGetCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	id := model.AccountID(c.ID)
	if id == "" {
		if id, err = cfg.account(); err != nil {
			return err
		}
	}
	account, found, err := api.Accounts().Get(cfg.Context, id)
	return cli.RenderFound(cfg.Env.Output, account, found, err)
}

type (
	ProjectsCmd struct {
		List   ProjectsListCmd   `cmd:"" help:"List the account's projects"`
		Get    ProjectsGetCmd    `cmd:"" help:"Show a project"`
		Create ProjectsCreateCmd `cmd:"" help:"Create a project in the account"`
	}
	ProjectsListCmd struct{ cli.ListFlags }
	ProjectsGetCmd  struct {
		ID string `arg:"" optional:"" help:"Project ID; the context's project if omitted"`
	}
	ProjectsCreateCmd struct {
		Name        string `arg:"" help:"Project name"`
		Description string `help:"What the project monitors"`
		Use         bool   `help:"Make the new project the profile's context"`
	}
)

type projectView struct{ model.Project }

func (v projectView) MarshalJSON() ([]byte, error) { return json.Marshal(v.Project) }

func (projectView) TableHeader(bool) []string {
	return []string{"NAME", "ID", "STATUS", "DESCRIPTION", "AGE"}
}

func (v projectView) TableRow(bool) []any {
	return []any{v.Name, v.ID, v.Status, orDash(v.Description), cli.HumanizeAge(v.UpdatedAt)}
}

func (c *ProjectsListCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	account, err := cfg.account()
	if err != nil {
		return err
	}
	return cli.ListPages(cfg.Context, cfg.Env.Output, c.ListFlags, func(ctx context.Context, q manifest.SearchQuery) ([]projectView, manifest.Page, error) {
		projects, page, err := api.Projects().ListForAccount(ctx, account, q)
		return views(projects, func(p model.Project) projectView { return projectView{p} }), page, err
	})
}

func (c *ProjectsGetCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	id := model.ProjectID(c.ID)
	if id == "" {
		if id, err = cfg.project(); err != nil {
			return err
		}
	}
	project, found, err := api.Projects().Get(cfg.Context, id)
	return cli.RenderFound(cfg.Env.Output, projectView{project}, found, err)
}

func (c *ProjectsCreateCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	account, err := cfg.account()
	if err != nil {
		return err
	}
	project, err := api.Projects().Create(cfg.Context, model.Project{Resource: model.Resource{AccountID: account, Name: c.Name}, Description: c.Description})
	if err != nil {
		return err
	}
	if c.Use {
		if err = (&cli.ContextUseCmd{Project: project.ID}).Run(cfg.Env); err != nil {
			return err
		}
	}
	return cli.RenderUpsert(cfg.Env.Output, projectView{project}, true, nil)
}

type (
	// MembersCmd manages who may work in the context's project.
	MembersCmd struct {
		List   MembersListCmd   `cmd:"" help:"List the project's members"`
		Add    MembersAddCmd    `cmd:"" help:"Add a member of the account to the project"`
		Remove MembersRemoveCmd `cmd:"" help:"Remove a member from the project"`
	}
	MembersListCmd struct{ cli.ListFlags }
	MembersAddCmd  struct {
		Email string `arg:"" help:"Email of an account member"`
	}
	MembersRemoveCmd struct {
		Member string `arg:"" help:"Email or membership ID"`
	}
)

type memberView struct{ model.ProjectMembership }

func (v memberView) MarshalJSON() ([]byte, error) { return json.Marshal(v.ProjectMembership) }

func (memberView) TableHeader(wide bool) []string {
	header := []string{"ID", "EMAIL", "NAME", "STATUS", "AGE"}
	if wide {
		header = append(header, "USER")
	}
	return header
}

func (v memberView) TableRow(wide bool) []any {
	name := "-"
	if v.DisplayName != nil && *v.DisplayName != "" {
		name = *v.DisplayName
	}
	row := []any{v.ID, orDash(v.Email), name, v.Status, cli.HumanizeAge(v.UpdatedAt)}
	if wide {
		row = append(row, v.UserID)
	}
	return row
}

func (c *MembersListCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	project, err := cfg.project()
	if err != nil {
		return err
	}
	return cli.ListPages(cfg.Context, cfg.Env.Output, c.ListFlags, func(ctx context.Context, q manifest.SearchQuery) ([]memberView, manifest.Page, error) {
		members, page, err := api.ProjectMemberships().List(ctx, project, q)
		return views(members, func(m model.ProjectMembership) memberView { return memberView{m} }), page, err
	})
}

// Run finds the person among the account's members: a project member must
// be one, and people are known by email, not user ID. Someone whose project
// access was removed has a membership already; it is restored, not duplicated.
func (c *MembersAddCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	project, err := cfg.project()
	if err != nil {
		return err
	}
	candidates, _, err := api.Directory().ProjectMemberCandidates(cfg.Context, project, c.Email, manifest.SearchQuery{Limit: 20})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		if !strings.EqualFold(candidate.Email, c.Email) {
			continue
		}
		switch candidate.ProjectMembershipStatus {
		case "":
			membership, err := api.ProjectMemberships().Create(cfg.Context, project, model.ProjectMembership{UserID: candidate.UserID})
			return cli.RenderUpsert(cfg.Env.Output, memberView{membership}, true, err)
		case "active":
			return fmt.Errorf("%s is already a member of the project", c.Email)
		}
		members, err := identityclient.Collect(cfg.Context, manifest.SearchQuery{}, func(ctx context.Context, q manifest.SearchQuery) ([]model.ProjectMembership, manifest.Page, error) {
			return api.ProjectMemberships().List(ctx, project, q)
		})
		if err != nil {
			return err
		}
		for _, m := range members {
			if m.UserID == candidate.UserID {
				membership, err := setStatus(cfg.Context, model.ProjectMembershipID(m.ID), "active", api.ProjectMemberships().Get, api.ProjectMemberships().CreateOrUpdate)
				return cli.RenderUpsert(cfg.Env.Output, memberView{membership}, false, err)
			}
		}
		// Project memberships are visible to the project's active members
		// only, so an administrator who removed themselves cannot see theirs.
		return fmt.Errorf("%s's project access is %s, and only an active member of the project can restore it", c.Email, candidate.ProjectMembershipStatus)
	}
	return fmt.Errorf("%s is not a member of the account; invite them first with `urthctl invitations create`", c.Email)
}

func (c *MembersRemoveCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	project, err := cfg.project()
	if err != nil {
		return err
	}
	id := model.ProjectMembershipID(c.Member)
	if strings.Contains(c.Member, "@") {
		members, err := identityclient.Collect(cfg.Context, manifest.SearchQuery{}, func(ctx context.Context, q manifest.SearchQuery) ([]model.ProjectMembership, manifest.Page, error) {
			return api.ProjectMemberships().List(ctx, project, q)
		})
		if err != nil {
			return err
		}
		id = ""
		for _, m := range members {
			if strings.EqualFold(m.Email, c.Member) && m.Status == "active" {
				id = model.ProjectMembershipID(m.ID)
			}
		}
		if id == "" {
			return fmt.Errorf("%s is not an active member of the project", c.Member)
		}
	}
	membership, err := setStatus(cfg.Context, id, "inactive", api.ProjectMemberships().Get, api.ProjectMemberships().CreateOrUpdate)
	return cli.RenderResource(cfg.Env.Output, memberView{membership}, err)
}

type (
	// InvitationsCmd invites people to the account.
	InvitationsCmd struct {
		List   InvitationsListCmd   `cmd:"" help:"List the account's invitations"`
		Create InvitationsCreateCmd `cmd:"" help:"Invite someone to the account"`
		Revoke InvitationsRevokeCmd `cmd:"" help:"Revoke a pending invitation"`
	}
	InvitationsListCmd   struct{ cli.ListFlags }
	InvitationsCreateCmd struct {
		Email    string `arg:"" help:"Email to invite"`
		Role     string `help:"Account role" enum:"member,admin,owner" default:"member"`
		Delivery string `help:"email: the server mails the link; manual: print it here to pass on yourself" enum:"email,manual" default:"email"`
	}
	InvitationsRevokeCmd struct {
		ID string `arg:"" help:"Invitation ID"`
	}
)

type invitationView struct{ model.AccountInvitation }

func (v invitationView) MarshalJSON() ([]byte, error) { return json.Marshal(v.AccountInvitation) }

func (invitationView) TableHeader(wide bool) []string {
	header := []string{"ID", "EMAIL", "ROLE", "STATUS", "EXPIRES"}
	if wide {
		header = append(header, "DELIVERY", "AGE")
	}
	return header
}

func (v invitationView) TableRow(wide bool) []any {
	row := []any{v.ID, v.Email, v.Role, v.Status, v.ExpiresAt.Local().Format("2006-01-02 15:04")}
	if wide {
		row = append(row, orDash(v.Delivery), cli.HumanizeAge(v.CreatedAt))
	}
	return row
}

func (c *InvitationsListCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	account, err := cfg.account()
	if err != nil {
		return err
	}
	return cli.ListPages(cfg.Context, cfg.Env.Output, c.ListFlags, func(ctx context.Context, q manifest.SearchQuery) ([]invitationView, manifest.Page, error) {
		invitations, page, err := api.AccountInvitations().List(ctx, account, q)
		return views(invitations, func(i model.AccountInvitation) invitationView { return invitationView{i} }), page, err
	})
}

func (c *InvitationsCreateCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	account, err := cfg.account()
	if err != nil {
		return err
	}
	invitation, err := api.AccountInvitations().Create(cfg.Context, account, model.AccountInvitation{Email: c.Email, Role: c.Role, Delivery: c.Delivery})
	if err != nil {
		return err
	}
	if invitation.Token != "" && !cfg.Env.Output.Structured() {
		// The token is shown once; a manual invitation is useless without it.
		fmt.Fprintf(os.Stderr, "Invitation token (shown once): %s\n", invitation.Token)
	}
	return cli.RenderUpsert(cfg.Env.Output, invitationView{invitation}, true, nil)
}

func (c *InvitationsRevokeCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	invitation, err := setStatus(cfg.Context, model.AccountInvitationID(c.ID), "revoked", api.AccountInvitations().Get, api.AccountInvitations().CreateOrUpdate)
	return cli.RenderResource(cfg.Env.Output, invitationView{invitation}, err)
}

type (
	// SessionsCmd shows where you are signed in, and ends sessions.
	SessionsCmd struct {
		List   SessionsListCmd   `cmd:"" help:"List your sessions"`
		Revoke SessionsRevokeCmd `cmd:"" help:"End a session"`
	}
	SessionsListCmd   struct{ cli.ListFlags }
	SessionsRevokeCmd struct {
		ID string `arg:"" help:"Session ID"`
	}
)

type sessionView struct {
	model.Session
	current bool
}

func (v sessionView) MarshalJSON() ([]byte, error) { return json.Marshal(v.Session) }

func (sessionView) TableHeader(wide bool) []string {
	header := []string{"ID", "CURRENT", "CLIENT", "METHOD", "STATUS", "EXPIRES"}
	if wide {
		header = append(header, "SCOPE", "ORIGIN", "IP", "SIGNED IN")
	}
	return header
}

func (v sessionView) TableRow(wide bool) []any {
	current := ""
	if v.current {
		current = "*"
	}
	row := []any{v.ID, current, v.ClientID, v.AuthenticationMethod, v.Status, v.RefreshExpiresAt.Local().Format("2006-01-02 15:04")}
	if wide {
		row = append(row, v.Scope, orDash(v.Origin), orDash(v.IPAddress), cli.HumanizeAge(v.AuthenticatedAt))
	}
	return row
}

func (c *SessionsListCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	// The session this invocation runs on, so it can be told apart.
	principal, _, err := api.Principal().Get(cfg.Context)
	if err != nil {
		return err
	}
	return cli.ListPages(cfg.Context, cfg.Env.Output, c.ListFlags, func(ctx context.Context, q manifest.SearchQuery) ([]sessionView, manifest.Page, error) {
		sessions, page, err := api.Sessions().List(ctx, q)
		return views(sessions, func(s model.Session) sessionView {
			return sessionView{Session: s, current: s.ID == principal.CredentialID}
		}), page, err
	})
}

func (c *SessionsRevokeCmd) Run(cfg *commandContext) error {
	api, err := cfg.identity()
	if err != nil {
		return err
	}
	session, err := setStatus(cfg.Context, model.SessionID(c.ID), "revoked", api.Sessions().Get, api.Sessions().CreateOrUpdate)
	return cli.RenderResource(cfg.Env.Output, sessionView{Session: session}, err)
}
