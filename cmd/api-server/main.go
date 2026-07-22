package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"time"

	"github.com/alecthomas/kong"
	_ "github.com/joho/godotenv/autoload"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"gorm.io/gorm"
	// dlogger "gorm.io/gorm/logger"

	// Prober packages are linked for their registration side effects only. The
	// server does not execute probs -- workers do -- but it owns the registry of
	// which kinds exist, and cannot answer that for kinds it has never seen.
	// Without these imports GET /probs reports an empty list rather than a wrong
	// one, which is a quieter failure than it looks.
	_ "github.com/sre-norns/urth/pkg/probers/dns"
	_ "github.com/sre-norns/urth/pkg/probers/grpc"
	_ "github.com/sre-norns/urth/pkg/probers/har"
	_ "github.com/sre-norns/urth/pkg/probers/http"
	_ "github.com/sre-norns/urth/pkg/probers/icmp"
	_ "github.com/sre-norns/urth/pkg/probers/puppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/pypuppeteer"
	_ "github.com/sre-norns/urth/pkg/probers/rest"
	_ "github.com/sre-norns/urth/pkg/probers/tcp"
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

func apiRoutes(srv urth.Service, natsConn *nats.Conn) *gin.Engine {
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
			// This route exists to serve the deprecated Auth flow; the /auth/workers
			// route below is the replacement. Kept because the asynq prototype worker
			// still reads its identity out of the runner manifest this returns.
			//lint:ignore SA1019 deliberate: this handler implements the deprecated endpoint the prototype worker depends on.
			bark.Manifest(ctx).Created(srv.Runners().Auth(ctx.Request.Context(), urth.APIToken(token), bark.RequireManifest(ctx)))
		})
		// Worker registration: exchange an enrolment token for an identity, a
		// session credential, and the queue to pull from.
		//
		// Separate from /auth/runners rather than replacing it, because the
		// asynq prototype worker reads its identity out of the runner manifest
		// that route returns. Both admit workers by the same rules.
		v1.POST("/auth/workers", bark.AuthBearerAPI(), bark.ManifestAPI(urth.KindWorkerInstance), func(ctx *gin.Context) {
			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")

			token := bark.RequireBearerToken(ctx)
			registration, err := srv.Runners().AuthWorker(ctx.Request.Context(), urth.APIToken(token), bark.RequireManifest(ctx))
			if err != nil {
				bark.AbortWithError(ctx, http.StatusUnauthorized, err)
				return
			}

			bark.Ok(ctx, registration)
		})

		// Job claim, authenticated by the worker's session.
		//
		// Unlike the legacy /auth//scenarios route below, this one derives the
		// claiming worker and its runner from the bearer token. Nothing in the
		// request body identifies the caller, because a request body is not
		// evidence of identity.
		v1.POST("/auth/runs/:resultUid/claim", bark.AuthBearerAPI(), func(ctx *gin.Context) {
			resultUID := manifest.ResourceID(ctx.Param("resultUid"))
			if resultUID == "" {
				bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
				return
			}

			var claimRequest urth.ClaimJobRequest
			if err := ctx.ShouldBind(&claimRequest); err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			session := urth.APIToken(bark.RequireBearerToken(ctx))
			resource, err := srv.Results("").ClaimRun(ctx.Request.Context(), resultUID, session, claimRequest)
			if err != nil {
				// Deliberately not distinguishing "no such run" from "not
				// yours" from "already taken": a worker that may not have this
				// job has no business learning which of those it was.
				log.Print("claim refused for run ", resultUID, ": ", err)
				bark.AbortWithError(ctx, http.StatusUnauthorized, bark.ErrResourceUnauthorized)
				return
			}

			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
			bark.Ok(ctx, resource)
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

			// This route implements the deprecated body-asserted job claim retained
			// for the asynq prototype worker; ClaimRun is the session-backed replacement.
			//lint:ignore SA1019 deliberate: this handler implements the deprecated endpoint the prototype worker depends on.
			resource, err := srv.Results(manifest.ResourceName(resourceRequest.ID)).Auth(ctx.Request.Context(), resourceRequest.RunID, authRequest)
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
		// The kinds of prob a scenario may declare. Read by clients offering a
		// choice, so that the list comes from the server rather than being
		// duplicated and drifting.
		v1.GET("/probs", func(ctx *gin.Context) {
			ctx.JSON(http.StatusOK, gin.H{"data": urth.ListProbKinds()})
		})
		//------------
		// Run results, across all scenarios
		//------------
		// Distinct from /scenarios/:id/results, which is scoped to one scenario.
		// This answers "what has run recently, anywhere", which is how a failure
		// is found when its scenario is not known yet.
		v1.GET("/results", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			bark.WithContext[urth.Result](ctx).List(srv.AllResults().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx)))
		})
		v1.GET("/results/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			bark.WithContext[urth.Result](ctx).Found(srv.AllResults().Get(ctx.Request.Context(), bark.RequireResourceName(ctx)))
		})
		//------------
		// Workers API
		//------------
		// Worker instances are created by registration, not by an operator, so
		// there is no POST here. These endpoints exist to see who has registered
		// against a runner and to take one out of service.
		v1.GET("/workers", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			bark.Manifest(ctx).List(srv.Workers().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx)))
		})
		v1.GET("/workers/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Found(srv.Workers().Get(ctx.Request.Context(), bark.RequireResourceName(ctx)))
		})
		// Pause or resume a single worker. Separate from a resource update
		// because a worker rewrites its own record on every registration; an
		// operator's decision has to land somewhere the worker cannot reach.
		v1.PUT("/workers/:id/paused", bark.ResourceAPI(), func(ctx *gin.Context) {
			var request urth.SetPausedRequest
			if err := ctx.ShouldBindJSON(&request); err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			bark.Manifest(ctx).Found(
				srv.Workers().SetPaused(ctx.Request.Context(), bark.RequireResourceName(ctx), request.IsPaused),
			)
		})
		// Revoke a worker's registration.
		v1.DELETE("/workers/:id", bark.ResourceAPI(), bark.VersionedResourceAPI(), func(ctx *gin.Context) {
			bark.Manifest(ctx).Deleted(srv.Workers().Delete(ctx.Request.Context(), bark.RequireVersionedResource(ctx)))
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

			bark.WithContext[urth.Result](ctx).Found(srv.Results(manifest.ResourceName(resourceRequest.ID)).Get(ctx.Request.Context(), manifest.ResourceName(resourceRequest.RunID)))
		})
		// Live run log, falling back to the stored artifact once the run has
		// finished, so one URL serves a run whether or not it is still going.
		v1.GET("/scenarios/:id/results/:runId/logs", runLogHandler(srv, natsConn))
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

			resource, err := srv.Results(manifest.ResourceName(resourceRequest.ID)).UpdateStatus(ctx.Request.Context(), manifest.NewVersionedID(manifest.ResourceID(resourceRequest.RunID), versionInfo.Version), urth.APIToken(token), newEntry)
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
			bark.Manifest(ctx).Created(srv.Artifacts().Create(ctx.Request.Context(), urth.APIToken(token), bark.RequireManifest(ctx)))
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

			ctx.Writer.Header().Set("Content-Type", resource.Artifact.MimeType)
			ctx.Writer.Write(resource.Artifact.Content)
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
	dbstore.Config `help:"Persistent storage URL" embed:"" prefix:"store."`

	// Named rather than embedded: both of these are called Config, and
	// embedding a second one collides with dbstore's.
	Signing urth.SigningKeysConfig `embed:"" prefix:"signing."`
	NATS    natsq.Config           `embed:"" prefix:"nats."`

	// Transport selects the job queue. Both implementations are kept while the
	// migration in ADR 0004 proceeds, so an operator can cut over and back
	// without changing binaries.
	Transport string `help:"Job transport to use: nats or asynq" enum:"nats,asynq" default:"asynq"`

	MessageBrokerURL string `help:"Message broker address:port to connect to (asynq transport)" default:"localhost:6379"`

	SessionTTL     time.Duration `help:"How long an issued worker session remains valid" default:"1h"`
	MaxRunDuration time.Duration `help:"Maximum time a worker may hold a run capability" default:"30m"`
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
	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	grace.SuccessRequired(err, "db store")

	keys, err := appCli.Signing.Build()
	grace.SuccessRequired(err, "failed to prepare token signing keys")

	serviceOptions := []urth.ServiceOption{
		urth.WithSigningKeys(keys),
		urth.WithSessionTTL(appCli.SessionTTL),
		urth.WithMaxRunDuration(appCli.MaxRunDuration),
	}

	var natsConn *nats.Conn

	var scheduler urth.Scheduler
	switch appCli.Transport {
	case "nats":
		natsScheduler, nerr := natsq.NewScheduler(context.TODO(), appCli.NATS)
		grace.SuccessRequired(nerr, "failed to connect to NATS")
		scheduler = natsScheduler

		// The NATS scheduler doubles as the transport provider: it already owns
		// the JetStream handle and the naming, so having it answer "where does
		// this runner collect work" keeps one component responsible for the
		// topology.
		if provider, ok := natsScheduler.(urth.WorkerTransportProvider); ok {
			serviceOptions = append(serviceOptions, urth.WithWorkerTransport(provider))
		}

		// A separate connection for log tailing, so a browser holding a slow
		// stream open cannot interfere with job publication.
		natsConn, err = appCli.NATS.Connect("urth-api-server-logs")
		grace.SuccessRequired(err, "failed to connect to NATS for run log streaming")
		defer natsConn.Drain()
	default:
		scheduler, err = redqueue.NewScheduler(context.TODO(), appCli.MessageBrokerURL)
		grace.SuccessRequired(err, "failed to create a scheduler")
	}
	defer scheduler.Close()

	api := urth.NewService(store, scheduler, serviceOptions...)
	router := apiRoutes(api, natsConn)

	grace.FatalOnError(router.Run()) // listen and serve on 0.0.0.0:8080
}
