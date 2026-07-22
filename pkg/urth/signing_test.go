package urth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func testKeys(t *testing.T) SigningKeys {
	t.Helper()

	keys, err := SigningKeysConfig{
		EnrolmentKey: "enrolment-key-for-tests",
		SessionKey:   "session-key-for-tests",
		RunKey:       "run-key-for-tests",
	}.Build()
	require.NoError(t, err)

	return keys
}

func TestWorkerSessionRoundTrip(t *testing.T) {
	keys := testKeys(t)

	token, expiresAt, err := IssueWorkerSession(keys, "runner-uid", "worker-uid", time.Hour)
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, time.Minute)

	claims, err := ParseWorkerSession(keys, token)
	require.NoError(t, err)
	require.Equal(t, manifest.ResourceID("runner-uid"), claims.RunnerID)
	require.Equal(t, manifest.ResourceID("worker-uid"), claims.WorkerID)
	require.Equal(t, "worker-uid", claims.Subject)
}

// A session signed with the run-capability key must not authenticate a worker.
//
// This is what separating the keys buys. A run capability is handed to every
// worker that executes anything, so it is the most widely spread credential
// Urth issues; if it shared a key with sessions, a leaked run token could be
// re-signed into permission to claim arbitrary jobs. The prototype used one
// hardcoded literal for enrolment and another for runs, both in the source.
func TestWorkerSessionRejectsTokenSignedWithAnotherKey(t *testing.T) {
	keys := testKeys(t)

	forged := SigningKeys{Session: keys.Run}
	token, _, err := IssueWorkerSession(forged, "runner-uid", "worker-uid", time.Hour)
	require.NoError(t, err)

	_, err = ParseWorkerSession(keys, token)
	require.Error(t, err, "a session signed with the run key must not be accepted")
}

func TestWorkerSessionRejectsExpired(t *testing.T) {
	keys := testKeys(t)

	token, _, err := IssueWorkerSession(keys, "runner-uid", "worker-uid", -time.Minute)
	require.NoError(t, err)

	_, err = ParseWorkerSession(keys, token)
	require.Error(t, err, "an expired session must not be accepted")
}

// An unsigned token must not be accepted.
//
// Note what actually stops this: jwt/v5 refuses the `none` method because the
// keyfunc hands back an HMAC secret rather than UnsafeAllowNoneSignatureType,
// so it fails on key type before the algorithm allowlist is consulted. The
// allowlist in ParseWorkerSession is defence in depth for the case this test
// does not reach -- a future key type usable by more than one algorithm family.
// The test is here to hold the outcome, not to prove which check produced it.
func TestWorkerSessionRejectsUnsignedToken(t *testing.T) {
	keys := testKeys(t)

	claims := WorkerSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Subject:   "worker-uid",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		RunnerID: "runner-uid",
		WorkerID: "worker-uid",
	}

	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = ParseWorkerSession(keys, APIToken(unsigned))
	require.Error(t, err, "an alg=none token must not be accepted")
}

func TestWorkerSessionRejectsForeignIssuer(t *testing.T) {
	keys := testKeys(t)

	claims := WorkerSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "somebody-else",
			Subject:   "worker-uid",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		RunnerID: "runner-uid",
		WorkerID: "worker-uid",
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(keys.Session)
	require.NoError(t, err)

	_, err = ParseWorkerSession(keys, APIToken(token))
	require.Error(t, err, "a token from another issuer must not be accepted")
}

// The subject and the worker claim are written together, so a token where they
// disagree has been tampered with. Accepting it would let a holder keep a valid
// signature while pointing the claim at a different worker.
func TestWorkerSessionRejectsSubjectMismatch(t *testing.T) {
	keys := testKeys(t)

	claims := WorkerSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Subject:   "worker-uid",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		RunnerID: "runner-uid",
		WorkerID: "a-different-worker",
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(keys.Session)
	require.NoError(t, err)

	_, err = ParseWorkerSession(keys, APIToken(token))
	require.Error(t, err, "a token whose subject and worker claim disagree must not be accepted")
}

func TestWorkerSessionRejectsMissingIdentity(t *testing.T) {
	keys := testKeys(t)

	claims := WorkerSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Subject:   "",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(keys.Session)
	require.NoError(t, err)

	_, err = ParseWorkerSession(keys, APIToken(token))
	require.Error(t, err, "a token naming no worker or runner must not be accepted")
}

func TestWorkerSessionRejectsGarbage(t *testing.T) {
	keys := testKeys(t)

	for _, token := range []APIToken{"", "not-a-jwt", "a.b.c"} {
		_, err := ParseWorkerSession(keys, token)
		require.Errorf(t, err, "ParseWorkerSession(%q) must fail", token)
	}
}

// Unset keys must be generated, never defaulted to a constant. A built-in
// default is a published secret: anyone with the source could mint a session
// for any worker of any deployment that forgot to configure one.
func TestSigningKeysAreGeneratedWhenUnset(t *testing.T) {
	first, err := SigningKeysConfig{}.Build()
	require.NoError(t, err)

	second, err := SigningKeysConfig{}.Build()
	require.NoError(t, err)

	require.NotEmpty(t, first.Enrolment)
	require.NotEmpty(t, first.Session)
	require.NotEmpty(t, first.Run)

	// Distinct from each other...
	require.NotEqual(t, first.Enrolment, first.Session)
	require.NotEqual(t, first.Session, first.Run)
	require.NotEqual(t, first.Enrolment, first.Run)

	// ...and not a fixed value shared between builds.
	require.NotEqual(t, first.Session, second.Session)
}

func TestSigningKeysUseConfiguredValues(t *testing.T) {
	keys, err := SigningKeysConfig{
		EnrolmentKey: "a",
		SessionKey:   "b",
		RunKey:       "c",
	}.Build()
	require.NoError(t, err)

	require.Equal(t, []byte("a"), keys.Enrolment)
	require.Equal(t, []byte("b"), keys.Session)
	require.Equal(t, []byte("c"), keys.Run)
}

func TestClampRunDuration(t *testing.T) {
	const maximum = 30 * time.Minute

	tests := map[string]struct {
		requested time.Duration
		maximum   time.Duration
		want      time.Duration
	}{
		"a shorter request is granted":         {requested: time.Minute, maximum: maximum, want: time.Minute},
		"a longer request is clamped":          {requested: 24 * time.Hour, maximum: maximum, want: maximum},
		"an unset request takes the maximum":   {requested: 0, maximum: maximum, want: maximum},
		"a negative request takes the maximum": {requested: -time.Hour, maximum: maximum, want: maximum},
		"an unset maximum falls back":          {requested: 24 * time.Hour, maximum: 0, want: DefaultMaxRunDuration},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, clampRunDuration(test.requested, test.maximum))
		})
	}
}
