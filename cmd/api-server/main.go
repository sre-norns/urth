// Command api-server hosts the Urth control plane: the REST API, the dispatch
// outbox relay, and the reconciler.
//
// Everything it composes lives in pkg/apiserver. What is left here is what only
// a process can own: flags, a database connection, a listener, and a shutdown.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/alecthomas/kong"
	_ "github.com/joho/godotenv/autoload"
	"gorm.io/gorm"

	"github.com/sre-norns/urth/pkg/apiserver"
	"github.com/sre-norns/wyrd/pkg/grace"
	// dlogger "gorm.io/gorm/logger"
)

var appCli apiserver.Config

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
	grace.SuccessRequired(db.AutoMigrate(apiserver.Models()...), "DB schema migration failed")

	api, err := apiserver.New(ctx, db, appCli)
	grace.SuccessRequired(err, "failed to compose the API server")
	defer api.Close()

	api.Start(ctx)

	server := &http.Server{
		Addr:              listenAddress(),
		Handler:           api.Router,
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

		if err := api.Wait(appCli.Controllers.ShutdownTimeout); err != nil {
			log.Printf("control loops did not stop cleanly: %v", err)
		}
	}
}
