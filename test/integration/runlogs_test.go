package integration

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/sre-norns/urth/pkg/natsq"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestRunLogsRequireProjectSessionAndServeStoredArtifact(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("logs-runner", nil)
	scenario := h.applyScenario("logs-scenario", testProbSpec{Message: "m7 stored log"}, manifest.LabelSelector{})
	h.startWorker(runner.Name)
	run := h.createRun(scenario.Name)
	h.mustRelay(1)
	h.awaitTerminal(run.UID, 30*time.Second)
	path := fmt.Sprintf("/v1/projects/%s/scenarios/%s/results/%s/logs", h.scope.Project, scenario.Name, run.Name)
	code, _ := h.httpRequest(http.MethodGet, path, "", nil)
	require.Equal(t, http.StatusUnauthorized, code)
	var body []byte
	h.eventually(10*time.Second, "the completed run log artifact to be uploaded", func() bool {
		code, body = h.httpRequest(http.MethodGet, path, h.token, nil)
		return code == http.StatusOK
	})
	require.Contains(t, string(body), "m7 stored log")
	require.Contains(t, string(body), "event: end")
	// A valid session cannot use a different project path to address this UID.
	other, err := h.Server.Identity.Projects().Create(h.ctx, im.Project{Resource: im.Resource{Name: "other-log-project", AccountID: im.AccountID(h.scope.Account)}})
	require.NoError(t, err)
	otherPath := fmt.Sprintf("/v1/projects/%s/scenarios/%s/results/%s/logs", other.ID, scenario.Name, run.UID)
	code, body = h.httpRequest(http.MethodGet, otherPath, h.token, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, code, string(body))
	require.NotContains(t, string(body), "m7 stored log")
	// Removing membership blocks another request even for an account owner.
	require.NoError(t, h.DB.Where("project_id = ?", h.scope.Project).Delete(&im.ProjectMembership{}).Error)
	code, body = h.httpRequest(http.MethodGet, path, h.token, nil)
	require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, code, string(body))
}

func TestRunLogsStreamWithAuthorizationHeader(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("live-logs-runner", nil)
	scenario := h.applyScenario("live-logs", testProbSpec{}, manifest.LabelSelector{})
	run := h.createRun(scenario.Name)
	ctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()
	path := fmt.Sprintf("/v1/projects/%s/scenarios/%s/results/%s/logs", h.scope.Project, scenario.Name, run.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.HTTP.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+string(h.token))
	req.Header.Set("Accept", "text/event-stream")
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	require.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	require.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	conn, err := nats.Connect(h.natsURL())
	require.NoError(t, err)
	defer conn.Close()
	got := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(res.Body)
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "data:") {
				got <- scanner.Text()
				return
			}
		}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case line := <-got:
			require.Equal(t, "data: m7 live log", line)
			return
		case <-ticker.C:
			natsq.NewLogPublisher(conn, runner.UID, run.UID).PublishLine([]byte("m7 live log"))
		case <-ctx.Done():
			t.Fatal("authenticated stream did not receive the run's log")
		}
	}
}
