package apiserver

import (
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity"
	"github.com/sre-norns/wyrd/pkg/bark"
	"net/http"
)

// Authenticate before parsing a manifest or issuing a database challenge.
func workerEnrollmentGate(service *identity.Service) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ctx.Header("Cache-Control", "no-store")
		if service == nil {
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		principal, err := service.Authenticate(ctx.Request.Context(), bark.RequireBearerToken(ctx))
		if err != nil {
			if problem, ok := errors.AsType[*identity.Problem](err); ok && problem.Status < 500 {
				ctx.AbortWithStatus(http.StatusUnauthorized)
			} else {
				ctx.AbortWithStatus(http.StatusServiceUnavailable)
			}
			return
		}
		if principal.Type != "agent" {
			ctx.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, 64*1024)
		ctx.Next()
	}
}
func workerRegistrationStatus(err error) int {
	if problem, ok := errors.AsType[*bark.ErrorResponse](err); ok {
		return problem.Code
	}
	if disposition, ok := urth.ClaimDispositionOf(err); ok && disposition == urth.ClaimUnavailable {
		return http.StatusServiceUnavailable
	}
	return http.StatusUnauthorized
}

func workerEnrollmentNoStore() gin.HandlerFunc {
	return func(ctx *gin.Context) { ctx.Header("Cache-Control", "no-store"); ctx.Next() }
}
