// Command nats-worker executes Urth scenarios, taking its jobs from a NATS
// JetStream queue owned by one runner.
//
// Everything it does lives in pkg/worker. What is left here is what only a
// process can own: flags, an enrolment secret read from its file, an API client,
// and a signal-aware context.
package main

import (
	"errors"

	"github.com/alecthomas/kong"
	_ "github.com/joho/godotenv/autoload"

	"github.com/sre-norns/urth/pkg/worker"
	"github.com/sre-norns/wyrd/pkg/grace"
)

var appConfig = worker.NewDefaultConfig()

func main() {
	kong.Parse(&appConfig,
		kong.Name("nats-worker"),
		kong.Description("Urth worker: claims scenarios from its runner's queue and executes them"),
	)

	token, err := appConfig.EnrolmentToken()
	grace.SuccessRequired(err, "failed to read enrolment token")
	if token == "" {
		grace.SuccessRequired(errors.New("no enrolment token provided"),
			"an enrolment token is required: pass --token, --token-file, or set the token in the environment")
	}

	apiClient, err := appConfig.NewClient()
	grace.SuccessRequired(err, "failed to initialize API client")

	// Signal-aware context, so a shutdown request drains in-flight runs rather
	// than abandoning results the server is waiting for.
	ctx := grace.NewSignalHandlingContext()

	grace.SuccessRequired(worker.New(&appConfig, apiClient, token).Run(ctx), "worker terminated")
}
