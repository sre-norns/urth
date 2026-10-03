package urth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestM9RunCapabilityRejectsIncompleteAndWrongAuthority(t *testing.T) {
	keys := testKeys(t)
	now := time.Now()
	entry := Result{ObjectMeta: manifest.ObjectMeta{UID: "run", Account: "account", Project: "project"}, Status: ResultStatus{Status: JobRunning, Deadline: now.Add(time.Minute), Executor: ExecutorRef{RunnerID: "runner", WorkerID: "worker"}}}
	api := resultsAPIImpl{keys: keys, resultsSigningKey: keys.Run}
	claims := func() jwt.MapClaims {
		return jwt.MapClaims{"iss": TokenIssuer, "sub": "run", "aud": "urth-run", "iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(time.Minute).Unix(), "runnerId": "runner", "workerId": "worker", "account": "account", "project": "project", "scope": []string{"run.status", "run.artifacts"}}
	}
	sign := func(c jwt.MapClaims, method jwt.SigningMethod) APIToken {
		token := jwt.NewWithClaims(method, c)
		token.Header["kid"] = "run"
		value, err := token.SignedString(keys.Run)
		require.NoError(t, err)
		return APIToken(value)
	}
	require.NoError(t, api.validateUpdateRequest(t.Context(), entry, sign(claims(), jwt.SigningMethodHS256)), "a correct run capability is the positive control")
	for _, tc := range []struct {
		name   string
		change func(jwt.MapClaims)
		method jwt.SigningMethod
	}{
		{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "another-issuer" }, jwt.SigningMethodHS256},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = "urth-session" }, jwt.SigningMethodHS256},
		{"missing expiry", func(c jwt.MapClaims) { delete(c, "exp") }, jwt.SigningMethodHS256},
		{"missing issued at", func(c jwt.MapClaims) { delete(c, "iat") }, jwt.SigningMethodHS256},
		{"missing not before", func(c jwt.MapClaims) { delete(c, "nbf") }, jwt.SigningMethodHS256},
		{"wrong runner", func(c jwt.MapClaims) { c["runnerId"] = "other-runner" }, jwt.SigningMethodHS256},
		{"wrong worker", func(c jwt.MapClaims) { c["workerId"] = "other-worker" }, jwt.SigningMethodHS256},
		{"wrong project", func(c jwt.MapClaims) { c["project"] = "other-project" }, jwt.SigningMethodHS256},
		{"artifact-only scope", func(c jwt.MapClaims) { c["scope"] = []string{"run.artifacts"} }, jwt.SigningMethodHS256},
		{"different HMAC algorithm", func(jwt.MapClaims) {}, jwt.SigningMethodHS384},
		{"past server grace", func(c jwt.MapClaims) { c["exp"] = now.Add(time.Hour).Unix() }, jwt.SigningMethodHS256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := claims()
			tc.change(c)
			require.Error(t, api.validateUpdateRequest(t.Context(), entry, sign(c, tc.method)))
		})
	}
	valid := sign(claims(), jwt.SigningMethodHS256)
	for _, state := range []JobStatus{JobPending, JobCompleted, JobErrored, JobExpired} {
		entry.Status.Status = state
		require.Error(t, api.validateUpdateRequest(t.Context(), entry, valid), "terminal or unclaimed run %s cannot be rewritten", state)
	}
}
