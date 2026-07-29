package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/alecthomas/kong"
	_ "github.com/joho/godotenv/autoload"
	"github.com/sre-norns/urth/pkg/controllers"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/grace"
	"github.com/sre-norns/wyrd/pkg/manifest"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	// Without this the label-search endpoints report an unknown kind for dead
	// letters, so the one resource an operator most often filters by reason or
	// runner would be the one kind they could not enumerate labels for.
	string(urth.KindDispatchFailure): urth.KindDispatchFailure,
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

// Generic, reason-free claim rejections. A worker keys off the status class, not
// the body: the body must not reveal which run exists, who holds it, or the exact
// reason it was refused. The status class is enough for the worker to decide
// whether to retry, acknowledge, or terminate the dispatch.
var (
	errClaimUnavailable = &bark.ErrorResponse{Code: http.StatusServiceUnavailable, Message: "claim temporarily unavailable"}
	errClaimObsolete    = &bark.ErrorResponse{Code: http.StatusConflict, Message: "run is not claimable"}
	errClaimForbidden   = &bark.ErrorResponse{Code: http.StatusForbidden, Message: "claim refused"}
)

// claimHTTPResponse maps a claim outcome to the generic response the worker sees.
// An unclassified error becomes 503 rather than a terminal refusal: acking or
// terminating the only dispatch for a still-pending run loses it, so the safe
// default is to have the worker retry. This is a pure function so the mapping can
// be tested without standing up the HTTP stack.
func claimHTTPResponse(err error) *bark.ErrorResponse {
	disposition, _ := urth.ClaimDispositionOf(err)
	switch disposition {
	case urth.ClaimObsolete:
		return errClaimObsolete
	case urth.ClaimForbidden:
		return errClaimForbidden
	default:
		return errClaimUnavailable
	}
}

// abortClaim answers a failed claim with a generic body while recording the full
// reason server-side. The worker never learns more than the status class.
func abortClaim(ctx *gin.Context, resultUID manifest.ResourceID, err error) {
	response := claimHTTPResponse(err)
	log.Printf("claim for run %v refused (%d): %v", resultUID, response.Code, err)
	bark.AbortWithError(ctx, response.Code, response)
}

// abortDispatchReport answers a refused dead-letter report.
//
// The status class carries the same meaning as it does on the claim path,
// because the worker reads it the same way: 4xx means this report will never be
// accepted and the message should be terminated anyway, while 5xx means try
// again later and leave the message alone. Getting that backwards either spins
// on an unreportable message forever or throws away the evidence.
func abortDispatchReport(ctx *gin.Context, err error) {
	if errors.Is(err, urth.ErrInvalidDispatchFailure) {
		log.Printf("dispatch failure report refused as malformed: %v", err)
		bark.AbortWithError(ctx, http.StatusBadRequest, err)

		return
	}

	response := claimHTTPResponse(err)
	log.Printf("dispatch failure report refused (%d): %v", response.Code, err)
	bark.AbortWithError(ctx, response.Code, response)
}

// probScript extracts the script a prob spec carries, if its kind has one.
//
// Reflection rather than a type switch because the set of script-bearing kinds
// is open: probers register themselves at link time (pkg/probers/*), and a type
// switch here would be a list this file has to be edited to extend -- silently
// returning "no script" for any kind added later.
//
// It reads a *typed* spec, which is the whole point. This used to assert the
// spec to map[string]any and look for a "Script" key, which worked only while
// specs were untyped maps. Once the api-server started linking probers in, every
// spec decoded to its registered type instead, the assertion failed for every
// kind, and the endpoint answered 404 for scenarios that plainly had a script.
// The map case is still handled for a kind nobody registered, where the spec
// really does arrive as a map.
func probScript(spec any) (string, bool) {
	if data, ok := spec.(map[string]any); ok {
		// Both spellings: YAML authored with Go field names decodes as "Script",
		// JSON through the struct tags as "script".
		for _, key := range []string{"script", "Script"} {
			if script, ok := data[key].(string); ok && script != "" {
				return script, true
			}
		}

		return "", false
	}

	value := reflect.Indirect(reflect.ValueOf(spec))
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return "", false
	}

	field := value.FieldByName("Script")
	if !field.IsValid() || field.Kind() != reflect.String || field.String() == "" {
		return "", false
	}

	return field.String(), true
}

// dispatchFailureManifests renders a listing in the shape every other resource
// list uses.
func dispatchFailureManifests(failures []urth.DispatchFailure) []manifest.ResourceManifest {
	manifests := make([]manifest.ResourceManifest, 0, len(failures))
	for _, failure := range failures {
		manifests = append(manifests, failure.ToManifest())
	}

	return manifests
}

// statusForResourceError maps an operator action's failure to a status.
func statusForResourceError(err error) int {
	switch {
	case errors.Is(err, bark.ErrResourceNotFound):
		return http.StatusNotFound
	case errors.Is(err, bark.ErrForbidden):
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func apiRoutes(srv urth.Service, natsConn *nats.Conn, metrics *prometheus.Registry) *gin.Engine {
	router := gin.Default()
	router.UseRawPath = true

	// Deliberately outside the /api/v1 group. Prometheus asks for a text exposition
	// format, and bark's content negotiation on that group answers anything it does
	// not recognise with 406 -- which is how the live run log stream came to be
	// unreachable from a browser (task 019). A scrape endpoint is not a resource
	// API and does not want that middleware.
	if metrics != nil {
		router.GET("/metrics", gin.WrapH(promhttp.HandlerFor(metrics, promhttp.HandlerOpts{
			// A collector that cannot reach its backend reports the failure as a
			// metric and returns what it does have; continuing is what lets
			// "NATS is unreachable" be visible rather than blanking the scrape.
			ErrorHandling: promhttp.ContinueOnError,
		})))
	}

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

		// Worker liveness, authenticated by the worker's session.
		//
		// Grouped with the other session-authenticated worker routes rather than
		// under /workers/:id, because the caller's identity comes from the token
		// and nothing else: a heartbeat that named the worker it was for would
		// let anyone report presence on anyone's behalf.
		v1.POST("/auth/workers/heartbeat", bark.AuthBearerAPI(), func(ctx *gin.Context) {
			var request urth.WorkerHeartbeatRequest
			// Bound leniently: an empty body is the ordinary heartbeat, and
			// requiring `{}` of every worker every interval would be ceremony.
			if ctx.Request.ContentLength > 0 {
				if err := ctx.ShouldBindJSON(&request); err != nil {
					bark.AbortWithError(ctx, http.StatusBadRequest, err)
					return
				}
			}

			session := urth.APIToken(bark.RequireBearerToken(ctx))
			response, err := srv.Workers().Heartbeat(ctx.Request.Context(), session, request)
			if err != nil {
				// A dropped registration is 404 so the worker re-registers; a bad
				// or expired session is 401 so it stops.
				if errors.Is(err, bark.ErrResourceNotFound) {
					bark.AbortWithError(ctx, http.StatusNotFound, err)
					return
				}

				bark.AbortWithError(ctx, http.StatusUnauthorized, err)
				return
			}

			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
			bark.Ok(ctx, response)
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
				abortClaim(ctx, resultUID, err)
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
		// Dispatch failures (dead letters)
		//------------
		// The operational answer to "why did this run never start". Reads are
		// open like any other resource; the write paths are asymmetric on
		// purpose -- reporting is a worker talking about work it was handed,
		// while retrying and resolving are operator actions.
		// Listed as manifests rather than as the model, so an entry has its name
		// and labels under `metadata` exactly as every other resource does.
		// Serializing the model directly is what makes a `Result` come back flat
		// and forces the UI to special-case it; there is no reason to grow a
		// second resource with that shape.
		v1.GET("/dispatch-failures", bark.SearchableAPI(paginationLimit), func(ctx *gin.Context) {
			failures, total, err := srv.DispatchFailures().List(ctx.Request.Context(), bark.RequireSearchQuery(ctx))
			bark.Manifest(ctx).List(dispatchFailureManifests(failures), total, err)
		})
		v1.GET("/dispatch-failures/:id", bark.ResourceAPI(), func(ctx *gin.Context) {
			failure, found, err := srv.DispatchFailures().Get(ctx.Request.Context(), bark.RequireResourceName(ctx))
			bark.Manifest(ctx).Found(failure.ToManifest(), found, err)
		})

		// A worker reporting a dispatch it cannot make progress on. Bearer
		// authenticated for the same reason the claim is: the report strands a
		// run, and the identity behind it comes from the session rather than the
		// body.
		v1.POST("/dispatch-failures", bark.AuthBearerAPI(), func(ctx *gin.Context) {
			var report urth.ReportDispatchFailureRequest
			if err := ctx.ShouldBind(&report); err != nil {
				bark.AbortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			session := urth.APIToken(bark.RequireBearerToken(ctx))
			failure, err := srv.DispatchFailures().Report(ctx.Request.Context(), session, report)
			if err != nil {
				abortDispatchReport(ctx, err)
				return
			}

			ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
			bark.Ok(ctx, failure.ToManifest())
		})

		v1.POST("/dispatch-failures/:id/retry", bark.ResourceAPI(), func(ctx *gin.Context) {
			var request urth.RetryDispatchFailureRequest
			// An empty body is the common case -- "retry this, with the
			// defaults" -- so a body that will not bind is only an error when
			// one was actually sent.
			if ctx.Request.ContentLength > 0 {
				if err := ctx.ShouldBind(&request); err != nil {
					bark.AbortWithError(ctx, http.StatusBadRequest, err)
					return
				}
			}

			failure, retry, err := srv.DispatchFailures().Retry(ctx.Request.Context(),
				bark.RequireResourceName(ctx), request)
			if err != nil {
				bark.AbortWithError(ctx, statusForResourceError(err), err)
				return
			}

			bark.Ok(ctx, urth.RetryDispatchFailureResponse{
				Failure: failure.ToManifest(),
				Retry:   retry.ToManifest(),
			})
		})

		v1.POST("/dispatch-failures/:id/resolve", bark.ResourceAPI(), func(ctx *gin.Context) {
			failure, err := srv.DispatchFailures().Resolve(ctx.Request.Context(), bark.RequireResourceName(ctx))
			if err != nil {
				bark.AbortWithError(ctx, statusForResourceError(err), err)
				return
			}

			bark.Ok(ctx, failure.ToManifest())
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

		// Where a run of this scenario would go, without creating one. Read
		// before offering to trigger a run: a scenario whose requirements match
		// no active runner produces a run that is terminal the moment it exists.
		v1.GET("/scenarios/:id/placement", bark.ResourceAPI(), func(ctx *gin.Context) {
			preview, exists, err := srv.Scenarios().Placement(ctx.Request.Context(), bark.RequireResourceName(ctx))
			bark.MaybeGotOne(ctx, preview, exists, err)
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
				bark.AbortWithError(ctx, http.StatusInternalServerError, err)
				return
			}

			script, ok := probScript(scenario.Spec.Prob.Spec)
			if !ok {
				bark.AbortWithError(ctx, http.StatusNotFound,
					fmt.Errorf("prob kind %q carries no script", scenario.Spec.Prob.Kind))
				return
			}

			ctx.Header(bark.HTTPHeaderContentType, gin.MIMEPlain)
			ctx.Writer.Write([]byte(script))
		})

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

	// Worker liveness. The interval is what the server asks workers to report
	// at; the timeout is derived from it unless set, so the two cannot be
	// configured into contradicting each other.
	WorkerHeartbeatInterval time.Duration `name:"worker.heartbeat-interval" help:"How often a worker is asked to report that it is still there" default:"1m"`
	WorkerOfflineAfter      time.Duration `name:"worker.offline-after" help:"How long a liveness signal may go unheard before it counts as offline. Zero derives it from the heartbeat interval" default:"0"`
	WorkerRetention         time.Duration `name:"worker.retention" help:"How long a worker silent on every signal is kept before its registration is dropped" default:"24h"`

	// Control loops are configured by the package that composes them, so that a
	// command hosting them elsewhere offers the same flags rather than a second
	// set that drifted. See ADR 0006.
	Controllers controllers.Config `embed:""`
}

// metricsRegistry assembles what this process can tell an operator about the
// dispatch pipeline.
//
// Two collectors, because the pipeline has two halves that look identical from
// either side alone: the outbox knows what was committed and not yet published,
// and JetStream knows what was published and not yet claimed. A backlog in the
// first with an empty stream is the relay; the same backlog with a full stream
// is the fleet.
//
// A registry of its own rather than prometheus.DefaultRegisterer, so that what
// this endpoint exposes is a decision made here rather than whatever any
// imported package happened to register into the global.
func metricsRegistry(db *gorm.DB, scheduler urth.Scheduler, placement *urth.PlacementMetrics) *prometheus.Registry {
	registry := prometheus.NewRegistry()

	// Process and Go runtime metrics: the baseline any on-call runbook assumes is
	// there, and the thing that says whether the api-server itself is healthy
	// before its own numbers are worth reading.
	registry.MustRegister(
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
	)

	registry.MustRegister(urth.NewDispatchCollector(db, urth.NewDispatchOutbox(db)))
	registry.MustRegister(placement)

	// Only the routing transport has a stream to report on. The legacy asynq path
	// has no equivalent, and inventing empty gauges for it would read as a queue
	// that is always empty rather than one nobody is measuring.
	if source, ok := scheduler.(natsq.MetricsSource); ok {
		registry.MustRegister(source.Collector())
	}

	return registry
}

// listenAddress reproduces gin's own address resolution, which router.Run() used
// to do for us. Serving through an http.Server is what makes a graceful shutdown
// possible, and that is the one thing router.Run() cannot do -- but an operator
// setting PORT should not discover that it stopped being read.
func listenAddress() string {
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}

	return ":8080"
}

func main() {
	kong.Parse(&appCli,
		kong.Name("urthd"),
		kong.Description("Urth API service"),
	)

	ctx := grace.NewSignalHandlingContext()

	dial, err := appCli.Dialector()
	grace.SuccessRequired(err, "Failed to create datasource connector (config issue)")

	// Init DB connection based on env or config
	db, err := gorm.Open(dial, &gorm.Config{
		// Logger: dlogger.Default.LogMode(dlogger.Info),
	})

	grace.SuccessRequired(err, "failed to connect the datastore")

	// Migrate the schema (TODO: should be limited to dev env only)
	models := []any{
		&urth.WorkerInstance{},
		&urth.Runner{},
		&urth.Scenario{},
		&urth.Result{},
		&urth.Artifact{},
		&urth.DispatchFailure{},
	}
	models = append(models, controllers.Models()...)
	grace.SuccessRequired(db.AutoMigrate(models...), "DB schema migration failed")

	// Init service
	store, err := dbstore.NewDBStore(db, dbstore.ManifestModel)
	grace.SuccessRequired(err, "db store")

	keys, err := appCli.Signing.Build()
	grace.SuccessRequired(err, "failed to prepare token signing keys")

	// Worker liveness is written straight to its columns rather than through the
	// resource store, because recording it is not a resource edit: it happens on
	// a timer forever, and a resource Save would bump metadata.version every
	// interval. See urth.WorkerPresenceStore.
	presence := urth.NewWorkerPresenceStore(db)

	// Built before the service because both need it: placement increments it, and
	// the metrics registry exposes it.
	placementMetrics := urth.NewPlacementMetrics()

	serviceOptions := []urth.ServiceOption{
		urth.WithSigningKeys(keys),
		urth.WithSessionTTL(appCli.SessionTTL),
		urth.WithMaxRunDuration(appCli.MaxRunDuration),
		urth.WithWorkerPresence(presence),
		urth.WithWorkerHeartbeatInterval(appCli.WorkerHeartbeatInterval),
		urth.WithWorkerOfflineAfter(appCli.WorkerOfflineAfter),

		// Placement reads how much work each runner already holds straight from
		// the results table -- see urth.RunnerLoadStore for why this needs no
		// broker round trip.
		urth.WithRunnerLoad(urth.NewRunnerLoadStore(db)),
		urth.WithPlacementCounter(placementMetrics),
	}

	var natsConn *nats.Conn

	var scheduler urth.Scheduler

	// publisher is what the relay hands committed outbox entries to. The two
	// transports reach it differently: NATS publishes a dispatch envelope
	// straight from the entry, while asynq needs the whole job and so goes
	// through an adapter that reloads the Result and Scenario. Both share one
	// durability story, which is the point -- retiring asynq in task 015 deletes
	// the adapter rather than a second way of dispatching.
	var publisher urth.DispatchPublisher

	// channels is the transport's half of reconciliation: restoring a runner's
	// queue and withdrawing a dispatch nothing will claim. It stays nil for the
	// legacy transport, which has neither notion -- that is the honest answer,
	// not a degraded mode, and the reconciler skips those passes rather than
	// pretending it repaired something.
	var channels urth.RunnerChannelReconciler

	// presenceWatcher records the NATS half of worker liveness. Nil for a
	// transport with no broker to be present on, in which case workers report
	// that signal as `unknown` -- the honest answer, and the one that keeps the
	// reconciler from evicting them.
	var presenceWatcher *natsq.PresenceWatcher

	switch appCli.Transport {
	case "nats":
		natsScheduler, nerr := natsq.NewScheduler(context.TODO(), appCli.NATS)
		grace.SuccessRequired(nerr, "failed to connect to NATS")
		scheduler = natsScheduler
		publisher = natsScheduler
		channels = natsScheduler

		// The NATS scheduler doubles as the transport provider: it already owns
		// the JetStream handle and the naming, so having it answer "where does
		// this runner collect work" keeps one component responsible for the
		// topology.
		serviceOptions = append(serviceOptions,
			urth.WithWorkerTransport(natsScheduler),
			// The same handle answers "who is waiting at this runner's queue",
			// which is the fleet-level cross-check on per-worker presence.
			urth.WithRunnerChannelObserver(natsScheduler),
		)

		// A separate connection for log tailing, so a browser holding a slow
		// stream open cannot interfere with job publication.
		natsConn, err = appCli.NATS.Connect("urth-api-server-logs")
		grace.SuccessRequired(err, "failed to connect to NATS for run log streaming")
		defer natsConn.Drain()

		// Worker presence shares that connection. It is a handful of empty
		// messages a minute per worker, and the traffic it competes with is a
		// browser tailing a run.
		presenceWatcher = natsq.NewPresenceWatcher(natsConn, presence)
	default:
		scheduler, err = redqueue.NewScheduler(context.TODO(), appCli.MessageBrokerURL)
		grace.SuccessRequired(err, "failed to create a scheduler")
		publisher = urth.NewSchedulerDispatchPublisher(scheduler, urth.NewStoreResultLoader(store))
	}
	defer scheduler.Close()

	// Control loops run beside the API by default. Supervising them through a
	// manager is what makes that acceptable: a panic in a repair pass is
	// recovered and the loop restarted rather than taking the whole API server
	// down with it, and both loops stop with the process instead of being
	// hard-killed mid-transaction. See ADR 0006.
	//
	// Loop failures never propagate here. A server that stops accepting Results
	// because NATS is unwell is strictly worse than one that keeps recording them
	// for the relay to publish when NATS returns.
	controllerManager := controllers.NewManager()
	_, err = controllers.Register(controllerManager, appCli.Controllers, controllers.Dependencies{
		DB:        db,
		Store:     store,
		Publisher: publisher,
		Channels:  channels,
		MaxJobAge: appCli.NATS.MaxJobAge,

		WorkerRetention: appCli.WorkerRetention,
		// Reuses the log-streaming connection rather than opening a third: an
		// advisory subscription is idle almost all the time, and the traffic it
		// competes with is a browser tailing a run.
		Advisories: controllers.AdvisoryWatcherFor(natsConn, urth.NewAdvisoryRecorder(db, store)),
	})
	grace.SuccessRequired(err, "failed to compose the control loops")

	// Added directly rather than through controllers.Register: that package
	// composes the *dispatch* loops, and worker liveness is not one of them. It
	// still wants the manager's supervision, so a panic recording presence
	// restarts the watcher instead of taking the API server down.
	if presenceWatcher != nil {
		grace.SuccessRequired(controllerManager.Add("worker-presence", presenceWatcher),
			"failed to register the worker presence watcher")
	}

	controllerManager.Start(ctx)

	if names := controllerManager.Names(); len(names) > 0 {
		log.Printf("control loops running in this process: %v", names)
	} else {
		// Worth saying out loud: every loop disabled on every replica is a
		// deployment where nothing repairs anything, and the symptom is runs that
		// simply never move.
		log.Print("no control loops are running in this process")
	}

	api := urth.NewService(store, scheduler, serviceOptions...)
	router := apiRoutes(api, natsConn, metricsRegistry(db, scheduler, placementMetrics))

	server := &http.Server{
		Addr:              listenAddress(),
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverStopped := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", server.Addr)
		serverStopped <- server.ListenAndServe()
	}()

	select {
	case err := <-serverStopped:
		// The listener failed on its own -- a port already held by an earlier
		// api-server is the usual reason -- so there is nothing to shut down
		// gracefully.
		grace.FatalOnError(err)
	case <-ctx.Done():
		log.Print("shutting down")

		// Requests are drained before the loops are waited on, because an
		// in-flight claim is exactly the kind of work a scan should observe
		// finished rather than abandoned.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), appCli.Controllers.ShutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("http server did not shut down cleanly: %v", err)
		}

		if err := controllerManager.Wait(appCli.Controllers.ShutdownTimeout); err != nil {
			log.Printf("control loops did not stop cleanly: %v", err)
		}
	}
}
