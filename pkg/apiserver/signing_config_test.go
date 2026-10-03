package apiserver

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunKeyRotationEnvironmentAndFlags(t *testing.T) {
	t.Setenv("URTH_RUN_SIGNING_KEY", "new-secret")
	t.Setenv("URTH_RUN_SIGNING_KEY_ID", "new")
	t.Setenv("URTH_RUN_VERIFICATION_KEYS", "old=old-secret;staged=staged-secret")
	cfg, _ := parseConfig(t)
	require.Equal(t, "new", cfg.Signing.RunKeyID)
	require.Equal(t, map[string]string{"old": "old-secret", "staged": "staged-secret"}, cfg.Signing.RunVerificationKeys)
	_, err := cfg.Signing.Build()
	require.NoError(t, err)
	cfg, _ = parseConfig(t, "--signing.run-key-id=next", "--signing.run-key=next-secret", "--signing.run-verification-keys=new=new-secret")
	require.Equal(t, "next", cfg.Signing.RunKeyID)
	require.Equal(t, "next-secret", cfg.Signing.RunKey)
	require.Equal(t, map[string]string{"new": "new-secret"}, cfg.Signing.RunVerificationKeys)
	_, err = cfg.Signing.Build()
	require.NoError(t, err)
}
