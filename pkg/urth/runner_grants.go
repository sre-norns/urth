package urth

import (
	"context"
	"fmt"

	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

const KindRunnerAuthorization manifest.Kind = "runner-authorizations"

// RunnerAuthorizationSpec binds account infrastructure to a project.
type RunnerAuthorizationSpec struct {
	RunnerRef manifest.ResourceID `json:"runnerRef" yaml:"runnerRef"`
	Roles     []im.RoleType       `json:"roles" yaml:"roles"`
}

type RunnerAuthorizationStatus struct {
	Phase string `json:"phase" yaml:"phase"`
}

func init() {
	manifest.MustRegisterManifest(KindRunnerAuthorization, &RunnerAuthorizationSpec{}, &RunnerAuthorizationStatus{}, manifest.WithScope(manifest.ScopeProject))
}

type RunnerAuthorizationsAPI interface {
	ReadableResourceAPI[manifest.ResourceManifest]
	ManageableResourceAPI
}

type runnerGrantsAPI struct {
	db       *gorm.DB
	identity *identity.Service
}

func grantManifest(g im.AgentAuthorization) manifest.ResourceManifest {
	return manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{APIVersion: APIVersion, Kind: KindRunnerAuthorization},
		Metadata: manifest.ObjectMeta{UID: manifest.ResourceID(g.ID), Name: manifest.ResourceName(g.Name), Account: manifest.ResourceID(g.AccountID), Project: manifest.ResourceID(g.ProjectID), Version: manifest.Version(g.Revision), Labels: g.Labels},
		Spec:     &RunnerAuthorizationSpec{RunnerRef: manifest.ResourceID(g.AgentID), Roles: g.Roles},
		Status:   &RunnerAuthorizationStatus{Phase: g.Status},
	}
}

func (g *runnerGrantsAPI) scoped(ctx context.Context, write bool) (manifest.ScopeRef, error) {
	scope := RequestScope(ctx)
	if g.identity == nil || g.db == nil {
		return scope, fmt.Errorf("identity is not configured")
	}
	if err := scope.Validate(manifest.ScopeProject); err != nil {
		return scope, err
	}
	if identity.Principal(ctx).AccountID != im.AccountID(scope.Account) || !identity.ProjectAdmin(ctx, g.db, im.ProjectID(scope.Project)) {
		return scope, identityError(identity.Missing())
	}
	if write && !identity.AccountAdmin(ctx, g.db, im.AccountID(scope.Account)) {
		return scope, identityError(identity.Forbidden())
	}
	return scope, nil
}

func (g *runnerGrantsAPI) List(ctx context.Context, q manifest.SearchQuery) ([]manifest.ResourceManifest, manifest.Page, error) {
	scope, err := g.scoped(ctx, false)
	if err != nil {
		return nil, manifest.Page{}, err
	}
	if err := offsetError(q); err != nil {
		return nil, manifest.Page{}, err
	}
	rows, page, err := g.identity.MachineGrants().List(ctx, im.ProjectID(scope.Project), q)
	items := make([]manifest.ResourceManifest, 0, len(rows))
	for _, row := range rows {
		items = append(items, grantManifest(row))
	}
	return items, page, identityError(err)
}

func (g *runnerGrantsAPI) Get(ctx context.Context, name manifest.ResourceName) (manifest.ResourceManifest, bool, error) {
	scope, err := g.scoped(ctx, false)
	if err != nil {
		return manifest.ResourceManifest{}, false, err
	}
	var row im.AgentAuthorization
	err = g.db.WithContext(ctx).Where("account_id = ? AND project_id = ? AND name = ?", scope.Account, scope.Project, name).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return manifest.ResourceManifest{}, false, nil
	}
	return grantManifest(row), err == nil, err
}

func (g *runnerGrantsAPI) write(ctx context.Context, body manifest.ResourceManifest, id *manifest.VersionedResourceID) (manifest.ResourceManifest, error) {
	scope, err := g.scoped(ctx, true)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}
	if err = body.Metadata.ApplyScope(scope); err != nil {
		return manifest.ResourceManifest{}, err
	}
	parsed, err := manifest.ManifestAsStatefulResource[RunnerAuthorizationSpec, RunnerAuthorizationStatus](body)
	if err != nil {
		return manifest.ResourceManifest{}, err
	}
	if len(parsed.Spec.Roles) != 1 || parsed.Spec.Roles[0] != "runner" {
		return manifest.ResourceManifest{}, identityError(identity.Invalid("A runner grant must specify roles: [runner]."))
	}
	var runner Runner
	if err = g.db.WithContext(ctx).Where("uid = ? AND account_id = ?", parsed.Spec.RunnerRef, scope.Account).First(&runner).Error; err != nil {
		return manifest.ResourceManifest{}, identityError(identity.Missing())
	}
	row := im.AgentAuthorization{Resource: im.Resource{Name: string(body.Metadata.Name), AccountID: im.AccountID(scope.Account), ProjectID: im.ProjectID(scope.Project), Labels: body.Metadata.Labels}, AgentID: im.AgentIdentityID(runner.UID), Roles: parsed.Spec.Roles}
	if id == nil {
		if body.Metadata.UID != "" {
			return manifest.ResourceManifest{}, identityError(identity.Invalid("A new runner grant must not supply a UID."))
		}
		row, err = g.identity.MachineGrants().Create(ctx, im.ProjectID(scope.Project), row)
	} else {
		var existing im.AgentAuthorization
		if err = g.db.WithContext(ctx).Where("id = ? AND account_id = ? AND project_id = ?", id.ID, scope.Account, scope.Project).First(&existing).Error; err != nil {
			return manifest.ResourceManifest{}, identityError(identity.Missing())
		}
		if existing.Name != row.Name || existing.AgentID != row.AgentID {
			return manifest.ResourceManifest{}, identityError(identity.Invalid("Grant identity is immutable."))
		}
		row.ID = string(id.ID)
		row.Revision = int64(id.Version)
		row.Status = parsed.Status.Phase
		if row.Status == "" {
			row.Status = existing.Status
		}
		ctx = identity.WithRequest(ctx, identity.Request{IfMatch: identity.ETag(int64(id.Version))})
		row, _, err = g.identity.MachineGrants().CreateOrUpdate(ctx, row)
	}
	return grantManifest(row), identityError(err)
}
func (g *runnerGrantsAPI) Create(ctx context.Context, body manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return g.write(ctx, body, nil)
}
func (g *runnerGrantsAPI) Update(ctx context.Context, id manifest.VersionedResourceID, body manifest.ResourceManifest) (manifest.ResourceManifest, error) {
	return g.write(ctx, body, &id)
}
func (g *runnerGrantsAPI) CreateOrUpdate(ctx context.Context, body manifest.ResourceManifest) (manifest.ResourceManifest, bool, error) {
	existing, found, err := g.Get(ctx, body.Metadata.Name)
	if err != nil {
		return manifest.ResourceManifest{}, false, err
	}
	if !found {
		if err := missingForIfMatch(ctx); err != nil {
			return manifest.ResourceManifest{}, false, err
		}
		result, err := g.Create(ctx, body)
		return result, true, err
	}
	// identity checks the version inside its mutation transaction.
	id := existing.Metadata.GetVersionedID()
	id.Version = expectedVersion(ctx, id.Version)
	result, err := g.Update(ctx, id, body)
	return result, false, err
}
func (g *runnerGrantsAPI) Delete(ctx context.Context, id manifest.VersionedResourceID) (bool, error) {
	scope, err := g.scoped(ctx, true)
	if err != nil {
		return false, err
	}
	var row im.AgentAuthorization
	err = g.db.WithContext(ctx).Where("id = ? AND account_id = ? AND project_id = ?", id.ID, scope.Account, scope.Project).First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	body := grantManifest(row)
	body.Status = &RunnerAuthorizationStatus{Phase: "revoked"}
	_, err = g.Update(ctx, id, body)
	return err == nil, err
}
