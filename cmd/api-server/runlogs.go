package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"

	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// runLogHandler serves a run's log, live if it is still running and from the
// stored artifact if it is not.
//
// One URL for both, deliberately. A client watching a run should not have to
// notice that it finished and switch to a different endpoint -- and it cannot
// do that reliably anyway, because the run may finish between the check and the
// request.
//
// conn may be nil, when the server is running on the asynq transport. Live
// tailing is then unavailable and only finished runs can be read.
func runLogHandler(srv urth.Service, conn *nats.Conn) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var request urth.ScenarioRunResultsRequest
		if err := ctx.ShouldBindUri(&request); err != nil {
			bark.AbortWithError(ctx, http.StatusNotFound, err)
			return
		}

		resource, found, err := srv.Results(manifest.ResourceName(request.ID)).
			Get(ctx.Request.Context(), manifest.ResourceName(request.RunID))
		if err != nil {
			bark.AbortWithError(ctx, http.StatusInternalServerError, err)
			return
		}
		if !found {
			bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
			return
		}

		// A finished run has its log as an artifact; there is nothing to tail.
		if isTerminal(resource.Status.Status) {
			serveStoredRunLog(ctx, srv, resource)
			return
		}

		if conn == nil {
			bark.AbortWithError(ctx, http.StatusServiceUnavailable,
				fmt.Errorf("live run logs require the NATS transport"))
			return
		}

		// A run that has not been claimed has no executor yet, so there is
		// nobody to listen to.
		if resource.Status.Executor.RunnerID == "" {
			bark.AbortWithError(ctx, http.StatusConflict,
				fmt.Errorf("run has not been claimed by a worker yet"))
			return
		}

		streamRunLog(ctx, conn, resource)
	}
}

func isTerminal(status urth.JobStatus) bool {
	switch status {
	case urth.JobCompleted, urth.JobErrored, urth.JobExpired:
		return true
	default:
		return false
	}
}

// serveStoredRunLog writes the log artifact a finished run left behind.
func serveStoredRunLog(ctx *gin.Context, srv urth.Service, result urth.Result) {
	// Found by label rather than by a constructed name: the two workers name
	// their artifacts differently -- one after the Result's name, one after its
	// UID -- and the labels are server-derived, so they are the same either way.
	selector, err := manifest.ParseSelector(fmt.Sprintf("%s=%s,%s=%s",
		urth.LabelResultUID, result.UID,
		urth.LabelArtifactKind, runner.LogRelType))
	if err != nil {
		bark.AbortWithError(ctx, http.StatusInternalServerError, err)
		return
	}

	artifacts, _, err := srv.Artifacts().List(ctx.Request.Context(), manifest.SearchQuery{Selector: selector})
	if err != nil {
		bark.AbortWithError(ctx, http.StatusInternalServerError, err)
		return
	}
	if len(artifacts) == 0 {
		bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
		return
	}

	content, found, err := srv.Artifacts().GetContent(ctx.Request.Context(), artifacts[0].Metadata.Name)
	if err != nil {
		bark.AbortWithError(ctx, http.StatusInternalServerError, err)
		return
	}
	if !found {
		bark.AbortWithError(ctx, http.StatusNotFound, bark.ErrResourceNotFound)
		return
	}

	writeSSEHeaders(ctx)
	writeSSEData(ctx, content.Artifact.Content)
	writeSSEEvent(ctx, "end", []byte(string(result.Status.Status)))
	ctx.Writer.Flush()
}

// streamRunLog relays a running job's output to the client as it arrives.
func streamRunLog(ctx *gin.Context, conn *nats.Conn, result urth.Result) {
	subscriber, err := natsq.SubscribeRunLog(conn, result.Status.Executor.RunnerID, result.UID, 0)
	if err != nil {
		bark.AbortWithError(ctx, http.StatusInternalServerError, err)
		return
	}
	defer subscriber.Close()

	writeSSEHeaders(ctx)
	ctx.Writer.Flush()

	// Bounded, because this holds a NATS subscription and an HTTP connection
	// open. A run cannot outlive its own lease, and a client that still wants
	// to watch can reconnect -- at which point, if the run has finished, it
	// gets the stored artifact instead.
	deadline := time.After(maxLogStreamDuration)
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	clientGone := ctx.Request.Context().Done()

	for {
		select {
		case line := <-subscriber.Lines():
			writeSSEData(ctx, line)
			ctx.Writer.Flush()

		case <-keepalive.C:
			// A comment frame. Without it an idle proxy between here and the
			// browser may close a connection that is merely quiet because the
			// probe is waiting on something.
			if _, err := io.WriteString(ctx.Writer, ": keepalive\n\n"); err != nil {
				return
			}
			ctx.Writer.Flush()

		case <-deadline:
			writeSSEEvent(ctx, "end", []byte("stream deadline reached"))
			ctx.Writer.Flush()
			return

		case <-clientGone:
			return
		}
	}
}

// maxLogStreamDuration bounds one live tail.
const maxLogStreamDuration = 30 * time.Minute

func writeSSEHeaders(ctx *gin.Context) {
	ctx.Header(bark.HTTPHeaderContentType, "text/event-stream")
	ctx.Header(bark.HTTPHeaderCacheControl, "no-store")
	ctx.Header("Connection", "keep-alive")
	// Proxies that buffer defeat the point of streaming.
	ctx.Header("X-Accel-Buffering", "no")
}

// writeSSEData emits a data frame, splitting on newlines as the format requires.
func writeSSEData(ctx *gin.Context, payload []byte) {
	if len(payload) == 0 {
		return
	}

	for _, line := range splitLines(payload) {
		if _, err := fmt.Fprintf(ctx.Writer, "data: %s\n", line); err != nil {
			return
		}
	}

	if _, err := io.WriteString(ctx.Writer, "\n"); err != nil {
		log.Print("failed to terminate SSE frame: ", err)
	}
}

func writeSSEEvent(ctx *gin.Context, event string, payload []byte) {
	fmt.Fprintf(ctx.Writer, "event: %s\n", event)
	writeSSEData(ctx, payload)
}

// splitLines breaks payload on newlines, dropping a single trailing empty
// element so a log chunk ending in "\n" does not produce a blank data line --
// which would read as a spurious empty line in the client's output.
func splitLines(payload []byte) [][]byte {
	lines := make([][]byte, 0, 8)
	start := 0

	for i := 0; i < len(payload); i++ {
		if payload[i] != '\n' {
			continue
		}

		line := payload[start:i]
		// Tolerate CRLF, which a subprocess on Windows may produce.
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		lines = append(lines, line)
		start = i + 1
	}

	if start < len(payload) {
		lines = append(lines, payload[start:])
	}

	return lines
}
