package apiserver

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Optimistic concurrency for resource edits (ADR 0001 §5). A PUT must carry
// If-Match: the version it was based on, or `*` for an unconditional write;
// bark.RequireIfMatch answers 428 without one. A stale version is refused with
// 412 and changes nothing -- the write itself is guarded, so two edits of the
// same version cannot both land. A PUT naming a version of a resource that does
// not exist is refused too; `*` may create it.
//
// Deletes keep their ?version= guard, and pausing a worker takes none: a
// worker rewrites its own spec on every registration, and a pause should not
// fail because it re-registered a moment earlier (see SetPaused).

// conditional is the request context carrying the PUT's If-Match version, for
// the service to guard the update with.
func conditional(ctx *gin.Context) context.Context {
	if version, ok := bark.IfMatchVersion(ctx); ok {
		return urth.WithIfMatch(ctx.Request.Context(), version)
	}
	return ctx.Request.Context()
}

// written sets the ETag of a resource a PUT created or updated, so a client can
// edit it again without reading it first.
func written(ctx *gin.Context) func(manifest.ResourceManifest, bool, error) (manifest.ResourceManifest, bool, error) {
	return func(result manifest.ResourceManifest, created bool, err error) (manifest.ResourceManifest, bool, error) {
		if err == nil && result.Metadata.Version != 0 {
			bark.SetETag(ctx, result.Metadata.Version)
		}
		return result, created, err
	}
}

// created sets the ETag of a resource a POST created.
func created(ctx *gin.Context) func(manifest.ResourceManifest, error) (manifest.ResourceManifest, error) {
	return func(result manifest.ResourceManifest, err error) (manifest.ResourceManifest, error) {
		if err == nil && result.Metadata.Version != 0 {
			bark.SetETag(ctx, result.Metadata.Version)
		}
		return result, err
	}
}
