package bark

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/urth/pkg/wyrd"
)

// ResourceRequest represents information to identify a single resource being referd in the path / query
type (
	ResourceRequest struct {
		ID wyrd.ResourceID `uri:"id" form:"id" binding:"required"`
	}

	// VersionQuery is a set of query params for the versioned resounse,
	// such as specific version number of the resource in questions
	VersionQuery struct {
		Version wyrd.Version `uri:"version" form:"version" binding:"required"`
	}

	// CreatedResponse return information about newly created resource
	CreatedResponse struct {
		// Gives us kind info
		wyrd.TypeMeta `json:",inline" yaml:",inline"`

		// Id and version information of the newly created resourse
		wyrd.VersionedResourceId `json:",inline" yaml:",inline"`

		// Semantic actions
		HResponse `form:",inline" json:",inline" yaml:",inline"`
	}
)

func ManifestApi(kind wyrd.Kind) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		manifest := wyrd.ResourceManifest{
			TypeMeta: wyrd.TypeMeta{
				Kind: kind, // Assume correct kind in case of run-triggers with min info
			},
		}
		if err := ctx.ShouldBindWith(&manifest, bindingFor(ctx.Request.Method, ctx.ContentType())); err != nil {
			AbortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		// d, _ := json.MarshalIndent(manifest, "", "  ")
		// os.Stdout.Write(d)

		if manifest.Kind == "" {
			manifest.Kind = kind
		} else if manifest.Kind != kind { // validate that API request is for correct manifest type:
			AbortWithError(ctx, http.StatusBadRequest, ErrWrongApiKind)
			return
		}

		ctx.Set(resourceManifestKey, manifest)
		ctx.Next()
	}
}

func RequireManifest(ctx *gin.Context) wyrd.ResourceManifest {
	return ctx.MustGet(resourceManifestKey).(wyrd.ResourceManifest)
}

// A filter/middleware to add support for resource ID requests
// Used in conjunction with `RequireResourceId`
func ResourceIdApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var resourceRequest ResourceRequest
		if err := ctx.BindUri(&resourceRequest); err != nil {
			AbortWithError(ctx, http.StatusNotFound, err)
			return
		}

		ctx.Set(resourceIdKey, resourceRequest)
		ctx.Next()
	}
}

// Shortcut to get ID of the requested resource from the path
// Used in conjucntion with `ResourceIdApi`
func RequireResourceId(ctx *gin.Context) ResourceRequest {
	return ctx.MustGet(resourceIdKey).(ResourceRequest)
}

func VersionedResourceApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var versionInfo VersionQuery
		if err := ctx.ShouldBindQuery(&versionInfo); err != nil {
			AbortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		if resourceId, ok := ctx.Get(resourceIdKey); ok {
			ctx.Set(versionedIdKey, wyrd.NewVersionedId(resourceId.(ResourceRequest).ID, versionInfo.Version))
		}

		ctx.Set(versionInfoKey, versionInfo)
		ctx.Next()
	}
}

func RequireVersionedResource(ctx *gin.Context) wyrd.VersionedResourceId {
	return ctx.MustGet(versionedIdKey).(wyrd.VersionedResourceId)
}

func RequireVersionedResourceQuery(ctx *gin.Context) VersionQuery {
	return ctx.MustGet(versionInfoKey).(VersionQuery)
}
