package runner

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/urth"
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

func TestRuntimeLabelsAdvertiseHostname(t *testing.T) {
	hostname, err := os.Hostname()
	require.NoError(t, err)

	labels := GetRuntimeLabels()

	require.Equal(t, urth.LabelSafeValue(hostname), labels[urth.LabelWorkerHostname])
}

func TestHostnameLabelOmitsUnrepresentableValues(t *testing.T) {
	testCases := map[string]struct {
		hostname string
		value    string
		valid    bool
	}{
		"ordinary":        {hostname: "build-7", value: "build-7", valid: true},
		"label-safe":      {hostname: "build_host", value: "build_host", valid: true},
		"empty after map": {hostname: "::", valid: false},
		"too long":        {hostname: strings.Repeat("a", 64), valid: false},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			value, valid := hostnameLabel(testCase.hostname)
			require.Equal(t, testCase.valid, valid)
			require.Equal(t, testCase.value, value)
		})
	}
}

func defaultEffectiveLabels() manifest.Labels {
	cfg := NewDefaultConfig()
	return cfg.GetEffectiveLabels()
}
