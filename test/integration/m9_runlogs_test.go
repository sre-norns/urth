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

func TestM9LiveLogsCloseWhenAccessChanges(t *testing.T) {
	for _, change := range []string{"membership", "session", "expiry"} {
		t.Run(change, func(t *testing.T) {
			h := newHarness(t)
			runner := h.applyRunner("revoked-logs-runner", nil)
			scenario := h.applyScenario("revoked-logs", testProbSpec{}, manifest.LabelSelector{})
			run := h.createRun(scenario.Name)
			ctx, cancel := context.WithTimeout(h.ctx, 8*time.Second)
			defer cancel()
			path := fmt.Sprintf("/v1/projects/%s/scenarios/%s/results/%s/logs", h.scope.Project, scenario.Name, run.Name)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.HTTP.URL+path, nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+string(h.token))
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			require.Equal(t, http.StatusOK, res.StatusCode)
			conn, err := nats.Connect(h.natsURL())
			require.NoError(t, err)
			defer conn.Close()
			lines := make(chan string, 16)
			done := make(chan error, 1)
			go func() {
				scanner := bufio.NewScanner(res.Body)
				for scanner.Scan() {
					if strings.HasPrefix(scanner.Text(), "data:") {
						lines <- scanner.Text()
					}
				}
				done <- scanner.Err()
			}()
			publisher := natsq.NewLogPublisher(conn, runner.UID, run.UID)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
		firstLine:
			for {
				select {
				case line := <-lines:
					require.Equal(t, "data: before", line)
					break firstLine
				case <-ticker.C:
					publisher.PublishLine([]byte("before"))
				case <-ctx.Done():
					t.Fatal("initial authorized log was not delivered")
				}
			}
			principal, err := h.Server.Identity.Authenticate(h.ctx, string(h.token))
			require.NoError(t, err)
			switch change {
			case "membership":
				require.NoError(t, h.DB.Where("project_id = ?", h.scope.Project).Delete(&im.ProjectMembership{}).Error)
			case "session":
				require.NoError(t, h.DB.Where("id = ?", principal.CredentialID).Delete(&im.Session{}).Error)
			case "expiry":
				require.NoError(t, h.DB.Model(&im.Session{}).Where("id = ?", principal.CredentialID).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			}
			// Membership removal checks each emission; the other cases exercise idle closure.
			if change == "membership" {
				publisher.PublishLine([]byte("after"))
				require.NoError(t, conn.Flush())
			}
			timeout := time.NewTimer(3 * time.Second)
			defer timeout.Stop()
			for {
				select {
				case line := <-lines:
					require.NotEqual(t, "data: after", line, "revoked access must not receive new log data")
				case err := <-done:
					require.NoError(t, err)
					return
				case <-timeout.C:
					t.Fatal("stream remains open after access was revoked")
				}
			}
		})
	}
}
