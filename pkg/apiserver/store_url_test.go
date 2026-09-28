package apiserver

import (
	"testing"

	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/stretchr/testify/require"
)

// The api-server opens its database from --store.url through wyrd's
// dbstore.Config, which names the dialect after the driver xo/dburl reports.
// From dburl v0.32.0 a postgres:// URL reports "pgx", which wyrd v0.3.0 does not
// recognise -- and a dependency update that moved dburl that far left the
// api-server unable to start against Postgres, with every test still green:
// the tests open their databases directly, never through a URL. This is the
// test that notices.
func TestStoreURLsOpenPostgres(t *testing.T) {
	for _, url := range []string{
		"postgres://urth:urth@localhost:5432/urth",
		"postgresql://urth:urth@localhost:5432/urth?sslmode=disable",
	} {
		dialector, err := dbstore.Config{URL: url}.Dialector()
		require.NoError(t, err, url)
		require.Equal(t, "postgres", dialector.Name(), url)
	}
}
