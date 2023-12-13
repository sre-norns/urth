package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/sre-norns/urth/pkg/dbstore"
	"github.com/sre-norns/urth/pkg/grace"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const paginationLimit = 512

var (
	ErrUnsupportedMediaType = fmt.Errorf("unsupported content type request")
	ErrInvalidAuthHeader    = fmt.Errorf("invalid Authorization header")
	ErrWrongApiKind         = fmt.Errorf("invalid resource kind for the API")
)

const (
	responseMarshalKey = "responseMarshal"
	searchQueryKey     = "searchQuery"
	resourceIdKey      = "resourceId"
	versionedIdKey     = "versionedId"
	versionInfoKey     = "versionInfoKey"

	resourceManifestKey = "resourceManifestKey"

	authBearerKey = "Bearer"
)

func filterFlags(content string) string {
	for i, char := range content {
		if char == ' ' || char == ';' {
			return content[:i]
		}
	}
	return content
}

func selectAcceptedType(header http.Header) []string {
	accepts := header.Values("Accept")
	result := make([]string, 0, len(accepts))
	for _, a := range accepts {
		result = append(result, filterFlags(a))
	}

	return result
}

type responseHandler func(code int, obj any)

func replyWithAcceptedType(c *gin.Context) (responseHandler, error) {
	for _, contentType := range selectAcceptedType(c.Request.Header) {
		switch contentType {
		case "", "*/*", gin.MIMEJSON:
			return c.JSON, nil
		case gin.MIMEYAML, "text/yaml", "application/yaml", "text/x-yaml":
			return c.YAML, nil
		case gin.MIMEXML, gin.MIMEXML2:
			return c.XML, nil
		}
	}

	return nil, ErrUnsupportedMediaType
}

func marshalResponse(ctx *gin.Context, code int, responseValue any) {
	marshalResponse := ctx.MustGet(responseMarshalKey).(responseHandler)
	marshalResponse(code, responseValue)
}

func abortWithError(ctx *gin.Context, code int, errValue error) {
	if apiError, ok := errValue.(*urth.ErrorResponse); ok {
		ctx.AbortWithStatusJSON(apiError.Code, apiError)
		return
	}

	ctx.AbortWithStatusJSON(code, urth.NewErrorResponse(code, errValue))
}

func contentTypeApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// select response encoder base of accept-type:
		marshalResponse, err := replyWithAcceptedType(ctx)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorResponse(http.StatusBadRequest, err))
			return
		}

		ctx.Set(responseMarshalKey, marshalResponse)
		ctx.Next()
	}
}

func resourceIdApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var resourceRequest urth.ResourceRequest
		if err := ctx.BindUri(&resourceRequest); err != nil {
			abortWithError(ctx, http.StatusNotFound, err)
			return
		}

		ctx.Set(resourceIdKey, resourceRequest)
		ctx.Next()
	}
}

func versionedResourceApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var versionInfo urth.VersionQuery
		if err := ctx.ShouldBindQuery(&versionInfo); err != nil {
			abortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		if resourceId, ok := ctx.Get(resourceIdKey); ok {
			ctx.Set(versionedIdKey, urth.NewVersionedId(resourceId.(urth.ResourceRequest).ID, versionInfo.Version))
		}

		ctx.Set(versionInfoKey, versionInfo)
		ctx.Next()
	}
}

func searchableApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var searchQuery urth.SearchQuery
		if ctx.ShouldBindQuery(&searchQuery) != nil {
			searchQuery.Limit = paginationLimit
		}
		searchQuery.ClampLimit(paginationLimit) // Redundant, now that the server clamps it
		ctx.Set(searchQueryKey, searchQuery)
		ctx.Next()
	}
}

func extractAuthBearer(ctx *gin.Context) (urth.ApiToken, error) {
	// Get the "Authorization" header
	authorization := ctx.Request.Header.Get("Authorization")
	if authorization == "" {
		return "", ErrInvalidAuthHeader
	}

	// Split it into two parts - "Bearer" and token
	parts := strings.SplitN(authorization, " ", 2)
	if parts[0] != "Bearer" {
		return "", ErrInvalidAuthHeader
	}

	return urth.ApiToken(parts[1]), nil
}

func authBearerApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		token, err := extractAuthBearer(ctx)
		if err != nil {
			abortWithError(ctx, http.StatusUnauthorized, err)
			return
		}

		ctx.Set(authBearerKey, token)
		ctx.Next()
	}
}

// Monkey-patch GIN to respect other spelling of yaml mime-type
func bindingFor(method, contentType string) binding.Binding {
	switch contentType {
	case gin.MIMEYAML, "text/yaml", "application/yaml", "text/x-yaml":
		return binding.YAML
	case "", "*/*", gin.MIMEJSON:
		return binding.JSON
	default:
		return binding.Default(method, contentType)
	}
}

func manifestApi(kind wyrd.Kind) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		manifest := wyrd.ResourceManifest{
			TypeMeta: wyrd.TypeMeta{
				Kind: kind, // Assume correct kind in case of run-triggers with min info
			},
		}
		if err := ctx.ShouldBindWith(&manifest, bindingFor(ctx.Request.Method, ctx.ContentType())); err != nil {
			abortWithError(ctx, http.StatusBadRequest, err)
			return
		}

		// d, _ := json.MarshalIndent(manifest, "", "  ")
		// os.Stdout.Write(d)

		if manifest.Kind == "" {
			manifest.Kind = kind
		} else if manifest.Kind != kind { // validate that API request is for correct manifest type:
			abortWithError(ctx, http.StatusBadRequest, ErrWrongApiKind)
			return
		}

		ctx.Set(resourceManifestKey, manifest)
		ctx.Next()
	}
}

func requireManifest(ctx *gin.Context) wyrd.ResourceManifest {
	return ctx.MustGet(resourceManifestKey).(wyrd.ResourceManifest)
}

// TODO: JWT Auth middleware for runners!

func apiRoutes(srv urth.Service) *gin.Engine {
	router := gin.Default()

	// Simple group: v1
	v1 := router.Group("api/v1")
	{
		v1.GET("/version", func(ctx *gin.Context) {
			bi, ok := debug.ReadBuildInfo()
			if !ok {
				ctx.JSON(http.StatusOK, gin.H{
					"version": "unknown",
				})
				return
			}

			ctx.JSON(http.StatusOK, gin.H{
				"version":   bi.Main.Version,
				"goVersion": bi.GoVersion,
			})
		})

		// TODO: Extract labels from JSON field
		// v1.GET("/labels", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
		// 	searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

		// 	results, err := srv.GetLabels().List(ctx.Request.Context(), searchQuery)
		// 	if err != nil {
		// 		abortWithError(ctx, http.StatusBadRequest, err)
		// 		return
		// 	}

		// 	marshalResponse(ctx, http.StatusOK, urth.PaginatedResponse[urth.ResourceLabel]{
		// 		Pagination: searchQuery.Pagination,
		// 		Count:      len(results),
		// 		Data:       results,
		// 	})
		// })

		// Auth API for runners. TODO: Should it be something like `/auth/runner` ?
		v1.PUT("/runners", contentTypeApi(), authBearerApi(), func(ctx *gin.Context) {
			var newEntry urth.RunnerRegistration
			if err := ctx.ShouldBind(&newEntry); err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			token := ctx.MustGet(authBearerKey).(urth.ApiToken)

			result, err := srv.GetRunnerAPI().Auth(ctx.Request.Context(), token, newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header("Cache-Control", "no-store")
			marshalResponse(ctx, http.StatusAccepted, result)
		})

		//------------
		// Runners API
		//------------
		v1.GET("/runners", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

			results, err := srv.GetRunnerAPI().List(ctx.Request.Context(), searchQuery)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})

		v1.POST("/runners", contentTypeApi(), manifestApi(urth.KindRunner), func(ctx *gin.Context) {
			newEntry := requireManifest(ctx)
			result, err := srv.GetRunnerAPI().Create(ctx.Request.Context(), newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header("Location", fmt.Sprintf("%v/%v", ctx.Request.URL.Path, result.ID))
			marshalResponse(ctx, http.StatusCreated, result)
		})
		v1.GET("/runners/:id", contentTypeApi(), resourceIdApi(), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)
			resource, exists, err := srv.GetRunnerAPI().Get(ctx.Request.Context(), resourceId.ID)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			if !exists {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		v1.PUT("/runners/:id", contentTypeApi(), resourceIdApi(), versionedResourceApi(), manifestApi(urth.KindRunner), func(ctx *gin.Context) {
			versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)
			newEntry := requireManifest(ctx)

			updateResponse, err := srv.GetRunnerAPI().Update(ctx.Request.Context(), versionedId, newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusCreated, updateResponse)
		})

		v1.DELETE("/runners/:id", resourceIdApi(), versionedResourceApi(), func(ctx *gin.Context) {
			versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)

			// Note: Should Delete be is silent? - no error if deleting non-existing resource
			existed, err := srv.GetRunnerAPI().Delete(ctx.Request.Context(), versionedId)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !existed {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			ctx.Status(http.StatusNoContent)
		})
		//------------
		// Scenarios API
		//------------

		v1.GET("/scenarios", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

			results, err := srv.GetScenarioAPI().List(ctx.Request.Context(), searchQuery)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})

		v1.POST("/scenarios", contentTypeApi(), manifestApi(urth.KindScenario), func(ctx *gin.Context) {
			newEntry := requireManifest(ctx)
			result, err := srv.GetScenarioAPI().Create(ctx.Request.Context(), newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header("Location", fmt.Sprintf("%v/%v", ctx.Request.URL.Path, result.ID))
			marshalResponse(ctx, http.StatusCreated, result)
		})

		v1.GET("/scenarios/:id", contentTypeApi(), resourceIdApi(), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)

			resource, exists, err := srv.GetScenarioAPI().Get(ctx.Request.Context(), resourceId.ID)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !exists {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		v1.DELETE("/scenarios/:id", resourceIdApi(), versionedResourceApi(), func(ctx *gin.Context) {
			versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)

			existed, err := srv.GetScenarioAPI().Delete(ctx.Request.Context(), versionedId)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !existed {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			ctx.Status(http.StatusNoContent)
		})

		v1.PUT("/scenarios/:id", contentTypeApi(), resourceIdApi(), versionedResourceApi(), manifestApi(urth.KindScenario), func(ctx *gin.Context) {
			versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)
			newEntry := requireManifest(ctx)

			updateResponse, err := srv.GetScenarioAPI().Update(ctx.Request.Context(), versionedId, newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusCreated, updateResponse)
		})

		v1.GET("/scenarios/:id/script", resourceIdApi(), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)

			resource, exists, err := srv.GetScenarioAPI().Get(ctx.Request.Context(), resourceId.ID)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !exists {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			scenario, ok := resource.Spec.(*urth.ScenarioSpec)
			if !ok || scenario == nil || scenario.Prob.Spec == nil {
				abortWithError(ctx, http.StatusNotFound, fmt.Errorf("scenario %w", urth.ErrResourceSpecIsNil))
				return
			}

			data, ok := scenario.Prob.Spec.(map[string]any)
			if !ok || data == nil || len(data) == 0 {
				abortWithError(ctx, http.StatusNotFound, fmt.Errorf("prob spec %q is %w", reflect.TypeOf(scenario.Prob.Spec), urth.ErrResourceSpecIsNil))
				return
			}

			scriptData, ok := data["Script"]
			if !ok {
				abortWithError(ctx, http.StatusNotFound, fmt.Errorf("prod has no 'Script' field"))
				return
			}
			script, ok := scriptData.(string)
			if !ok {
				abortWithError(ctx, http.StatusNotFound, fmt.Errorf("prod 'Script' field is not a string"))
				return
			}

			ctx.Header("Content-Type", gin.MIMEPlain)
			ctx.Writer.Write([]byte(script))
		})

		/*
			v1.PUT("/scenarios/:id/script", resourceIdApi(), contentTypeApi(), versionedResourceApi(), func(ctx *gin.Context) {
				versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)

				// Considers streaming data to a blob storage
				data, err := ctx.GetRawData()
				if err != nil {
					abortWithError(ctx, http.StatusBadRequest, err)
					return
				}

				result, exists, err := srv.GetScenarioAPI().UpdateScript(ctx.Request.Context(), versionedId, urth.ScenarioScript{
					Kind:    urth.GuessScenarioKind(ctx.Query("kind"), ctx.ContentType(), data),
					Content: data,
				})
				if err != nil {
					abortWithError(ctx, http.StatusBadRequest, err)
					return
				}
				if !exists {
					abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
					return
				}

				marshalResponse(ctx, http.StatusCreated, result)
			})
		*/
		// DELETE script ? => UpdateScript("")

		//------------
		// Scenario run results API
		//------------

		v1.GET("/scenarios/:id/results", searchableApi(), contentTypeApi(), resourceIdApi(), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)
			results, err := srv.GetResultsAPI(resourceId.ID).List(ctx.Request.Context(), searchQuery)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})
		v1.POST("/scenarios/:id/results", contentTypeApi(), resourceIdApi(), manifestApi(urth.KindResult), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)
			newEntry := requireManifest(ctx)

			result, err := srv.GetResultsAPI(resourceId.ID).Create(ctx.Request.Context(), newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header("Location", fmt.Sprintf("%v/%v", ctx.Request.URL.Path, result.ID))
			marshalResponse(ctx, http.StatusCreated, result)
		})

		v1.GET("/scenarios/:id/results/:runId", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				abortWithError(ctx, http.StatusNotFound, err)
				return
			}

			resource, exists, err := srv.GetResultsAPI(resourceRequest.ID).Get(ctx.Request.Context(), resourceRequest.RunId)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			if !exists {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		v1.POST("/scenarios/:id/results/:runId/auth", contentTypeApi(), versionedResourceApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				abortWithError(ctx, http.StatusNotFound, err)
				return
			}

			versionedInfo := ctx.MustGet(versionInfoKey).(urth.VersionQuery)

			var authRequest urth.AuthJobRequest
			if err := ctx.ShouldBind(&authRequest); err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			resource, err := srv.GetResultsAPI(resourceRequest.ID).Auth(ctx.Request.Context(), urth.NewVersionedId(resourceRequest.RunId, versionedInfo.Version), authRequest)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header("Cache-Control", "no-store")
			marshalResponse(ctx, http.StatusOK, resource)
		})

		v1.PUT("/scenarios/:id/results/:runId", contentTypeApi(), authBearerApi(), versionedResourceApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				abortWithError(ctx, http.StatusNotFound, err)
				return
			}

			versionInfo := ctx.MustGet(versionInfoKey).(urth.VersionQuery)

			// FIXME: Should require valid JWT with exp claim validated
			token := ctx.MustGet(authBearerKey).(urth.ApiToken)

			var newEntry urth.FinalRunResults
			if err := ctx.ShouldBind(&newEntry); err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			resource, err := srv.GetResultsAPI(resourceRequest.ID).Update(ctx.Request.Context(), urth.NewVersionedId(resourceRequest.RunId, versionInfo.Version), urth.ApiToken(token), newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		//------------
		// Artifacts API
		//------------
		v1.GET("/artifacts", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

			results, err := srv.GetArtifactsApi().List(ctx.Request.Context(), searchQuery)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})

		// FIXME: Require valid worker auth / JWT
		v1.POST("/artifacts", contentTypeApi() /*authBearerApi(),*/, manifestApi(urth.KindArtifact), func(ctx *gin.Context) {
			newEntry := requireManifest(ctx)

			// Considers streaming data to a blob storage
			result, err := srv.GetArtifactsApi().Create(ctx.Request.Context(), newEntry)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			ctx.Header("Location", fmt.Sprintf("%v/%v", ctx.Request.URL.Path, result.ID))
			marshalResponse(ctx, http.StatusCreated, result)
		})

		v1.GET("/artifacts/:id", contentTypeApi(), resourceIdApi(), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)
			resource, exists, err := srv.GetArtifactsApi().Get(ctx.Request.Context(), resourceId.ID)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			if !exists {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			// TODO: Find a better way to not-expand content
			artifact := resource.Spec.(urth.ArtifactSpec) // FIXME: Unchecked cast!
			artifact.Content = nil
			marshalResponse(ctx, http.StatusOK, resource)
		})
		v1.GET("/artifacts/:id/content", contentTypeApi(), resourceIdApi(), func(ctx *gin.Context) {
			resourceId := ctx.MustGet(resourceIdKey).(urth.ResourceRequest)
			resource, exists, err := srv.GetArtifactsApi().GetContent(ctx.Request.Context(), resourceId.ID)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}

			if !exists {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			ctx.Writer.Header().Set("Content-Type", resource.MimeType)
			ctx.Writer.Write(resource.Content)
		})

		// TODO: POST("/artifacts/:id/content") ???

		// FIXME: Should you be able to delete an artifact. It should auto-expire
		v1.DELETE("/artifacts/:id", resourceIdApi(), versionedResourceApi(), func(ctx *gin.Context) {
			versionedId := ctx.MustGet(versionedIdKey).(urth.VersionedResourceId)

			existed, err := srv.GetArtifactsApi().Delete(ctx.Request.Context(), versionedId)
			if err != nil {
				abortWithError(ctx, http.StatusBadRequest, err)
				return
			}
			if !existed {
				abortWithError(ctx, http.StatusNotFound, urth.ErrResourceNotFound)
				return
			}

			ctx.Status(http.StatusNoContent)
		})
	}

	return router
}

func main() {
	// TODO: Init DB connection string from env? or config
	db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	// Migrate the schema (TODO: should be limited to dev env only)
	if err = db.AutoMigrate(
		&urth.Scenario{},
		&urth.Runner{},
		&urth.Result{},
		&urth.Artifact{},
	); err != nil {
		panic(fmt.Sprintf("DB migration failed: %v", err))
	}

	// Init service
	store := dbstore.NewDbStore(db)
	// scheduler, err := gqueue.NewScheduler(context.TODO(), "test-local-321", "prob-request")
	scheduler, err := redqueue.NewScheduler(context.TODO(), "localhost:6379")
	if err != nil {
		log.Fatalln("Failed to create scheduler: ", err)
	}
	defer scheduler.Close()

	api := urth.NewService(store, scheduler)
	router := apiRoutes(api)

	grace.ExitOrLog(router.Run()) // listen and serve on 0.0.0.0:8080
}
