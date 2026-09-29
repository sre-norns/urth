package urth

import (
	"context"
	"errors"
	"net/http"
	"reflect"

	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
)

type scopeKey struct{}

// WithScope binds a resource operation to a resolved account/project. It does
// not grant authority: identity visibility still checks current memberships.
func WithScope(ctx context.Context, scope manifest.ScopeRef) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// RequestScope returns the scope selected for this operation.
func RequestScope(ctx context.Context) manifest.ScopeRef {
	scope, _ := ctx.Value(scopeKey{}).(manifest.ScopeRef)
	return scope
}

// controlContext is reserved for authenticated worker operations and control
// loops. Worker methods must validate their narrower capability themselves.
func controlContext(ctx context.Context) context.Context {
	return identity.WithServicePrincipal(WithScope(ctx, manifest.ScopeRef{}))
}

// RegisterIdentity declares product ownership and runner grant roles before
// identity migration. The product and identity services share one database.
func RegisterIdentity(db *gorm.DB) error {
	return identity.Register(db, identity.Extensions{
		Kinds: map[string]identity.Kind{
			"Runner":          {Table: "runners", IDColumn: "uid", Scope: "account"},
			"WorkerInstance":  {Table: "worker_instances", IDColumn: "uid", Scope: "account"},
			"Scenario":        {Table: "scenarios", IDColumn: "uid", Scope: "project"},
			"Result":          {Table: "results", IDColumn: "uid", Scope: "project"},
			"Artifact":        {Table: "artifacts", IDColumn: "uid", Scope: "project"},
			"DispatchFailure": {Table: "dispatch_failures", IDColumn: "uid", Scope: "project"},
		},
		AuxiliaryTables: []string{"dispatch_outbox"},
		GrantRoles:      []im.RoleType{"runner"},
		Auditor: identity.AuditorFunc(func(ctx context.Context, tx *gorm.DB, a identity.Audit) error {
			if grant, ok := a.Resource.(*im.AgentAuthorization); ok {
				if !identity.AccountAdmin(ctx, tx, grant.AccountID) || !identity.ProjectAdmin(ctx, tx, grant.ProjectID) {
					return identity.Forbidden()
				}
				var runner Runner
				if err := tx.Where("uid = ? AND account_id = ?", grant.AgentID, grant.AccountID).First(&runner).Error; err != nil {
					return identity.Missing()
				}
			}
			return nil
		}),
	})
}

// WithIdentity enables the shared, fail-closed identity policy on the service.
// Production composition always supplies this option. Storage-only unit tests
// can use NewService without an identity database.
func WithIdentity(db *gorm.DB, service *identity.Service) ServiceOption {
	return func(s *serviceImpl) {
		s.identityDB, s.identity = db, service
		s.store = s.store.WithVisibility(scopedVisibility{DB: db})
	}
}

type scopedVisibility struct{ DB *gorm.DB }

func identityError(err error) error {
	var problem *identity.Problem
	if errors.As(err, &problem) {
		return manifest.NewStatusError(problem.Status, problem.Code, problem.Detail)
	}
	return err
}

func (v scopedVisibility) Filter(ctx context.Context, q *gorm.DB, model any) (*gorm.DB, error) {
	scope := RequestScope(ctx)
	if scope.Project != "" && modelName(model) != "Runner" && modelName(model) != "WorkerInstance" {
		if err := identity.Authorize(ctx, v.DB, &Scenario{ObjectMeta: manifest.ObjectMeta{Account: scope.Account, Project: scope.Project}}, false); err != nil {
			return nil, identityError(err)
		}
	}
	authorizationModel := model
	if modelName(model) == "DispatchFailure" && RequestScope(ctx).Account != "" && RequestScope(ctx).Project == "" {
		// Malformed messages without a run belong to the runner's account. An
		// account admin does not gain access to project failure records here.
		authorizationModel = &Runner{}
		q = q.Where("project_id = ''")
	}
	q, err := (identity.Visibility{DB: v.DB}).Filter(ctx, q, authorizationModel)
	if err != nil {
		return nil, identityError(err)
	}
	if scope.Account != "" {
		q = q.Where("account_id = ?", scope.Account)
	}
	if scope.Project != "" && modelName(model) != "Runner" && modelName(model) != "WorkerInstance" {
		q = q.Where("project_id = ?", scope.Project)
	}
	return q, nil
}

func (v scopedVisibility) Admit(ctx context.Context, model any) error {
	if entry, ok := model.(*DispatchOutboxEntry); ok {
		parent := Result{ObjectMeta: manifest.ObjectMeta{Account: entry.AccountID, Project: entry.ProjectID}}
		return v.Admit(ctx, &parent)
	}
	scoped, ok := model.(interface {
		ApplyScope(manifest.ScopeRef) error
		Scope() manifest.ScopeRef
	})
	if !ok {
		return manifest.ErrInvalidScope
	}
	if scope := RequestScope(ctx); !scope.IsZero() {
		if modelName(model) == "Runner" || modelName(model) == "WorkerInstance" {
			scope.Project = ""
		}
		if err := scoped.ApplyScope(scope); err != nil {
			return err
		}
	}
	name := reflect.Indirect(reflect.ValueOf(model)).Type().Name()
	scopeKind := manifest.ScopeProject
	if name == "DispatchFailure" && scoped.Scope().Project == "" {
		if err := scoped.Scope().Validate(manifest.ScopeAccount); err != nil {
			return err
		}
		return identityError(identity.Authorize(ctx, v.DB, &Runner{ObjectMeta: manifest.ObjectMeta{Account: scoped.Scope().Account}}, true))
	}
	if name == "Runner" || name == "WorkerInstance" {
		scopeKind = manifest.ScopeAccount
	}
	if err := scoped.Scope().Validate(scopeKind); err != nil {
		return err
	}
	return identityError(identity.Authorize(ctx, v.DB, model, true))
}

func offsetError(query manifest.SearchQuery) error {
	if query.Offset != 0 {
		return manifest.NewStatusError(http.StatusBadRequest, "offset-unsupported", "Lists page by cursor only.")
	}
	return nil
}

// resourceStore adds cursor pages to the transaction-capable store surface.
type resourceStore interface {
	dbstore.TransactionalStore
	FindPage(context.Context, any, manifest.SearchQuery, ...dbstore.Option) (manifest.Page, error)
}

func modelName(model any) string {
	t := reflect.TypeOf(model)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t.Name()
}

func applyRequestScope(ctx context.Context, meta *manifest.ObjectMeta, kind manifest.Scope) error {
	scope := RequestScope(ctx)
	if scope.IsZero() {
		return nil
	}
	if kind == manifest.ScopeAccount {
		scope.Project = ""
	}
	return meta.ApplyScope(scope)
}
