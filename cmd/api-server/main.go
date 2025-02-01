package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"reflect"

	"github.com/alecthomas/kong"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	dlogger "gorm.io/gorm/logger"
)

const (
	paginationLimit = 512
	kindRequestKey  = "request_kind"
)

var kindMap = map[string]manifest.Kind{
	string(urth.KindScenario): urth.KindScenario,
	string(urth.KindRunner):   urth.KindRunner,
	string(urth.KindResult):   urth.KindResult,
	string(urth.KindArtifact): urth.KindArtifact,
}

type KindRequest struct {
	Kind string `uri:"kind" form:"kind" binding:"required"`
}

func KindAPI() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request KindRequest

		if err := ctx.BindUri(&request); err != nil {
			bark.AbortWithError(ctx, http.StatusNotFound, err)
			return
		}

		kind, ok := kindMap[request.Kind]
		if !ok {
			bark.AbortWithError(ctx, http.StatusNotFound, manifest.ErrUnknownKind)
			return
		}

		ctx.Set(kindRequestKey, kind)
		ctx.Next()
	}
}

func RequireKind(ctx *gin.Context) manifest.Kind {
	return ctx.MustGet(kindRequestKey).(manifest.Kind)
}

// TODO: JWT Auth middleware for runners!

func apiRoutes(srv urth.Service) *gin.Engine {
	router := gin.Default()
	router.UseRawPath = true

	// Simple group: v1
	v1 := router.Group("api/v1", bark.ContentTypeAPI())
	{
		v1.GET("/version", func(ctx *gin.Context) {
			bark.Ok(ctx, bark.NewVersionResponse())
		})

		v1.GET("/search/:kind/names", KindAPI(), bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			results, total, err := srv.GetLabels(RequireKind(ctx)).ListNames(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results.Slice(), total)
		})

		// Support search by listing all possible labels
		v1.GET("/search/:kind/labels", KindAPI(), bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			results, total, err := srv.GetLabels(RequireKind(ctx)).ListLabels(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results.Slice(), total)
		})
		// Support search by listing all values of a given label
		v1.GET("/search/:kind/labels/:id", KindAPI(), bark.ResourceAPI(), bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			results, total, err := srv.GetLabels(RequireKind(ctx)).ListLabelValues(ctx.Request.Context(), string(bark.RequireResourceID(ctx)), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results.Slice(), total)
		})

		// Auth API for runners. TODO: Should it be something like `/auth/runner` ?
		v1.PUT("/runners", bark.AuthBearerAPI(), bark.ManifestAPI(urth.KindWorkerInstance), func(ctx *gin.Context) {
			token := bark.RequireBearerToken(ctx)
			result, err := srv.GetRunnerAPI().Auth(ctx.Request.Context(), urth.ApiToken(token), bark.RequireManifest(ctx))

			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
			bark.MaybeResourceCreated(ctx, result, err)
		})

		//------------
		// Runners API
		//------------
		v1.GET("/runners", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			results, total, err := srv.GetRunnerAPI().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results, total)
		})
		v1.POST("/runners", bark.ManifestAPI(urth.KindRunner), func(ctx *gin.Context) {
			result, err := srv.GetRunnerAPI().Create(ctx.Request.Context(), bark.RequireManifest(ctx))
			bark.MaybeResourceCreated(ctx, result.ToManifest(), err)
		})

		v1.POST("/runners/:id", bark.ResourceAPI(), bark.ManifestAPI(urth.KindRunner), func(ctx *gin.Context) {
			resource, created, err := srv.GetRunnerAPI().CreateOrUpdate(ctx.Request.Context(), bark.RequireManifest(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			if created {
				bark.ReplyResourceCreated(ctx, "", resource.ToManifest())
			} else {
				bark.Ok(ctx, resource.ToManifest())
			}
		})
		v1.GET("/runners/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.GetRunnerAPI().Get(ctx.Request.Context(), bark.RequireResourceName(ctx))
			bark.MaybeManifest(ctx, resource.ToManifest(), exists, err)
		})
		// Update existing Runner info
		v1.PUT("/runners/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), bark.ManifestAPI(urth.KindRunner), func(ctx *gin.Context) {
			versionedId := bark.RequireVersionedResource(ctx)
			newEntry := bark.RequireManifest(ctx)

			updateResponse, err := srv.GetRunnerAPI().Update(ctx.Request.Context(), versionedId, newEntry)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			bark.MarshalResponse(ctx, http.StatusCreated, updateResponse)
		})

		v1.DELETE("/runners/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			versionedId := bark.RequireVersionedResource(ctx)

			// Note: Should Delete be is silent? - no error if deleting non-existing resource
			existed, err := srv.GetRunnerAPI().Delete(ctx.Request.Context(), versionedId)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !existed {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			ctx.Status(http.StatusNoContent)
		})
		//------------
		// Scenarios API
		//------------

		v1.GET("/scenarios", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			results, total, err := srv.GetScenarioAPI().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results, total)
		})

		v1.POST("/scenarios", bark.ManifestAPI(urth.KindScenario), func(ctx *gin.Context) {
			result, err := srv.GetScenarioAPI().Create(ctx.Request.Context(), bark.RequireManifest(ctx))
			bark.MaybeResourceCreated(ctx, result.ToManifest(), err)
		})

		v1.GET("/scenarios/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.GetScenarioAPI().Get(ctx.Request.Context(), bark.RequireResourceName(ctx))
			bark.MaybeManifest(ctx, resource.ToManifest(), exists, err)
		})

		v1.DELETE("/scenarios/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			versionedId := bark.RequireVersionedResource(ctx)

			existed, err := srv.GetScenarioAPI().Delete(ctx.Request.Context(), versionedId)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !existed {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			ctx.Status(http.StatusNoContent)
		})

		v1.PUT("/scenarios/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), bark.ManifestAPI(urth.KindScenario), func(ctx *gin.Context) {
			versionedId := bark.RequireVersionedResource(ctx)
			newEntry := bark.RequireManifest(ctx)

			updateResponse, err := srv.GetScenarioAPI().Update(ctx.Request.Context(), versionedId, newEntry)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			bark.MarshalResponse(ctx, http.StatusCreated, updateResponse)
		})

		v1.GET("/scenarios/:id/script", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.GetScenarioAPI().Get(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !exists {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			data, ok := resource.Spec.Prob.Spec.(map[string]any)
			if !ok || data == nil || len(data) == 0 {
				bark.AbortWithError(ctx, http.StatusNotFound, fmt.Errorf("prob spec %q is %w", reflect.TypeOf(resource.Spec.Prob.Spec), manifest.ErrNilSpec))
				return
			}

			scriptData, ok := data["Script"]
			if !ok {
				bark.AbortWithError(ctx, http.StatusNotFound, fmt.Errorf("prod has no 'Script' field"))
				return
			}
			script, ok := scriptData.(string)
			if !ok {
				bark.AbortWithError(ctx, http.StatusNotFound, fmt.Errorf("prod 'Script' field is not a string"))
				return
			}

			ctx.Header(bark.HTTPHeaderContentType, gin.MIMEPlain)
			ctx.Writer.Write([]byte(script))
		})

		/*
			v1.PUT("/scenarios/:id/script", ResourceAPI(), versionedResourceApi(), func(ctx *gin.Context) {
				versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)

				// Considers streaming data to a blob storage
				data, err := ctx.GetRawData()
				if err != nil {
					bark.AbortWithError(ctx, http.StatusBadRequest, err)
					return
				}

				result, exists, err := srv.GetScenarioAPI().UpdateScript(ctx.Request.Context(), versionedId, urth.ScenarioScript{
					Kind:    urth.GuessScenarioKind(ctx.Query("kind"), ctx.ContentType(), data),
					Content: data,
				})
				if err != nil {
					bark.AbortWithError(ctx, http.StatusBadRequest, err)
					return
				}
				if !exists {
					bark.AbortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
					return
				}

				bark.MarshalResponse(ctx, http.StatusCreated, result)
			})
		*/
		// DELETE script ? => UpdateScript("")

		//------------
		// Scenario run results API
		//------------

		v1.GET("/scenarios/:id/results", bark.SearchableAPI(paginationLimit), bark.ResourceAPI(), func(ctx *gin.Context) {
			results, total, err := srv.GetResultsAPI(bark.RequireResourceName(ctx)).List(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results, total)
		})
		v1.POST("/scenarios/:id/results", bark.ResourceAPI(), bark.ManifestAPI(urth.KindResult), func(ctx *gin.Context) {
			newEntry := bark.RequireManifest(ctx)
			result, err := srv.GetResultsAPI(bark.RequireResourceName(ctx)).Create(ctx.Request.Context(), newEntry)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			bark.ReplyResourceCreated(ctx, result, result)
		})

		v1.GET("/scenarios/:id/results/:runId", func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				bark.AbortWithError(ctx, http.StatusNotFound, err)
				return
			}

			resource, exists, err := srv.GetResultsAPI(manifest.ResourceName(resourceRequest.ID)).Get(ctx.Request.Context(), manifest.ResourceName(resourceRequest.RunId))
			bark.MaybeManifest(ctx, resource.ToManifest(), exists, err)
		})
		v1.POST("/auth/:id/:runId", func(ctx *gin.Context) {
			// v1.POST("/scenarios/:id/results/:runId/auth", func(ctx *gin.Context) {
			log.Print("YO!")

			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				log.Print("error while trying to bind to ScenarioRunResultsRequest", "err", err)
				bark.AbortWithError(ctx, http.StatusNotFound, err)
				return
			}

			var authRequest urth.AuthJobRequest
			if err := ctx.ShouldBind(&authRequest); err != nil {
				log.Print("error while trying to read AuthJobRequest", "err", err)
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			resource, err := srv.GetResultsAPI(manifest.ResourceName(resourceRequest.ID)).Auth(ctx.Request.Context(), resourceRequest.RunId, authRequest)
			// resource, err := srv.GetResultsAPI(manifest.ResourceName(resourceRequest.ID)).Auth(ctx.Request.Context(), manifest.NewVersionedID(resourceRequest.RunId, versionedInfo.Version), authRequest)
			if err != nil {
				log.Print("error while calling auth", "err", err)
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
			bark.Ok(ctx, resource)
		})

		v1.PUT("/scenarios/:id/results/:runId", bark.AuthBearerAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				bark.AbortWithError(ctx, http.StatusNotFound, err)
				return
			}

			versionInfo := bark.RequireVersionedResourceQuery(ctx)

			// FIXME: Should require valid JWT with exp claim validated
			token := bark.RequireBearerToken(ctx)

			var newEntry urth.ResultStatus
			if err := ctx.ShouldBind(&newEntry); err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			resource, err := srv.GetResultsAPI(manifest.ResourceName(resourceRequest.ID)).Update(ctx.Request.Context(), manifest.NewVersionedID(manifest.ResourceID(resourceRequest.RunId), versionInfo.Version), urth.ApiToken(token), newEntry)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			bark.Ok(ctx, resource)
		})

		//------------
		// Artifacts API
		//------------
		v1.GET("/artifacts", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			results, total, err := srv.GetArtifactsApi().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.FoundOrNot(ctx, err, results, total)
		})

		// FIXME: Require valid worker auth / JWT
		v1.POST("/artifacts", bark.ContentTypeAPI() /*authBearerApi(),*/, bark.ManifestAPI(urth.KindArtifact), func(ctx *gin.Context) {
			// TODO: Considers streaming data to a blob storage
			result, err := srv.GetArtifactsApi().Create(ctx.Request.Context(), bark.RequireManifest(ctx))
			bark.MaybeResourceCreated(ctx, result.ToManifest(), err)
		})

		v1.GET("/artifacts/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.GetArtifactsApi().Get(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !exists {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			// TODO: Find a better way to not-expand content
			resource.Spec.Content = nil
			bark.Ok(ctx, resource)
		})
		v1.GET("/artifacts/:id/content", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.GetArtifactsApi().GetContent(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			if !exists {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			ctx.Writer.Header().Set("Content-Type", resource.MimeType)
			ctx.Writer.Write(resource.Content)
		})

		// TODO: POST("/artifacts/:id/content") ???

		// FIXME: Should you be able to delete an artifact. It should auto-expire
		v1.DELETE("/artifacts/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			versionedId := bark.RequireVersionedResource(ctx)

			existed, err := srv.GetArtifactsApi().Delete(ctx.Request.Context(), versionedId)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !existed {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			ctx.Status(http.StatusNoContent)
		})
	}

	return router
}

var appCli struct {
	dbstore.StoreConfig `help:"Persistent storage URL" embed:"" prefix:"store."`

	MessageBrokerUrl string `help:"Message broker address:port to connect to" default:"localhost:6379"`
}

func main() {
	kong.Parse(&appCli,
		kong.Name("urthd"),
		kong.Description("Urth API service"),
	)
	// FIXME: Find a way to pass custom context to Gin
	// ctx := grace.NewSignalHandlingContext()

	dial, err := appCli.Dialector()
	grace.SuccessRequired(err, "Failed to create datasource connector (config issue)")

	// Init DB connection based on env or config
	db, err := gorm.Open(dial, &gorm.Config{
		Logger: dlogger.Default.LogMode(dlogger.Info),
	})

	grace.SuccessRequired(err, "failed to connect the datastore")

	// Migrate the schema (TODO: should be limited to dev env only)
	grace.SuccessRequired(db.AutoMigrate(
		&urth.WorkerInstance{},
		&urth.Runner{},
		&urth.Scenario{},
		&urth.Result{},
		&urth.Artifact{},
	), "DB schema migration failed")

	// Init service
	store, err := dbstore.NewDBStore(db, dbstore.Config{})
	grace.SuccessRequired(err, "db store")

	// scheduler, err := gqueue.NewScheduler(ctx, "test-local-321", "prob-request")
	scheduler, err := redqueue.NewScheduler(context.TODO(), appCli.MessageBrokerUrl)
	grace.SuccessRequired(err, "failed to create a scheduler")
	defer scheduler.Close()

	api := urth.NewService(store, scheduler)
	router := apiRoutes(api)

	grace.FatalOnError(router.Run()) // listen and serve on 0.0.0.0:8080
}
