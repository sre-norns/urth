# Bark! 
(Gin-gonic)[github.com/gin-gonic/gin] reusable Rest API components that you need!

This go-module provides a number types, helpers and filters that are very handy when creating rich Rest API using popular Go web framework.

# Usage
Using search filters allows to have standard API fith filter labels and pagination:
```go
    // Define API that allows filters and returns paginated response
    api.GET("/artifacts", bark.SearchableApi(paginationLimit), ..., func(ctx *gin.Context) {
        // Extract search qeury from the context:
        searchQuery := bark.RequireSearchQuery(ctx)

        // Use the query in your API;
        results, err := service.GetArtifactsApi().List(ctx.Request.Context(), searchQuery)
        ... 
    })
```

# Filters
The library offers a collection of middleware / filter that streamline implementation of a service responsinble for a collection of resource. 

- `ContentTypeApi` - enables API to support different serialization formats, respecting [`Accept`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) HTTP Request headers.
- `SearchableApi` - enables APIs to support query filter and pagination.
- `AuthBearerApi` - enables APIs to read Auth Bearer token.
- `ResourceIdApi` - streamline implementation of APIs that serves a single resourse.
- `VersionedResourceApi` - enables API implementation that can support request to versioned resources.
- `ManifestApi` - helps to streamline implementaion of APIs that deail with `wyrd.ResourceManifest`



### Filters: `ContentTypeApi`
This function produces `gin.HandlerFunc` as middleware to add support for response marshaler selection based on [`Accept`](https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept) HTTP Request header.

It is expected to be used in conjunction with `bark.MarshalResponse` and `bark.ReplyResourceCreated` method that select recommended marshaler method to be used for http response.

For example, a client can call `/artifacts/:id` API with `Accept` header to inform the server what response type is expected back. Given valid request, for example:

```
GET /artifacts/:id HTTP/1.1
Host: localhost:8080
Accept: application/xml
```

The server would select XML as serialization method:
```
HTTP/1.1 200 OK
Content-Type: application/xml; charset=utf-8

....
```


#### Usage: 
```go 
    api.GET("/artifacts/:id", bark.ContentTypeApi(), func(ctx *gin.Context) {
        ...

        bark.MarshalResponse(ctx, http.StatusOK, resource) // NOTE: To use this method ContentTypeApi middleware is required
    })

    v1.POST("/artifacts", bark.ContentTypeApi(), func(ctx *gin.Context) {
        ...
        
        bark.ReplyResourceCreated(ctx, result.ID, result) // NOTE: To use this method ContentTypeApi middleware is required
    })

``` 

