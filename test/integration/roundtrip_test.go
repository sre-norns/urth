package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// `urthctl get KIND NAME -o yaml | urthctl apply -` is the kubectl habit of
// editing a resource in place. It crosses every boundary that could break it --
// the server's manifest, the CLI's rendering of it, the CLI's parsing of that,
// and the server's acceptance of what comes back -- so it is tested with the
// real binary against the real server. It did not work before M6.4: responses
// carried no apiVersion, and apply refuses a manifest without one.

var (
	urthctlOnce sync.Once
	urthctlPath string
	urthctlErr  error
)

// urthctl builds the CLI once per test binary.
func urthctl(t *testing.T) string {
	t.Helper()
	urthctlOnce.Do(func() {
		dir, err := os.MkdirTemp("", "urthctl-")
		if err != nil {
			urthctlErr = err
			return
		}
		urthctlPath = filepath.Join(dir, "urthctl")
		out, err := exec.Command("go", "build", "-o", urthctlPath, "github.com/sre-norns/urth/cmd/urthctl").CombinedOutput()
		if err != nil {
			urthctlErr = &buildError{err: err, out: string(out)}
		}
	})
	require.NoError(t, urthctlErr)
	return urthctlPath
}

type buildError struct {
	err error
	out string
}

func (e *buildError) Error() string { return e.err.Error() + "\n" + e.out }

// run invokes urthctl as the harness's user, with stdin, and returns stdout.
func (h *harness) urthctl(stdin string, args ...string) (string, error) {
	h.t.Helper()
	cmd := exec.Command(urthctl(h.t), append([]string{
		"--api-server-address=" + h.HTTP.URL,
		"--token=" + string(h.token),
		"--account=" + string(h.scope.Account),
		"--project=" + string(h.scope.Project),
	}, args...)...)
	// Never the developer's own profiles.
	cmd.Env = append(os.Environ(), "URTH_PROFILES="+filepath.Join(h.t.TempDir(), "profiles.json"))
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), &buildError{err: err, out: stderr.String()}
	}
	return stdout.String(), nil
}

func TestGetAsYAMLAppliesBack(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("edge", manifest.Labels{"site": "syd"})
	h.applyScenario("probe", testProbSpec{}, manifest.LabelSelector{})
	// A probe timeout is a time.Duration: nanoseconds in JSON, "3s" in YAML.
	// The first version of this test had none, and passed while a real
	// scenario failed to apply back.
	timed := h.storedScenario("probe")
	timed.Spec.Prob.Timeout = 3 * time.Second
	_, err := h.Server.Service.Scenarios().Update(h.ctx, timed.GetVersionedID(), timed.ToManifest())
	require.NoError(t, err)
	require.Equal(t, 3*time.Second, h.storedScenario("probe").Spec.Prob.Timeout)
	// A scenario that has run lists its latest results in its status, as
	// nested manifests. Status is the server's; apply must not choke on it.
	h.createRun("probe")
	require.NotEmpty(t, h.storedScenario("probe").Status.Results, "the scenario's status lists its run")

	for _, format := range []string{"yaml", "json"} {
		for _, tc := range []struct {
			get    []string
			stored func() manifest.Version
		}{
			{[]string{"get", "runner", "edge"}, func() manifest.Version { return h.storedRunner("edge").Version }},
			{[]string{"get", "scenario", "probe"}, func() manifest.Version { return h.storedScenario("probe").Version }},
			{[]string{"runner-authorizations", "get", string(runner.Name)}, func() manifest.Version {
				grant, found, err := h.Server.Service.RunnerAuthorizations().Get(h.ctx, runner.Name)
				require.NoError(t, err)
				require.True(t, found)
				return grant.Metadata.Version
			}},
		} {
			name := strings.Join(tc.get, " ") + " -o " + format
			printed, err := h.urthctl("", append(tc.get, "-o", format)...)
			require.NoError(t, err, name)
			require.Contains(t, printed, urth.APIVersion, "%s prints the apiVersion apply needs", name)

			// Applied back as printed: the version it carries is current, so it lands.
			before := tc.stored()
			_, err = h.urthctl(printed, "apply", "-")
			require.NoError(t, err, "%s | apply -", name)
			require.Greater(t, tc.stored(), before, "%s: the apply was an update", name)

			// Applied again, the same copy is stale -- someone (this test)
			// changed the resource since it was read -- and is refused, not
			// written over.
			_, err = h.urthctl(printed, "apply", "-")
			require.Error(t, err, "%s: a stale copy must not be applied", name)
		}
	}
	require.Equal(t, 3*time.Second, h.storedScenario("probe").Spec.Prob.Timeout, "the timeout survived every trip")

	// An edit survives the trip.
	printed, err := h.urthctl("", "get", "runner", "edge", "-o", "yaml")
	require.NoError(t, err)
	edited := strings.Replace(printed, "site: syd", "site: mel", 1)
	require.NotEqual(t, printed, edited, "the label is in the printed manifest")
	_, err = h.urthctl(edited, "apply", "-")
	require.NoError(t, err)
	require.Equal(t, "mel", h.storedRunner("edge").Labels["site"])
}

// Only the canonical group is accepted; no legacy input window is needed.
func TestAPIVersions(t *testing.T) {
	h := newHarness(t)
	for _, version := range []string{"v1", "apps/v1", ""} {
		body := "apiVersion: " + version + "\nkind: runners\nmetadata:\n  name: legacy\nspec:\n  active: true\n"
		_, err := h.urthctl(body, "apply", "-")
		require.Error(t, err)
	}
}

func (h *harness) storedRunner(name manifest.ResourceName) urth.Runner {
	h.t.Helper()
	value, found, err := h.Server.Service.Runners().Get(h.ctx, name)
	require.NoError(h.t, err)
	require.True(h.t, found)
	runner, err := urth.NewRunner(value)
	require.NoError(h.t, err)
	return runner
}
