package apiserver

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
)

// supportedAPIVersion refuses a manifest of an apiVersion Urth does not
// serve, rather than storing it as if it were Urth's. It follows
// bark.ManifestAPI, which parses the body.
func supportedAPIVersion() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		version := bark.RequireManifest(ctx).APIVersion
		if !urth.AcceptsAPIVersion(version) {
			bark.AbortWithError(ctx, http.StatusBadRequest, fmt.Errorf("unsupported apiVersion %q: Urth serves %s", version, urth.APIVersion))
			return
		}
		ctx.Next()
	}
}
