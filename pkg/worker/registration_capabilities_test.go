package worker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

type capabilityRegistrationService struct {
	urth.Service
	runners urth.RunnersAPI
}

func (s capabilityRegistrationService) Runners() urth.RunnersAPI { return s.runners }

type capabilityRegistrationAPI struct {
	urth.RunnersAPI
	inspect func(manifest.ResourceManifest)
	stop    error
}

func (s capabilityRegistrationAPI) ChallengeWorker(_ context.Context, _ urth.APIToken, entry manifest.ResourceManifest) (urth.WorkerChallenge, error) {
	s.inspect(entry)
	return urth.WorkerChallenge{}, s.stop
}

func TestRegistrationBindsDiscoveredCapabilitiesBeforeProof(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	stop := errors.New("challenge inspected")
	inspected := false
	cfg := &Config{APIRegistrationTimeout: time.Minute, Name: "worker-test", RunnerConfig: runner.RunnerConfig{Timeout: 9 * time.Second, CustomLabels: manifest.Labels{urth.LabelWorkerOS: "forged"}}}
	w := &Worker{config: cfg, identityKey: key, apiClient: capabilityRegistrationService{runners: capabilityRegistrationAPI{stop: stop, inspect: func(entry manifest.ResourceManifest) {
		inspected = true
		worker, err := urth.NewWorkerInstance(entry)
		require.NoError(t, err)
		require.NoError(t, worker.Spec.Capabilities.Validate())
		require.NotEqual(t, "forged", worker.Spec.Capabilities.OS)
		require.Equal(t, "9s", worker.Spec.Capabilities.MaxDuration)
		require.NotContains(t, worker.Spec.Capabilities.ProbeVersions, "puppeteer")
		require.NotNil(t, worker.Spec.Proof)
		require.NotEmpty(t, worker.Spec.Proof.PublicKey)
		require.Nil(t, entry.Status)
	}}}}
	_, err = w.register(context.Background())
	require.ErrorIs(t, err, stop)
	require.True(t, inspected)
}
