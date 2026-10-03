package urth

import (
	"slices"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	runAudience      = "urth-run"
	runStatusScope   = "run.status"
	runArtifactScope = "run.artifacts"
)

// RunCapabilityClaims binds a run credential to the executor recorded at claim.
// The bearer may report that execution during its bounded upload grace period.
type RunCapabilityClaims struct {
	jwt.RegisteredClaims
	RunnerID manifest.ResourceID `json:"runnerId"`
	WorkerID manifest.ResourceID `json:"workerId"`
	Account  manifest.ResourceID `json:"account,omitempty"`
	Project  manifest.ResourceID `json:"project,omitempty"`
	Scope    []string            `json:"scope"`
}

func parseRunCapability(key []byte, token APIToken, scope string) (RunCapabilityClaims, error) {
	var claims RunCapabilityClaims
	parsed, err := jwt.ParseWithClaims(string(token), &claims, func(token *jwt.Token) (any, error) {
		if token.Header["kid"] != "run" {
			return nil, bark.ErrResourceUnauthorized
		}
		return key, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(TokenIssuer), jwt.WithAudience(runAudience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !parsed.Valid || claims.Subject == "" || claims.RunnerID == "" || claims.WorkerID == "" ||
		claims.IssuedAt == nil || claims.NotBefore == nil || !slices.Contains(claims.Scope, scope) {
		return RunCapabilityClaims{}, bark.ErrResourceUnauthorized
	}
	return claims, nil
}

func validateRunBinding(entry Result, claims RunCapabilityClaims) error {
	if claims.Subject != string(entry.UID) || claims.RunnerID != entry.Status.Executor.RunnerID ||
		claims.WorkerID != entry.Status.Executor.WorkerID || claims.Account != entry.Account || claims.Project != entry.Project ||
		entry.Status.Deadline.IsZero() || claims.ExpiresAt.Time.After(entry.Status.Deadline.Add(artifactUploadGrace)) {
		return bark.ErrResourceUnauthorized
	}
	return nil
}
