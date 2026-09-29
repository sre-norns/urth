package natsq_test

import (
	"context"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func testRunnerLookup(_ context.Context, uid manifest.ResourceID) (urth.Runner, error) {
	return urth.Runner{ObjectMeta: manifest.ObjectMeta{UID: uid, Account: "11111111-1111-4111-8111-111111111111", Name: manifest.ResourceName(uid)}}, nil
}
