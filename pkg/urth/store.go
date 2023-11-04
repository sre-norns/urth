package urth

import "context"

type Store interface {
	Create(ctx context.Context, value any) error
	Get(ctx context.Context, value any, id ResourceID) (bool, error)
	Delete(ctx context.Context, value any, id ResourceID) (bool, error)
	//FIXME: Update must accept versionedId
	Update(ctx context.Context, value any, id ResourceID) (bool, error)

	GetWithVersion(ctx context.Context, dest any, id VersionedResourceId) (bool, error)
	FindResources(ctx context.Context, dest any, searchQuery SearchQuery) (TypeMeta, error)
	FindInto(ctx context.Context, model, into any, pagination Pagination) error

	GuessKind(value any) (TypeMeta, error)
}
