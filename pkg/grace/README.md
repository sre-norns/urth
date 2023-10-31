# Grace
A utilities package for graceful handling of OS signals.

For any service / process that needs to play nice with Kubernetes and handle termination signals gracefully using Go-lang context.
```go

func main() {
    // Setup proper K8s signal handlers for graceful termination
    mainContext := grace.SetupSignalHandler()
    ....
    // Use mainContext

    // mainContext - will be canceled by SIGTERM or SIGINT sent by a runtime
}

```
