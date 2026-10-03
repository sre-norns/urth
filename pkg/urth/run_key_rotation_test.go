package urth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestRunKeyRotationUsesExactKeyID(t *testing.T) {
	old := testKeys(t)
	old.RunKeyID = "old"
	next := old.clone()
	next.RunKeyID = "next"
	next.Run = []byte("next-run-secret")
	next.RunVerificationKeys = map[string][]byte{"old": old.Run}
	now := time.Now()
	claims := RunCapabilityClaims{
		RegisteredClaims: jwt.RegisteredClaims{Issuer: TokenIssuer, Subject: "result", Audience: jwt.ClaimStrings{runAudience}, IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))},
		RunnerID:         manifest.ResourceID("runner"), WorkerID: manifest.ResourceID("worker"), DispatchID: "dispatch", Scope: []string{runStatusScope, runArtifactScope},
	}
	sign := func(kid any, key []byte) APIToken {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		if kid != nil {
			token.Header["kid"] = kid
		}
		signed, err := token.SignedString(key)
		require.NoError(t, err)
		return APIToken(signed)
	}
	for _, scope := range []string{runStatusScope, runArtifactScope} {
		_, err := parseRunCapability(next, sign("old", old.Run), scope)
		require.NoError(t, err, "old key remains valid during overlap")
		_, err = parseRunCapability(next, sign("next", next.Run), scope)
		require.NoError(t, err, "new key is accepted")
		for _, kid := range []any{nil, "", 42, []string{"old"}, "unknown", "next"} {
			_, err := parseRunCapability(next, sign(kid, old.Run), scope)
			require.Error(t, err, "must select exactly the declared key, kid=%v", kid)
		}
	}
	delete(next.RunVerificationKeys, "old")
	_, err := parseRunCapability(next, sign("old", old.Run), runStatusScope)
	require.Error(t, err, "retired keys must stop verifying")
}

func TestRunKeyRotationConfiguration(t *testing.T) {
	base := SigningKeysConfig{EnrolmentKey: "enrolment", SessionKey: "session", RunKey: "new-secret", RunKeyID: "new", RunVerificationKeys: map[string]string{"old": "old-secret"}}
	keys, err := base.Build()
	require.NoError(t, err)
	require.Equal(t, "new", keys.runKeyID())
	key, ok := keys.runVerificationKey("old")
	require.True(t, ok)
	require.Equal(t, []byte("old-secret"), key)
	for _, tc := range []struct {
		name   string
		change func(*SigningKeysConfig)
	}{
		{"active key missing", func(c *SigningKeysConfig) { c.RunKey = "" }},
		{"active ID invalid", func(c *SigningKeysConfig) { c.RunKeyID = "../key" }},
		{"active ID repeated", func(c *SigningKeysConfig) { c.RunVerificationKeys = map[string]string{"new": "secret"} }},
		{"verification ID empty", func(c *SigningKeysConfig) { c.RunVerificationKeys = map[string]string{"": "secret"} }},
		{"verification ID invalid", func(c *SigningKeysConfig) { c.RunVerificationKeys = map[string]string{"a/b": "secret"} }},
		{"verification secret empty", func(c *SigningKeysConfig) { c.RunVerificationKeys = map[string]string{"old": ""} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.change(&cfg)
			_, err := cfg.Build()
			require.Error(t, err)
			require.NotContains(t, err.Error(), "new-secret")
			require.NotContains(t, err.Error(), "old-secret")
		})
	}
}

func TestServiceKeepsSigningKeySnapshot(t *testing.T) {
	keys := testKeys(t)
	keys.RunVerificationKeys = map[string][]byte{"old": []byte("old-secret")}
	var service serviceImpl
	WithSigningKeys(keys)(&service)
	keys.Run[0] = '!'
	keys.RunVerificationKeys["old"][0] = '!'
	delete(keys.RunVerificationKeys, "old")
	require.Equal(t, []byte("run-key-for-tests"), service.keys.Run)
	require.Equal(t, []byte("old-secret"), service.keys.RunVerificationKeys["old"])
}
