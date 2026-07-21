package urth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/wyrd/pkg/dbstore"
)

type recordingSaver struct {
	saved any
	calls int
}

func (r *recordingSaver) CreateOrUpdate(_ context.Context, value any, _ ...dbstore.Option) (bool, error) {
	r.saved = value
	r.calls++

	return true, nil
}

// saveResource must go through CreateOrUpdate, which maps to gorm's Save.
//
// The alternative, dbstore.Update, hands the struct to gorm's Updates and skips
// every zero-valued field -- so a bool set to false is silently dropped. That is
// not a hypothetical: disabling a scenario or a runner reported success and
// changed nothing, and a paused worker could never be resumed.
func TestSaveResourceWritesThroughCreateOrUpdate(t *testing.T) {
	store := &recordingSaver{}

	runner := Runner{Spec: RunnerSpec{IsActive: false}}
	require.NoError(t, saveResource(context.Background(), store, &runner))

	require.Equal(t, 1, store.calls)
	require.Same(t, &runner, store.saved)
}
