package urth

import (
	"context"

	"github.com/sre-norns/urth/pkg/wyrd"
)

type Store interface {
	Create(ctx context.Context, value any) error
	Get(ctx context.Context, value any, id wyrd.ResourceID) (bool, error)
	Delete(ctx context.Context, value any, id VersionedResourceId) (bool, error)
	Update(ctx context.Context, value any, id VersionedResourceId) (bool, error)

	GetByToken(ctx context.Context, value any, token ApiToken) (bool, error)
	GetWithVersion(ctx context.Context, dest any, id VersionedResourceId) (bool, error)
	FindResources(ctx context.Context, dest any, searchQuery SearchQuery, maxLimit uint) (count uint, err error)

	// GuessKind(value reflect.Value) (TypeMeta, error)
}
