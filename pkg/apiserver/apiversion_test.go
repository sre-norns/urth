package apiserver

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/stretchr/testify/require"
)

func TestResourceWritesRequireCanonicalGroup(t *testing.T) {
	router := gin.New()
	router.POST("/runners", bark.ManifestAPI(urth.KindRunner), supportedAPIVersion(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	for _, version := range []string{urth.APIVersion, "v1", "", "identity.sre-norns.com/v1"} {
		t.Run(version, func(t *testing.T) {
			body := fmt.Sprintf(`{"apiVersion":%q,"kind":"runners","metadata":{"name":"edge"},"spec":{"active":true}}`, version)
			req := httptest.NewRequest(http.MethodPost, "/runners", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if version == urth.APIVersion {
				require.Equal(t, http.StatusNoContent, response.Code)
			} else {
				require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			}
		})
	}
}
