package urth

import (
	"context"
	"reflect"
)

type Store interface {
	Create(ctx context.Context, value any) (TypeMeta, error)
	Get(ctx context.Context, value any, id ResourceID) (bool, error)
	Delete(ctx context.Context, value any, id VersionedResourceId) (bool, error)
	Update(ctx context.Context, value any, id VersionedResourceId) (bool, error)

	GetByToken(ctx context.Context, value any, token ApiToken) (bool, error)
	GetWithVersion(ctx context.Context, dest any, id VersionedResourceId) (bool, error)
	FindResources(ctx context.Context, dest any, searchQuery SearchQuery) (TypeMeta, error)

	GuessKind(value reflect.Value) (TypeMeta, error)
}
