package runner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// A worker's labels are validated when it registers, so an unrepresentable
// value is not a cosmetic problem: the worker cannot join at all.
//
// Note this test cannot reproduce the failure on its own: a test binary reports
// its module version as "devel", which was always label-safe. The value that
// broke registration only appears in a `go build` binary carrying VCS state
// ("...+dirty"). The regression guard for that is the LabelSafeValue table in
// pkg/urth; this test covers the wiring.
func TestWorkerLabelsAreValid(t *testing.T) {
	for name, labels := range map[string]interface{ Validate() error }{
		"runtime": GetRuntimeLabels(),
		"probers": ProberAsLabels(),
		"default": defaultEffectiveLabels(),
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, labels.Validate())
		})
	}
}

func defaultEffectiveLabels() manifest.Labels {
	cfg := NewDefaultConfig()
	return cfg.GetEffectiveLabels()
}
