package prob

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildVersionPrefersStampedRelease(t *testing.T) {
	saved := buildVersion
	t.Cleanup(func() { buildVersion = saved })

	buildVersion = ""
	require.NotEmpty(t, BuildVersion())

	buildVersion = "v0.1.0-rc.3"
	require.Equal(t, "v0.1.0-rc.3", BuildVersion())
}
