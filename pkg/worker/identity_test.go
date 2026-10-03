package worker

import (
	"crypto/ed25519"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestInstallationKeyPersistsPrivatelyAndRejectsUnsafeFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "worker.key")
	first, err := LoadInstallationKey(path)
	require.NoError(t, err)
	again, err := LoadInstallationKey(path)
	require.NoError(t, err)
	require.Equal(t, first, again)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	require.NoError(t, os.Chmod(path, 0644))
	_, err = LoadInstallationKey(path)
	require.Error(t, err)
	require.NoError(t, os.Chmod(path, 0600))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(path, link))
	_, err = LoadInstallationKey(link)
	require.Error(t, err)
	other, err := LoadInstallationKey(filepath.Join(t.TempDir(), "worker.key"))
	require.NoError(t, err)
	require.NotEqual(t, first, other)
}
func TestWorkerAPITransportRequiresHTTPSOrExplicitLocalMode(t *testing.T) {
	for _, tc := range []struct {
		address string
		local   bool
		allowed bool
	}{
		{"https://urth.example", false, true}, {"http://127.0.0.1:8080", false, false}, {"http://127.0.0.1:8080", true, true}, {"http://example.com", true, false}, {"https://user:secret@example.com", false, false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			cfg := Config{AllowInsecureAPI: tc.local}
			cfg.APIServerAddress = tc.address
			err := cfg.ValidateAPITransport()
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestRenewalDelaySupportsShortAuthorityAndBacksOffAfterExpiry(t *testing.T) {
	require.Equal(t, 2*time.Second, renewalDelay(3*time.Second, 0))
	require.Less(t, renewalDelay(time.Second, 0), time.Second)
	require.Equal(t, 2*time.Second, renewalDelay(-time.Second, 1))
	require.Equal(t, time.Minute, renewalDelay(-time.Second, 20))
	require.LessOrEqual(t, renewalDelay(3*time.Second, 20), time.Second)
}

func TestConcurrentInstallationKeyCreationUsesOneCompleteSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worker.key")
	var wg sync.WaitGroup
	keys := make(chan ed25519.PrivateKey, 16)
	failures := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key, err := LoadInstallationKey(path)
			if err != nil {
				failures <- err
			} else {
				keys <- key
			}
		}()
	}
	wg.Wait()
	close(keys)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	expected, err := LoadInstallationKey(path)
	require.NoError(t, err)
	for key := range keys {
		require.Equal(t, expected, key)
	}
}
