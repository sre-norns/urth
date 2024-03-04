package urth

import (
	"context"

	"github.com/sre-norns/urth/pkg/bark"
	"github.com/sre-norns/urth/pkg/wyrd"
)

type Store interface {
	Create(ctx context.Context, value any) error
	Get(ctx context.Context, value any, id wyrd.ResourceID) (bool, error)
	Delete(ctx context.Context, value any, id wyrd.VersionedResourceId) (bool, error)
	Update(ctx context.Context, value any, id wyrd.VersionedResourceId) (bool, error)

	GetByToken(ctx context.Context, value any, token ApiToken) (bool, error)
	GetWithVersion(ctx context.Context, dest any, id wyrd.VersionedResourceId) (bool, error)
	FindResources(ctx context.Context, dest any, searchQuery bark.SearchQuery, maxLimit uint) (count uint, err error)
}
