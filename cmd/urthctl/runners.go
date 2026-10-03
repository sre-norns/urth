package main

import (
	"fmt"

	"github.com/sre-norns/wyrd/identity/cli"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// RunnersCmd groups runner operations.
type RunnersCmd struct {
	Block   RunnerBlockCmd   `cmd:"" help:"Block a verified worker fingerprint"`
	Unblock RunnerUnblockCmd `cmd:"" help:"Unblock a verified worker fingerprint"`
	Token   RunnerTokenCmd   `cmd:"" help:"Issue an enrolment token a runner's workers register with"`
}

// RunnerTokenCmd issues the token a runner's workers start with (RUNNER_TOKEN).
// It replaces `auth-worker`: issuing one is an account administrator's act, so
// it takes a signed-in profile like any other command.
type RunnerTokenCmd struct {
	Name manifest.ResourceName `help:"Name of the runner" arg:"" optional:"" xor:"source"`
	File string                `help:"A runner manifest; its name is used" short:"f" type:"existingfile" xor:"source"`
}

func (c *RunnerTokenCmd) Run(cfg *commandContext) error {
	if c.Name == "" && c.File == "" {
		return fmt.Errorf("name a runner, or a runner manifest with -f")
	}
	if c.File != "" {
		resource, ok, err := manifestFromFile(c.File)
		if err != nil {
			return fmt.Errorf("failed to read content: %w", err)
		} else if !ok {
			return fmt.Errorf("no manifest found in %q", c.File)
		}
		if resource.Kind != urth.KindRunner {
			return fmt.Errorf("file %q defines %q kind, while %q manifest is required", c.File, resource.Kind, urth.KindRunner)
		}
		c.Name = resource.Metadata.Name
	}

	apiClient, err := cfg.NewClient()
	if err != nil {
		return fmt.Errorf("failed to initialize API Client: %w", err)
	}

	ctx, cancel := cfg.ClientCallContext()
	defer cancel()

	token, exist, err := apiClient.Runners().GetToken(ctx, c.Name)
	if err != nil {
		return err
	} else if !exist {
		return fmt.Errorf("runner %q does not exist", c.Name)
	}

	fmt.Println(token)
	return nil
}

type (
	// RunnerAuthorizationsCmd manages which runners may run the project's
	// scenarios. Create or change one with `apply`.
	RunnerAuthorizationsCmd struct {
		List   RunnerAuthorizationsListCmd   `cmd:"" help:"List the project's runner authorizations"`
		Get    RunnerAuthorizationsGetCmd    `cmd:"" help:"Show a runner authorization"`
		Delete RunnerAuthorizationsDeleteCmd `cmd:"" help:"Withdraw a runner's authorization"`
	}
	RunnerAuthorizationsListCmd struct {
		Selector string `help:"Selector (label query) to filter on" optional:"" name:"selector" short:"l"`
	}
	RunnerAuthorizationsGetCmd struct {
		Name manifest.ResourceName `arg:"" help:"Name of the authorization"`
	}
	RunnerAuthorizationsDeleteCmd struct {
		Name manifest.ResourceName `arg:"" help:"Name of the authorization"`
	}
)

func (c *RunnerAuthorizationsListCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return err
	}
	q, err := cli.SearchQuery(c.Selector)
	if err != nil {
		return err
	}
	grants, page, err := collectPages(cfg.Context, q, apiClient.RunnerAuthorizations().List)
	if err != nil {
		return err
	}
	return cli.RenderList(cfg.Env.Output, views(grants, func(m manifest.ResourceManifest) grantView { return grantView{m} }), page)
}

func (c *RunnerAuthorizationsGetCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return err
	}
	grant, found, err := apiClient.RunnerAuthorizations().Get(cfg.Context, c.Name)
	return cli.RenderFound(cfg.Env.Output, grantView{grant}, found, err)
}

// Run deletes the version it read: a grant changed in between is refused
// rather than deleted unseen.
func (c *RunnerAuthorizationsDeleteCmd) Run(cfg *commandContext) error {
	apiClient, err := cfg.NewClient()
	if err != nil {
		return err
	}
	grant, found, err := apiClient.RunnerAuthorizations().Get(cfg.Context, c.Name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("runner authorization %q not found", c.Name)
	}
	// Addressed by UID: the delete route takes the grant's ID where the read
	// and the update take its name.
	deleted, err := apiClient.RunnerAuthorizations().Delete(cfg.Context, manifest.NewVersionedID(grant.Metadata.UID, grant.Metadata.Version))
	if err != nil {
		return err
	}
	if !deleted {
		return fmt.Errorf("runner authorization %q was not found to delete", c.Name)
	}
	fmt.Printf("Deleted runner authorization %q.\n", c.Name)
	return nil
}

type RunnerBlockCmd struct {
	Name        manifest.ResourceName `arg:"" help:"Runner name"`
	Fingerprint string                `arg:"" help:"Verified sha256 worker fingerprint"`
	Reason      string                `help:"Reason for blocking the worker"`
}
type RunnerUnblockCmd struct {
	Name        manifest.ResourceName `arg:"" help:"Runner name"`
	Fingerprint string                `arg:"" help:"Verified sha256 worker fingerprint"`
}

func (c *RunnerBlockCmd) Run(cfg *commandContext) error {
	return editWorkerBlocklist(cfg, c.Name, c.Fingerprint, c.Reason, true)
}
func (c *RunnerUnblockCmd) Run(cfg *commandContext) error {
	return editWorkerBlocklist(cfg, c.Name, c.Fingerprint, "", false)
}
func editWorkerBlocklist(cfg *commandContext, name manifest.ResourceName, fingerprint, reason string, block bool) error {
	api, err := cfg.NewClient()
	if err != nil {
		return err
	}
	ctx, cancel := cfg.ClientCallContext()
	defer cancel()
	entry, found, err := api.Runners().Get(ctx, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("runner %q not found", name)
	}
	runner, err := urth.NewRunner(entry)
	if err != nil {
		return err
	}
	entries := make([]urth.BlockedWorker, 0, len(runner.Spec.BlockedWorkers)+1)
	for _, existing := range runner.Spec.BlockedWorkers {
		if existing.Identity != fingerprint {
			entries = append(entries, existing)
		}
	}
	if block {
		entries = append(entries, urth.BlockedWorker{Identity: fingerprint, Reason: reason})
	}
	runner.Spec.BlockedWorkers = entries
	if _, err := urth.NewRunner(runner.ToManifest()); err != nil {
		return err
	}
	_, err = api.Runners().Update(ctx, entry.Metadata.GetVersionedID(), runner.ToManifest())
	if err == nil {
		fmt.Printf("Updated blocked workers for runner %q.\n", name)
	}
	return err
}
