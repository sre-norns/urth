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
	// dlogger "gorm.io/gorm/logger"
)

const (
	paginationLimit = 512
	kindRequestKey  = "request_kind"
)

var kindMap = map[string]manifest.Kind{
	string(urth.KindWorkerInstance): urth.KindWorkerInstance,
	string(urth.KindRunner):         urth.KindRunner,
	string(urth.KindScenario):       urth.KindScenario,
	string(urth.KindResult):         urth.KindResult,
	string(urth.KindArtifact):       urth.KindArtifact,
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

func apiRoutes(srv urth.Service) *gin.Engine {
	router := gin.Default()
	router.UseRawPath = true

	// Simple group: v1
	v1 := router.Group("/api/v1", bark.ContentTypeAPI())
	{
		v1.GET("/version", func(ctx *gin.Context) {
			bark.Ok(ctx, bark.NewVersionResponse())
		})

		search := v1.Group("/search/:kind", KindAPI(), bark.SearchableAPI(paginationLimit))
		{
			search.GET("/names", func(ctx *gin.Context) {
				results, total, err := srv.Labels(RequireKind(ctx)).ListNames(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
				bark.FoundOrNot(ctx, err, results.Slice(), total)
			})
			// Support search by listing all possible labels
			search.GET("/labels", func(ctx *gin.Context) {
				results, total, err := srv.Labels(RequireKind(ctx)).ListLabels(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
				bark.FoundOrNot(ctx, err, results.Slice(), total)
			})
			// Support search by listing all values of a given label
			search.GET("/labels/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
				results, total, err := srv.Labels(RequireKind(ctx)).ListLabelValues(ctx.Request.Context(), string(bark.RequireResourceID(ctx)), bark.RequireSearchQuery(ctx))
				bark.FoundOrNot(ctx, err, results.Slice(), total)
			})
		}

		//------------
		// Auth API for various operations
		//------------
		// Auth API for Worker to assume a Runner identity, given auth token
		v1.POST("/auth/runners", bark.AuthBearerAPI(), bark.ManifestAPI(urth.KindWorkerInstance), func(ctx *gin.Context) {
			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")

			token := bark.RequireBearerToken(ctx)
			bark.Manifest(ctx).Created(srv.Runners().Auth(ctx.Request.Context(), urth.ApiToken(token), bark.RequireManifest(ctx)))
		})
		// Request a JWT token to be used by workers to Auth as a Runner instance
		v1.GET("/auth/runners/:id" /*bark.AuthBearerAPI(),*/, bark.ResourceAPI(), func(ctx *gin.Context) {
			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")

			// TODO: Validate user's credentials and ACL
			// token := bark.RequireBearerToken(ctx)
			token, found, err := srv.Runners().GetToken(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			} else if !found {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			ctx.Header(bark.HTTPHeaderContentType, "application/jwt")
			ctx.Writer.Write([]byte(token))
		})

		// "/scenarios/:id/results/:runId/auth"
		v1.POST("/auth//scenarios/:id/:runId", func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				log.Print("error while trying to bind to ScenarioRunResultsRequest", "err", err)
				bark.AbortWithError(ctx, http.StatusNotFound, err)
				return
			}

			var authRequest urth.AuthJobRequest
			if err := ctx.ShouldBind(&authRequest); err != nil {
				log.Print("error while trying to parse AuthJobRequest", "err", err)
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			resource, err := srv.Results(manifest.ResourceName(resourceRequest.ID)).Auth(ctx.Request.Context(), resourceRequest.RunId, authRequest)
			if err != nil {
				log.Print("error while calling auth", "err", err)
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
			bark.Ok(ctx, resource)
		})
		//------------
		// Runners API
		//------------
		v1.GET("/runners", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			bark.Manifest(ctx).List(srv.Runners().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx)))
		})
		v1.POST("/runners", bark.ManifestAPI(urth.KindRunner), func(ctx *gin.Context) {
			bark.Manifest(ctx).Created(srv.Runners().Create(ctx.Request.Context(), bark.RequireManifest(ctx)))
		})
		v1.GET("/runners/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Found(srv.Runners().Get(ctx.Request.Context(), bark.RequireResourceName(ctx)))
		})
		// Create or Update existing resource
		// bark.VersionedResourceAPI()
		v1.PUT("/runners/:id", bark.ResourceAPI(), bark.ManifestAPI(urth.KindRunner), func(ctx *gin.Context) {
			// 	versionedId := bark.RequireVersionedResource(ctx)
			bark.Manifest(ctx).CreatedOrUpdated(srv.Runners().CreateOrUpdate(ctx.Request.Context(), bark.RequireManifest(ctx)))
		})
		v1.DELETE("/runners/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Deleted(srv.Runners().Delete(ctx.Request.Context(), bark.RequireVersionedResource(ctx)))
		})
		//------------
		// Scenarios API
		//------------
		v1.GET("/scenarios", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			bark.Manifest(ctx).List(srv.Scenarios().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx)))
		})
		v1.POST("/scenarios", bark.ManifestAPI(urth.KindScenario), func(ctx *gin.Context) {
			bark.Manifest(ctx).Created(srv.Scenarios().Create(ctx.Request.Context(), bark.RequireManifest(ctx)))
		})
		v1.GET("/scenarios/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Found(srv.Scenarios().Get(ctx.Request.Context(), bark.RequireResourceName(ctx)))
		})
		// Create or Update existing resource
		// bark.VersionedResourceAPI(
		v1.PUT("/scenarios/:id", bark.ResourceAPI(), bark.ManifestAPI(urth.KindScenario), func(ctx *gin.Context) {
			// 	versionedId := bark.RequireVersionedResource(ctx)
			bark.Manifest(ctx).CreatedOrUpdated(srv.Scenarios().CreateOrUpdate(ctx.Request.Context(), bark.RequireManifest(ctx)))
		})
		v1.DELETE("/scenarios/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Deleted(srv.Scenarios().Delete(ctx.Request.Context(), bark.RequireVersionedResource(ctx)))
		})

		v1.GET("/scenarios/:id/script", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.Scenarios().Get(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			} else if !exists {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			scenario, err := urth.NewScenario(resource)
			if err != nil {
				bark.AbortWithError(ctx, http.StatusInternalServerError, bark.ErrResourceNotFound)
			}

			data, ok := scenario.Spec.Prob.Spec.(map[string]any)
			if !ok || data == nil || len(data) == 0 {
				bark.AbortWithError(ctx, http.StatusNotFound, fmt.Errorf("prob spec %q is %w", reflect.TypeOf(scenario.Spec.Prob.Spec), manifest.ErrNilSpec))
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
		// Scenario run Results API
		//------------

		v1.GET("/scenarios/:id/results", bark.SearchableAPI(paginationLimit), bark.ResourceAPI(), func(ctx *gin.Context) {
			bark.WithContext[urth.Result](ctx).List(srv.Results(bark.RequireResourceName(ctx)).List(ctx.Request.Context(), bark.RequireSearchQuery(ctx)))
		})
		// AuthBearerAPI: Who is authorized to create new results ???
		v1.POST("/scenarios/:id/results", bark.ResourceAPI(), bark.ManifestAPI(urth.KindResult), func(ctx *gin.Context) {
			bark.WithContext[urth.Result](ctx).Created(srv.Results(bark.RequireResourceName(ctx)).Create(ctx.Request.Context(), bark.RequireManifest(ctx)))
		})
		v1.GET("/scenarios/:id/results/:runId", func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				bark.AbortWithError(ctx, http.StatusNotFound, err)
				return
			}

			bark.WithContext[urth.Result](ctx).Found(srv.Results(manifest.ResourceName(resourceRequest.ID)).Get(ctx.Request.Context(), manifest.ResourceName(resourceRequest.RunId)))
		})
		v1.PUT("/scenarios/:id/results/:runId/status", bark.AuthBearerAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				bark.AbortWithError(ctx, http.StatusNotFound, err)
				return
			}

			versionInfo := bark.RequireVersionedResourceQuery(ctx)
			token := bark.RequireBearerToken(ctx)

			var newEntry urth.ResultStatus
			if err := ctx.ShouldBind(&newEntry); err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			resource, err := srv.Results(manifest.ResourceName(resourceRequest.ID)).UpdateStatus(ctx.Request.Context(), manifest.NewVersionedID(manifest.ResourceID(resourceRequest.RunId), versionInfo.Version), urth.ApiToken(token), newEntry)
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
			bark.Manifest(ctx).List(srv.Artifacts().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx)))
		})

		// FIXME: Require valid worker auth / JWT
		// TODO: Considers streaming data to a blob storage
		v1.POST("/artifacts", bark.AuthBearerAPI(), bark.ManifestAPI(urth.KindArtifact), func(ctx *gin.Context) {
			token := bark.RequireBearerToken(ctx)
			bark.Manifest(ctx).Created(srv.Artifacts().Create(ctx.Request.Context(), urth.ApiToken(token), bark.RequireManifest(ctx)))
		})
		v1.GET("/artifacts/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Found(srv.Artifacts().Get(ctx.Request.Context(), bark.RequireResourceName(ctx)))
		})
		v1.GET("/artifacts/:id/content", bark.ResourceAPI(), func(ctx *gin.Context) {
			resource, exists, err := srv.Artifacts().GetContent(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			} else if !exists {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			ctx.Writer.Header().Set("Content-Type", resource.MimeType)
			ctx.Writer.Write(resource.Content)
		})

		// TODO: POST("/artifacts/:id/content") ???

		// FIXME: Should you be able to delete an artifact. It should auto-expire
		v1.DELETE("/artifacts/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Deleted(srv.Artifacts().Delete(ctx.Request.Context(), bark.RequireVersionedResource(ctx)))
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
		// Logger: dlogger.Default.LogMode(dlogger.Info),
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
