package apiserver

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Product authentication deliberately does not start identity's HTTP mutation
// transaction. Product writes own their transaction (including run + outbox).
// Mount's identity routes retain the module's transactional HTTP middleware.
func userAuthentication(service *identity.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, ok := strings.CutPrefix(ctx.GetHeader("Authorization"), "Bearer ")
		if !ok || service == nil {
			bark.AbortWithProblem(ctx, http.StatusInternalServerError, manifest.NewStatusError(401, "unauthenticated", "A user bearer credential is required."))
			return
		}
		p, err := service.Authenticate(ctx.Request.Context(), token)
		if err != nil || p.Type != "user" || p.Scope != im.ScopeAccount {
			bark.AbortWithProblem(ctx, http.StatusInternalServerError, manifest.NewStatusError(401, "unauthenticated", "An account user session is required."))
			return
		}
		ctx.Request = ctx.Request.WithContext(identity.WithPrincipal(ctx.Request.Context(), p))
		ctx.Next()
	}
}

func requestScope(service *identity.Service, project bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		principal := identity.Principal(ctx.Request.Context())
		scope := manifest.ScopeRef{Account: manifest.ResourceID(principal.AccountID)}
		if project {
			p, found, err := service.Projects().Get(ctx.Request.Context(), im.ProjectID(ctx.Param("id")))
			if err != nil || !found || p.AccountID != principal.AccountID {
				bark.AbortWithProblem(ctx, http.StatusInternalServerError, manifest.NewStatusError(http.StatusNotFound, "not-found", "Project not found."))
				return
			}
			scope.Project = manifest.ResourceID(p.ID)
		} else if ctx.Param("id") != string(principal.AccountID) {
			bark.AbortWithProblem(ctx, http.StatusInternalServerError, manifest.NewStatusError(http.StatusNotFound, "not-found", "Account not found."))
			return
		}
		// Gin shares the outer :id wildcard with identity.Mount. Product
		// resource paths use :resource; bark's resource middleware expects :id.
		for i := range ctx.Params {
			if ctx.Params[i].Key == "id" {
				ctx.Params[i].Value = ctx.Param("resource")
				break
			}
		}
		ctx.Request = ctx.Request.WithContext(urth.WithScope(ctx.Request.Context(), scope))
		ctx.Next()
	}
}
