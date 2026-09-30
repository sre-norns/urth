package client

import (
	"context"
	"path"
	"strings"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

type labelsAPIRestClient struct {
	RestAPIClient

	kind manifest.Kind
}

// search lists the strings under /search/:kind/<segments...>.
func (m *labelsAPIRestClient) search(ctx context.Context, searchQuery manifest.SearchQuery, segments ...string) (manifest.StringSet, manifest.Page, error) {
	kind := strings.ToLower(string(m.kind))
	targetAPI := urlForPath(m.baseURL, path.Join(append([]string{"v1", "search", kind}, segments...)...), searchToQuery(searchQuery))

	l, total, err := listResources[string](ctx, &m.RestAPIClient, targetAPI)
	var result manifest.StringSet
	if err == nil {
		result = manifest.NewStringSet(l...)
	}

	return result, total, err
}

func (m *labelsAPIRestClient) ListNames(ctx context.Context, searchQuery manifest.SearchQuery) (manifest.StringSet, manifest.Page, error) {
	return m.search(ctx, searchQuery, "names")
}

func (m *labelsAPIRestClient) ListLabels(ctx context.Context, searchQuery manifest.SearchQuery) (manifest.StringSet, manifest.Page, error) {
	return m.search(ctx, searchQuery, "labels")
}

func (m *labelsAPIRestClient) ListLabelValues(ctx context.Context, label string, searchQuery manifest.SearchQuery) (manifest.StringSet, manifest.Page, error) {
	return m.search(ctx, searchQuery, "labels", label)
}
