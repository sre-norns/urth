package urth

import (
	"crypto/rand"
	"fmt"
	"log"
	"maps"
	"regexp"
	"slices"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// TokenIssuer is the `iss` claim on every token Urth mints, and the value
// verified on every token it accepts.
const TokenIssuer = "urth"

// SigningKeys holds the three separate secrets Urth signs tokens with.
//
// They are separate on purpose. ADR 0002 requires it, and the reason is
// containment: a run capability is handed to every worker that executes
// anything, so it is the most widely distributed of the three and the most
// likely to leak. If it shared a key with the enrolment credential, a leaked
// run token would be forgeable into permission to join a runner. Distinct keys
// mean a compromise stays in its own tier and can be rotated there.
//
// The prototype signed with two hardcoded literals -- `my_secret_key` and
// `my_results signing secret key, duh` -- checked into the repository, which is
// to say it had no key management at all.
type SigningKeys struct {
	// Enrolment signs the long-lived runner tokens an operator hands to a
	// worker to let it register.
	Enrolment []byte

	// Session signs the short-lived credential a worker uses to claim jobs.
	Session []byte

	// Run signs the per-Result capability that authorises status updates and
	// artifact uploads for one run.
	Run []byte

	// RunKeyID identifies the active run key. The default is "run".
	RunKeyID string
	// RunVerificationKeys holds additional keys accepted during rotation.
	// A retained key can verify a capability but does not issue new ones.
	RunVerificationKeys map[string][]byte
}

// SigningKeysConfig is the operator-facing form of SigningKeys, embeddable into
// a command's kong configuration.
type SigningKeysConfig struct {
	EnrolmentKey        string            `help:"Secret used to sign runner enrolment tokens" env:"URTH_ENROLMENT_SIGNING_KEY"`
	SessionKey          string            `help:"Secret used to sign worker session tokens" env:"URTH_SESSION_SIGNING_KEY"`
	RunKey              string            `help:"Secret used to sign per-run capability tokens" env:"URTH_RUN_SIGNING_KEY"`
	RunKeyID            string            `help:"Key ID for newly issued run capabilities" env:"URTH_RUN_SIGNING_KEY_ID" default:"run"`
	RunVerificationKeys map[string]string `help:"Additional run verification keys as ID=secret pairs; never repeat the active key ID" env:"URTH_RUN_VERIFICATION_KEYS"`
}

// Build turns configured secrets into signing keys, generating any that were
// left unset.
//
// Generating rather than falling back to a constant is the point: a built-in
// default is a published key, and a deployment that forgot to configure one
// would be silently forgeable by anyone with a copy of the source. A generated
// key fails safe instead -- the cost is that tokens do not survive a restart
// and workers must re-register, which is noisy enough to notice but does not
// stop anyone developing locally.
func (c SigningKeysConfig) Build() (SigningKeys, error) {
	if c.RunKeyID == "" {
		c.RunKeyID = "run"
	}
	if !runKeyIDPattern.MatchString(c.RunKeyID) {
		return SigningKeys{}, fmt.Errorf("invalid active run key ID")
	}
	if c.RunKey == "" && (c.RunKeyID != "run" || len(c.RunVerificationKeys) > 0) {
		return SigningKeys{}, fmt.Errorf("an explicit run signing key is required for key rotation")
	}
	verification := make(map[string][]byte, len(c.RunVerificationKeys))
	for id, secret := range c.RunVerificationKeys {
		if !runKeyIDPattern.MatchString(id) || id == c.RunKeyID || secret == "" {
			return SigningKeys{}, fmt.Errorf("run verification keys require non-empty secrets and valid IDs distinct from the active ID")
		}
		verification[id] = []byte(secret)
	}
	keys := SigningKeys{
		Enrolment:           []byte(c.EnrolmentKey),
		Session:             []byte(c.SessionKey),
		Run:                 []byte(c.RunKey),
		RunKeyID:            c.RunKeyID,
		RunVerificationKeys: verification,
	}

	generated := make([]string, 0, 3)
	for _, k := range []struct {
		name string
		dst  *[]byte
	}{
		{"enrolment", &keys.Enrolment},
		{"session", &keys.Session},
		{"run", &keys.Run},
	} {
		if len(*k.dst) > 0 {
			continue
		}

		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return keys, fmt.Errorf("failed to generate %s signing key: %w", k.name, err)
		}
		*k.dst = secret
		generated = append(generated, k.name)
	}

	if len(generated) > 0 {
		log.Printf("WARNING: no signing key configured for %v; generated ephemeral keys. "+
			"Tokens will not survive a restart and will not validate across API server replicas. "+
			"Set the corresponding signing key flags or environment variables for any real deployment.", generated)
	}

	return keys, nil
}

// Key IDs are opaque deployment labels, not file paths or key lookup URLs.
var runKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (k SigningKeys) runKeyID() string {
	if k.RunKeyID == "" {
		return "run"
	}
	return k.RunKeyID
}

func (k SigningKeys) runVerificationKey(id string) ([]byte, bool) {
	if id == k.runKeyID() {
		return k.Run, len(k.Run) > 0
	}
	key := k.RunVerificationKeys[id]
	return key, len(key) > 0
}

// clone prevents caller-owned buffers or maps from changing a live service's
// signing configuration. Rotation requires a newly composed API replica.
func (k SigningKeys) clone() SigningKeys {
	k.Enrolment = slices.Clone(k.Enrolment)
	k.Session = slices.Clone(k.Session)
	k.Run = slices.Clone(k.Run)
	k.RunVerificationKeys = maps.Clone(k.RunVerificationKeys)
	for id, key := range k.RunVerificationKeys {
		k.RunVerificationKeys[id] = slices.Clone(key)
	}
	return k
}

// WorkerSessionClaims is the body of a worker session credential.
//
// Both identities are bound into the token, so the server never has to take a
// worker's word for who it is. The prototype's claim endpoint read WorkerID and
// RunnerID out of the request body, which meant any process that could reach
// the API could claim any job as any worker.
type WorkerSessionClaims struct {
	jwt.RegisteredClaims

	// RunnerID is the runner this session may pull work for.
	RunnerID manifest.ResourceID `json:"runnerId"`

	// WorkerID is the WorkerInstance this session belongs to. It is also the
	// token's subject.
	WorkerID manifest.ResourceID `json:"workerId"`
}

// IssueWorkerSession mints a session credential for a registered worker.
func IssueWorkerSession(keys SigningKeys, runnerUID, workerUID manifest.ResourceID, ttl time.Duration) (APIToken, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	claims := WorkerSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    TokenIssuer,
			Subject:   string(workerUID),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		RunnerID: runnerUID,
		WorkerID: workerUID,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(keys.Session)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to sign worker session token: %w", err)
	}

	return APIToken(signed), expiresAt, nil
}

// ParseWorkerSession validates a session credential and returns its claims.
//
// Every error is reported as ErrResourceUnauthorized rather than distinguished:
// telling a caller whether its token was expired, wrongly signed, or issued by
// someone else tells an attacker which part to change next.
func ParseWorkerSession(keys SigningKeys, token APIToken) (WorkerSessionClaims, error) {
	var claims WorkerSessionClaims

	parsed, err := jwt.ParseWithClaims(string(token), &claims,
		func(*jwt.Token) (interface{}, error) { return keys.Session, nil },
		// Pin the algorithm. Without this, a token presented with alg=none --
		// or signed with a different family than intended -- reaches the
		// verification step and may be accepted.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(TokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !parsed.Valid {
		return claims, bark.ErrResourceUnauthorized
	}

	if claims.WorkerID == "" || claims.RunnerID == "" {
		return claims, bark.ErrResourceUnauthorized
	}

	// The subject and the worker claim must agree. They are set together, so a
	// token where they differ has been tampered with or issued by a build that
	// meant something different by them.
	if string(claims.WorkerID) != claims.Subject {
		return claims, bark.ErrResourceUnauthorized
	}

	return claims, nil
}
