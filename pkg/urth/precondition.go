package urth

import (
	"context"
	"net/http"

	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// ifMatchKey carries the version a request's update was based on: its If-Match.
type ifMatchKey struct{}

// WithIfMatch records the version an update is based on (ADR 0001 §5). The
// update is refused with [bark.ErrPreconditionFailed] unless that is the stored
// version. Route handlers set it from If-Match; `*` sets nothing, which makes
// the update unconditional.
//
// It travels in the context rather than as a parameter because the Service
// interface is also the REST client's, which says the same thing as a header.
func WithIfMatch(ctx context.Context, version manifest.Version) context.Context {
	return context.WithValue(ctx, ifMatchKey{}, version)
}

func ifMatch(ctx context.Context) (manifest.Version, bool) {
	version, ok := ctx.Value(ifMatchKey{}).(manifest.Version)
	return version, ok
}

// expectedVersion is the version an update must find stored: the request's
// If-Match when it named one, otherwise the version just read. Either way the
// write is guarded by it, so two updates of the same version cannot both land.
func expectedVersion(ctx context.Context, read manifest.Version) manifest.Version {
	if version, ok := ifMatch(ctx); ok {
		return version
	}
	return read
}

// staleVersion is the refusal of an update whose version is not the stored one:
// 412 when the request named the version, 409 when it lost a race to a
// concurrent writer after reading. They are bark.ErrorResponse values because
// bark's response helpers keep the status of that type only; any other error
// is answered with their default, 400.
func staleVersion(ctx context.Context) error {
	if _, ok := ifMatch(ctx); ok {
		return bark.NewErrorResponse(http.StatusPreconditionFailed, bark.ErrPreconditionFailed)
	}
	return bark.NewErrorResponse(http.StatusConflict, bark.ErrResourceVersionConflict)
}

// missingForIfMatch refuses to create a resource the request expected to find
// at a version: If-Match names a version, and nothing is stored to match.
func missingForIfMatch(ctx context.Context) error {
	if _, ok := ifMatch(ctx); ok {
		return bark.NewErrorResponse(http.StatusPreconditionFailed, bark.ErrPreconditionFailed)
	}
	return nil
}

// saveResourceAt writes back a resource read at version, only if it is still
// at that version. See saveResource for why whole-resource writes are saves.
func saveResourceAt(ctx context.Context, store resourceSaver, value any, version manifest.Version) error {
	written, err := store.CreateOrUpdate(ctx, value, saveOptions(version)...)
	if err != nil {
		return err
	}
	if !written {
		return staleVersion(ctx)
	}
	return nil
}
