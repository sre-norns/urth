package client

import (
	"net/http"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// APIClientConfig is the connection a client makes: which server, as whom, and
// in which account and project unless a request's context names others.
type APIClientConfig struct {
	Account    manifest.ResourceID `help:"Account for runner and worker operations"`
	Project    manifest.ResourceID `help:"Project for scenarios, runs, artifacts and grants"`
	HTTPClient *http.Client        `kong:"-"`

	Token            urth.APIToken `help:"API token to authenticate to the API server"`
	APIServerAddress string        `help:"URL of the API server" default:"http://localhost:8080"`
	Timeout          time.Duration `help:"Communication timeout for API server" default:"1m"`
}

func (c *APIClientConfig) NewClient() (*RestAPIClient, error) {
	return NewRestAPIClient(c.APIServerAddress, *c)
}
