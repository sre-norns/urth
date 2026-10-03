package urth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/dbstore"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const WorkerChallengeTTL = 2 * time.Minute
const workerProofAudience = "urth.worker-enrollment.v1"

// WorkerProof contains only public identity and a signature. Private keys stay local.
type WorkerProof struct {
	PublicKey string `json:"publicKey" yaml:"publicKey"`
	Challenge string `json:"challenge,omitempty" yaml:"challenge,omitempty"`
	Signature string `json:"signature,omitempty" yaml:"signature,omitempty"`
}
type WorkerChallenge struct {
	Challenge string    `json:"challenge"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// WorkerChallengeRecord is shared across API replicas. Consuming it is atomic.
type WorkerChallengeRecord struct {
	ID        string              `gorm:"primaryKey;size:64"`
	RunnerID  manifest.ResourceID `gorm:"index"`
	AccountID manifest.ResourceID `gorm:"index"`
	Digest    string              `gorm:"size:64"`
	PublicKey string              `gorm:"size:64"`
	ExpiresAt time.Time           `gorm:"type:TIMESTAMPTZ;index"`
}
type BlockedWorker struct {
	Identity string `json:"identity" yaml:"identity"`
	Reason   string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

func validateBlocklist(entries []BlockedWorker) error {
	if len(entries) > 1024 {
		return fmt.Errorf("blockedWorkers exceeds 1024 entries")
	}
	seen := map[string]bool{}
	for _, e := range entries {
		raw, err := hex.DecodeString(strings.TrimPrefix(e.Identity, "sha256:"))
		if err != nil || len(raw) != 32 || e.Identity != "sha256:"+hex.EncodeToString(raw) || seen[e.Identity] || len(e.Reason) > 1024 {
			return fmt.Errorf("invalid or duplicate blocked worker fingerprint")
		}
		seen[e.Identity] = true
	}
	return nil
}
func (r Runner) BlocksWorker(fingerprint string) bool {
	for _, e := range r.Spec.BlockedWorkers {
		if e.Identity == fingerprint {
			return true
		}
	}
	return false
}
func WorkerFingerprint(public ed25519.PublicKey) string {
	sum := sha256.Sum256(public)
	return fmt.Sprintf("sha256:%x", sum)
}

// WithWorkerIdentityDB enables persistent, single-use proof challenges in storage tests.
// Production uses WithIdentity, which sets this database too.
func WithWorkerIdentityDB(db *gorm.DB) ServiceOption { return func(s *serviceImpl) { s.workerDB = db } }

func workerProofMaterial(entry manifest.ResourceManifest) (WorkerInstance, []byte, string, error) {
	worker, err := NewWorkerInstance(entry)
	if err != nil {
		return worker, nil, "", err
	}
	if worker.Spec.Proof == nil {
		return worker, nil, "", bark.ErrResourceUnauthorized
	}
	proof := *worker.Spec.Proof
	public, err := base64.RawURLEncoding.DecodeString(proof.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return worker, nil, "", bark.ErrResourceUnauthorized
	}
	proof.Challenge = ""
	proof.Signature = ""
	worker.Spec.Proof = &proof
	// Bind public identity, name, capabilities and requested TTL. Reject client
	// resource/status identity: all registration state is assigned by the server.
	if entry.Metadata.UID != "" || entry.Metadata.Version != 0 || entry.Status != nil {
		return worker, nil, "", bark.ErrResourceUnauthorized
	}
	entry.Spec = worker.Spec
	data, err := json.Marshal(entry)
	if err != nil || len(data) > 64*1024 {
		return worker, nil, "", bark.ErrResourceUnauthorized
	}
	sum := sha256.Sum256(data)
	return worker, public, hex.EncodeToString(sum[:]), nil
}
func (m *runnersAPIImpl) enrollmentRunner(ctx context.Context, token APIToken) (manifest.ResourceID, error) {
	if m.identity != nil {
		return m.machineRunner(ctx, token)
	}
	if !m.storageTestEnrollment {
		return "", bark.ErrResourceUnauthorized
	}
	// Storage-only tests retain their scoped enrollment JWT. Mounted production
	// has shared identity and cannot enter this path.
	parsed, err := jwt.Parse(string(token), func(*jwt.Token) (any, error) { return m.hmacSampleSecret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		return "", bark.ErrResourceUnauthorized
	}
	subject, err := parsed.Claims.GetSubject()
	if err != nil || subject == "" {
		return "", bark.ErrResourceUnauthorized
	}
	return manifest.ResourceID(subject), nil
}
func (m *runnersAPIImpl) ChallengeWorker(ctx context.Context, token APIToken, entry manifest.ResourceManifest) (WorkerChallenge, error) {
	id, err := m.enrollmentRunner(ctx, token)
	if err != nil {
		return WorkerChallenge{}, err
	}
	if m.workerDB == nil {
		return WorkerChallenge{}, fmt.Errorf("worker identity database is not configured")
	}
	worker, public, digest, err := workerProofMaterial(entry)
	if err != nil {
		return WorkerChallenge{}, err
	}
	var runner Runner
	if ok, err := m.store.GetByUID(controlContext(ctx), &runner, id); err != nil {
		return WorkerChallenge{}, claimUnavailable("load challenge runner", err)
	} else if !ok || !runner.Spec.IsActive || runner.BlocksWorker(WorkerFingerprint(public)) {
		return WorkerChallenge{}, bark.ErrResourceUnauthorized
	}
	if (entry.Metadata.Account != "" && entry.Metadata.Account != runner.Account) || entry.Metadata.Project != "" {
		return WorkerChallenge{}, bark.ErrResourceUnauthorized
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return WorkerChallenge{}, err
	}
	record := WorkerChallengeRecord{ID: hex.EncodeToString(nonce), RunnerID: id, AccountID: runner.Account, Digest: digest, PublicKey: worker.Spec.Proof.PublicKey, ExpiresAt: time.Now().Add(WorkerChallengeTTL).UTC().Truncate(time.Microsecond)}
	err = m.workerDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked Runner
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uid = ?", id).First(&locked).Error; err != nil {
			return claimUnavailable("lock challenge runner", err)
		}
		if !locked.Spec.IsActive || locked.BlocksWorker(WorkerFingerprint(public)) {
			return bark.ErrResourceUnauthorized
		}
		if err := tx.Where("expires_at <= ?", time.Now()).Delete(&WorkerChallengeRecord{}).Error; err != nil {
			return claimUnavailable("sweep worker challenges", err)
		}
		var count int64
		if err := tx.Model(&WorkerChallengeRecord{}).Where("runner_id = ?", id).Count(&count).Error; err != nil {
			return claimUnavailable("count worker challenges", err)
		}
		if count >= 128 {
			return bark.NewErrorResponse(http.StatusTooManyRequests, fmt.Errorf("worker challenge capacity reached; retry after challenge expiry"))
		}
		if err := tx.Create(&record).Error; err != nil {
			return claimUnavailable("store worker challenge", err)
		}
		return nil
	})
	if err != nil {
		return WorkerChallenge{}, err
	}

	return WorkerChallenge{Challenge: record.ID, Message: workerChallengeMessage(record, runner.Account), ExpiresAt: record.ExpiresAt}, nil
}
func workerChallengeMessage(c WorkerChallengeRecord, account manifest.ResourceID) string {
	return strings.Join([]string{workerProofAudience, string(account), string(c.RunnerID), c.ID, c.Digest, c.ExpiresAt.UTC().Format(time.RFC3339Nano)}, "\n")
}
func (m *runnersAPIImpl) verifyWorkerProof(ctx context.Context, runner Runner, entry manifest.ResourceManifest) (string, error) {
	_, public, digest, err := workerProofMaterial(entry)
	if err != nil {
		return "", err
	}
	// The material helper clears the signature for hashing. Read the original.
	original, err := NewWorkerInstance(entry)
	if err != nil {
		return "", err
	}
	proof := original.Spec.Proof
	if len(proof.Challenge) != 64 || len(proof.Signature) > 128 {
		return "", bark.ErrResourceUnauthorized
	}
	var record WorkerChallengeRecord
	err = m.workerDB.WithContext(ctx).Where("id = ? AND runner_id = ? AND expires_at > ?", proof.Challenge, runner.UID, time.Now()).First(&record).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", bark.ErrResourceUnauthorized
		}
		return "", claimUnavailable("load worker challenge", err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(proof.Signature)
	if err != nil || record.AccountID != runner.Account || record.Digest != digest || record.PublicKey != proof.PublicKey || !ed25519.Verify(public, []byte(workerChallengeMessage(record, runner.Account)), signature) {
		return "", bark.ErrResourceUnauthorized
	}
	consumed := m.workerDB.WithContext(ctx).Where("id = ? AND expires_at > ?", record.ID, time.Now()).Delete(&WorkerChallengeRecord{})
	if consumed.Error != nil {
		return "", claimUnavailable("consume worker challenge", consumed.Error)
	}
	if consumed.RowsAffected != 1 {
		return "", bark.ErrResourceUnauthorized
	}
	return WorkerFingerprint(public), nil
}
func (m *runnersAPIImpl) admitVerifiedWorker(ctx context.Context, token APIToken, entry manifest.ResourceManifest) (Runner, WorkerInstance, error) {
	id, err := m.enrollmentRunner(ctx, token)
	if err != nil {
		return Runner{}, WorkerInstance{}, err
	}
	if m.workerDB == nil {
		return Runner{}, WorkerInstance{}, fmt.Errorf("worker identity database is not configured")
	}
	var runner Runner
	var worker WorkerInstance
	err = m.workerDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize enrollment for one Runner, including name/fingerprint checks.
		// A concurrent blocklist edit must commit before or after this admission.
		var locked Runner
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uid = ?", id).First(&locked).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return bark.ErrResourceUnauthorized
			}
			return claimUnavailable("lock admission runner", err)
		}
		copy := *m
		copy.workerDB = tx
		store, err := dbstore.NewDBStore(tx, dbstore.ManifestModel)
		if err != nil {
			return err
		}
		if m.identity != nil {
			store = store.WithVisibility(scopedVisibility{DB: tx})
		}
		copy.store = store
		runner, worker, err = copy.admitWorker(ctx, token, entry)
		return err
	})
	return runner, worker, err
}

// WithStorageTestEnrollment enables legacy signing only for storage unit tests.
// The API server always uses shared machine identity; no runtime flag enables this.
func WithStorageTestEnrollment() ServiceOption {
	return func(s *serviceImpl) { s.storageTestEnrollment = true }
}

// SignWorkerChallenge validates the domain and exact local manifest before the
// installation key signs anything received from the control plane.
func SignWorkerChallenge(entry manifest.ResourceManifest, challenge WorkerChallenge, key ed25519.PrivateKey) (manifest.ResourceManifest, error) {
	worker, public, digest, err := workerProofMaterial(entry)
	if err != nil {
		return entry, err
	}
	if !ed25519.PublicKey(public).Equal(key.Public()) {
		return entry, fmt.Errorf("worker proof key mismatch")
	}
	fields := strings.Split(challenge.Message, "\n")
	if len(fields) != 6 || fields[0] != workerProofAudience || fields[3] != challenge.Challenge || len(fields[3]) != 64 || fields[4] != digest || fields[5] != challenge.ExpiresAt.UTC().Format(time.RFC3339Nano) || fields[2] == "" {
		return entry, fmt.Errorf("invalid worker proof challenge")
	}
	if entry.Metadata.Account != "" && string(entry.Metadata.Account) != fields[1] {
		return entry, fmt.Errorf("worker proof account mismatch")
	}
	worker.Spec.Proof.Challenge = challenge.Challenge
	worker.Spec.Proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(challenge.Message)))
	entry.Spec = &worker.Spec
	return entry, nil
}
