// Package client is the REST client of the Urth API server. It implements
// urth.Service, so a worker or a test drives the server through the same
// interface the server implements.
package client

import (
	"net/http"
	"net/url"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

var _ urth.Service = (*RestAPIClient)(nil)

type RestAPIClient struct {
	baseURL *url.URL

	config APIClientConfig
}

func NewRestAPIClient(baseURL string, config APIClientConfig) (*RestAPIClient, error) {
	url, err := url.Parse(baseURL)

	if config.HTTPClient == nil {
		config.HTTPClient = http.DefaultClient
	}

	return &RestAPIClient{
		baseURL: url,
		config:  config,
	}, err
}

// Labels implements the urth.Service interface.
func (c *RestAPIClient) Labels(k manifest.Kind) urth.LabelsAPI {
	return &labelsAPIRestClient{
		RestAPIClient: *c,
		kind:          k,
	}
}

// Runners implements the urth.Service interface.
func (c *RestAPIClient) Runners() urth.RunnersAPI {
	return &runnersAPIClient{
		RestAPIClient: *c,
	}
}

// RunnerAuthorizations returns the project's runner grant collection.
func (c *RestAPIClient) RunnerAuthorizations() urth.RunnerAuthorizationsAPI {
	return &runnerGrantsClient{RestAPIClient: *c}
}

// Workers implements the urth.Service interface.
func (c *RestAPIClient) Workers() urth.WorkersAPI {
	return &workersAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) Scenarios() urth.ScenarioAPI {
	return &scenariosAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) Results(scenarioName manifest.ResourceName) urth.RunResultAPI {
	return &resultsAPIRestClient{
		RestAPIClient: *c,
		ScenarioID:    scenarioName,
	}
}

// AllResults implements the urth.Service interface.
func (c *RestAPIClient) AllResults() urth.RunResultsAPI {
	return &allResultsAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) Artifacts() urth.ArtifactAPI {
	return &artifactAPIClient{
		RestAPIClient: *c,
	}
}

func (c *RestAPIClient) DispatchFailures() urth.DispatchFailuresAPI {
	return &dispatchFailuresAPIClient{
		RestAPIClient: *c,
	}
}
