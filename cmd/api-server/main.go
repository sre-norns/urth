package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"runtime/debug"

	"github.com/sre-norns/urth/pkg/grace"
	"github.com/sre-norns/urth/pkg/redqueue"
	"github.com/sre-norns/urth/pkg/urth"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const paginationLimit = 512

var (
	ErrResourceNotFound     = fmt.Errorf("requested resource not found")
	ErrUnsupportedMediaType = fmt.Errorf("unsupported content type request")
)

const (
	responseMarshalKey = "responseMarshal"
	searchQueryKey     = "searchQuery"
)

func selectAcceptedType(header http.Header) []string {
	// TODO: Clean-up headers tags!
	return header.Values("Accept")
}

type responseHandler func(code int, obj any)

func replyWithAcceptedType(c *gin.Context) (responseHandler, error) {
	for _, contentType := range selectAcceptedType(c.Request.Header) {
		switch contentType {
		case "", "*/*", gin.MIMEJSON:
			return c.JSON, nil
		case "text/yaml", "application/yaml", "text/x-yaml", gin.MIMEYAML:
			return c.YAML, nil
		case gin.MIMEXML, gin.MIMEXML2:
			return c.XML, nil
		}
	}

	return nil, ErrUnsupportedMediaType
}

func marshalResponse(ctx *gin.Context, code int, responseValue any) {
	marshalResponse := ctx.MustGet(responseMarshalKey).(responseHandler)
	marshalResponse(http.StatusOK, responseValue)
}

func contentTypeApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		// select response encoder base of accept-type:
		marshalResponse, err := replyWithAcceptedType(ctx)
		if err != nil {
			ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
			return
		}

		ctx.Set(responseMarshalKey, marshalResponse)
		ctx.Next()
	}
}

func searchableApi() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var searchQuery urth.SearchQuery
		if ctx.ShouldBindQuery(&searchQuery) != nil {
			searchQuery.Limit = paginationLimit
		}
		searchQuery.ClampLimit(paginationLimit)
		ctx.Set(searchQueryKey, searchQuery)
		ctx.Next()
	}
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

		v1.GET("/labels", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

			results, err := srv.GetLabels().List(ctx.Request.Context(), searchQuery)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.PaginatedResponse[urth.ResourceLabel]{
				Pagination: searchQuery.Pagination,
				Count:      len(results),
				Data:       results,
			})
		})

		//------------
		// Runners API
		//------------
		v1.GET("/runners", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

			results, err := srv.GetRunnerAPI().List(ctx.Request.Context(), searchQuery)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})

		v1.POST("/runners", contentTypeApi(), func(ctx *gin.Context) {
			var newEntry urth.CreateRunnerRequest
			if err := ctx.ShouldBind(&newEntry); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			result, err := srv.GetRunnerAPI().Create(ctx.Request.Context(), newEntry)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusCreated, result)
		})

		v1.GET("/runners/:id", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			resource, exists, err := srv.GetRunnerAPI().Get(ctx.Request.Context(), resourceRequest.ResourceID())
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			if !exists {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), ErrResourceNotFound))
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		//------------
		// Scenarios API
		//------------

		v1.GET("/scenarios", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)

			results, err := srv.GetScenarioAPI().List(ctx.Request.Context(), searchQuery)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})

		v1.POST("/scenarios", contentTypeApi(), func(ctx *gin.Context) {
			var newEntry urth.CreateScenarioRequest
			if err := ctx.ShouldBind(&newEntry); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			result, err := srv.GetScenarioAPI().Create(ctx.Request.Context(), newEntry)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusCreated, result)
		})

		v1.GET("/scenarios/:id", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.ShouldBindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), err))
				return
			}

			resource, exists, err := srv.GetScenarioAPI().Get(ctx.Request.Context(), resourceRequest.ResourceID())
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			if !exists {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), ErrResourceNotFound))
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		v1.DELETE("/scenarios/:id", func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			_, err := srv.GetScenarioAPI().Delete(ctx.Request.Context(), resourceRequest.ResourceID())
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			ctx.Status(http.StatusNoContent)
		})

		v1.PUT("/scenarios/:id", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			var newEntry urth.CreateScenario
			if err := ctx.ShouldBind(&newEntry); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			updateResponse, err := srv.GetScenarioAPI().Update(ctx.Request.Context(), resourceRequest.ResourceID(), newEntry)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusCreated, updateResponse)
		})

		v1.GET("/scenarios/:id/script", func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			resource, exists, err := srv.GetScenarioAPI().Get(ctx.Request.Context(), resourceRequest.ResourceID())
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			if !exists {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), ErrResourceNotFound))
				return
			}

			ctx.Status(http.StatusOK)
			ctx.Writer.Header().Set("Content-Type", urth.ScriptKindToMimeType(resource.Script.Kind))
			ctx.Writer.Write(resource.Script.Content)
		})

		v1.PUT("/scenarios/:id/script", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			data, err := ioutil.ReadAll(ctx.Request.Body)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			id, exists, err := srv.GetScenarioAPI().UpdateScript(ctx.Request.Context(), resourceRequest.ResourceID(), urth.ScenarioScript{
				Kind:    urth.GuessScenarioKind(ctx.Query("kind"), ctx.ContentType(), data),
				Content: data,
			})
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}
			if !exists {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), ErrResourceNotFound))
				return
			}

			marshalResponse(ctx, http.StatusCreated, urth.CreatedResponse{
				TypeMeta:            urth.TypeMeta{Kind: "scenario"},
				VersionedResourceId: id,
			})
		})

		//------------
		// Scenario run results API
		//------------

		v1.GET("/scenarios/:id/results", searchableApi(), contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			searchQuery := ctx.MustGet(searchQueryKey).(urth.SearchQuery)
			results, err := srv.GetResultsAPI(resourceRequest.ResourceID()).List(ctx.Request.Context(), searchQuery)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusOK, urth.NewPaginatedResponse(results, searchQuery.Pagination))
		})

		// Sneaky manual run trigger API
		v1.PUT("/scenarios/:id/results", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			var newEntry urth.CreateScenarioManualRunRequest
			if err := ctx.ShouldBind(&newEntry); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			result, exists, err := srv.ScheduleScenarioRun(ctx.Request.Context(), resourceRequest.ResourceID(), newEntry)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			if !exists {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), ErrResourceNotFound))
				return
			}

			marshalResponse(ctx, http.StatusCreated, result)
		})

		// TODO: Can only be posted by a runner with valid JWT
		v1.POST("/scenarios/:id/results", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ResourceRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			var newEntry urth.CreateScenarioRunResults
			if err := ctx.ShouldBind(&newEntry); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			result, err := srv.GetResultsAPI(resourceRequest.ResourceID()).Create(ctx.Request.Context(), newEntry)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusCreated, result)
		})

		v1.GET("/scenarios/:id/results/:runId", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			resource, exists, err := srv.GetResultsAPI(resourceRequest.ID).Get(ctx.Request.Context(), resourceRequest.RunResultsID)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			if !exists {
				ctx.AbortWithStatusJSON(http.StatusNotFound, urth.NewErrorRepose(http.StatusText(http.StatusNotFound), ErrResourceNotFound))
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
		})

		v1.PUT("/scenarios/:id/results/:runId", contentTypeApi(), func(ctx *gin.Context) {
			var resourceRequest urth.ScenarioRunResultsRequest
			if err := ctx.BindUri(&resourceRequest); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			var version uint32 = 1                  //ctx.Query("version")
			token := ctx.GetHeader("Authorization") // FIXME: Use JWT middleware!
			// FIXME: Get version from the tokens claims!

			var newEntry urth.FinalRunResults
			if err := ctx.ShouldBind(&newEntry); err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			resource, err := srv.GetResultsAPI(resourceRequest.ID).Update(ctx.Request.Context(), urth.VersionedResourceId{ID: resourceRequest.RunResultsID, Version: version}, urth.ApiToken(token), newEntry)
			if err != nil {
				ctx.AbortWithStatusJSON(http.StatusBadRequest, urth.NewErrorRepose(http.StatusText(http.StatusBadRequest), err))
				return
			}

			marshalResponse(ctx, http.StatusOK, resource)
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
		&urth.ScenarioRunResults{},
		&urth.ResourceLabelModel{},
		&urth.Artifact{},
	); err != nil {
		panic(fmt.Sprintf("DB migration failed: %v", err))
	}

	// Init service
	store := urth.NewDbStore(db)
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
