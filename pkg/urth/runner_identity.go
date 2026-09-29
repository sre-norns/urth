package urth

import (
	"context"
	"fmt"
	"slices"

	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// createIdentityRunner creates the runner and its machine identity atomically.
// Both have the same UID, so grants and revocable tokens need no lookup alias.
func (m *runnersAPIImpl) createIdentityRunner(ctx context.Context, runner Runner) (Runner, error) {
	if runner.UID != "" {
		return runner, identityError(identity.Invalid("A new runner must not supply a UID."))
	}
	if err := (scopedVisibility{DB: m.identityDB}).Admit(ctx, &runner); err != nil {
		return runner, err
	}
	err := m.identityDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		machine, err := identity.NewService(tx).MachineIdentities().Create(ctx, im.AccountID(runner.Account), im.AgentIdentity{Resource: im.Resource{Name: string(runner.Name)}})
		if err != nil {
			return identityError(err)
		}
		runner.UID = manifest.ResourceID(machine.ID)
		store, err := dbstore.NewDBStore(tx, dbstore.ManifestModel)
		if err != nil {
			return err
		}
		return store.WithVisibility(scopedVisibility{DB: tx}).Create(ctx, &runner, dbstore.Omit(clause.Associations))
	})
	return runner, err
}

func runnerGranted(ctx context.Context, db *gorm.DB, runner Runner, scope manifest.ScopeRef) (bool, error) {
	if scope.Account == "" || scope.Project == "" || runner.Account != scope.Account {
		return false, nil
	}
	// Lifecycle revocation also prevents new claims. Hold these rows through
	// the claim transaction alongside the grant row.
	for _, parent := range []struct {
		value any
		id    string
	}{{&im.Account{}, string(scope.Account)}, {&im.Project{}, string(scope.Project)}, {&im.AgentIdentity{}, string(runner.UID)}} {
		err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = 'active'", parent.id).First(parent.value).Error
		if err == gorm.ErrRecordNotFound {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	var grants []im.AgentAuthorization
	err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("account_id = ? AND project_id = ? AND agent_id = ? AND status = 'active'", scope.Account, scope.Project, runner.UID).Find(&grants).Error
	if err != nil {
		return false, err
	}
	for _, grant := range grants {
		if slices.Contains(grant.Roles, im.RoleType("runner")) {
			return true, nil
		}
	}
	return false, nil
}

func (m *runnersAPIImpl) machineToken(ctx context.Context, runner Runner) (APIToken, bool, error) {
	token, err := m.identity.MachineTokens().Create(ctx, im.AgentIdentityID(runner.UID), im.AgentIdentityToken{Resource: im.Resource{Name: fmt.Sprintf("%s-%s", runner.Name, NewRandToken(8))}})
	return APIToken(token.Token), true, identityError(err)
}

func (m *runnersAPIImpl) machineRunner(ctx context.Context, token APIToken) (manifest.ResourceID, error) {
	principal, err := m.identity.Authenticate(ctx, string(token))
	if err != nil || principal.Type != "agent" {
		return "", bark.ErrResourceUnauthorized
	}
	return manifest.ResourceID(principal.AgentID), nil
}
