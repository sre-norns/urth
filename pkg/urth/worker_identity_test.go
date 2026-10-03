package urth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/bark"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func signedWorker(t *testing.T, srv urth.Service, ctx context.Context, token urth.APIToken, entry manifest.ResourceManifest, key ed25519.PrivateKey) manifest.ResourceManifest {
	t.Helper()
	worker, err := urth.NewWorkerInstance(entry)
	require.NoError(t, err)
	worker.Spec.Proof = &urth.WorkerProof{PublicKey: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}
	entry.Spec = &worker.Spec
	entry.Status = nil
	challenge, err := srv.Runners().ChallengeWorker(ctx, token, entry)
	require.NoError(t, err)
	worker.Spec.Proof.Challenge = challenge.Challenge
	worker.Spec.Proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(challenge.Message)))
	return entry
}
func enrollTestWorker(t *testing.T, srv urth.Service, ctx context.Context, token urth.APIToken, entry manifest.ResourceManifest) (urth.WorkerRegistrationResponse, error) {
	t.Helper()
	seed := sha256.Sum256([]byte(t.Name() + "/" + string(entry.Metadata.Name)))
	key := ed25519.NewKeyFromSeed(seed[:])
	return srv.Runners().AuthWorker(ctx, token, signedWorker(t, srv, ctx, token, entry, key))
}

func TestWorkerProofIdentityReplayAndBlocklist(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{}, urth.WithSigningKeys(testKeys(t)))
	seedScenario(t, store)
	ctx := context.Background()
	token, found, err := srv.Runners().GetToken(ctx, "test-runner")
	require.NoError(t, err)
	require.True(t, found)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "stable-worker"}, Spec: &urth.WorkerInstanceSpec{}}
	proof := signedWorker(t, srv, ctx, token, entry, key)
	// A forged signature cannot consume the valid proof.
	forged, err := urth.NewWorkerInstance(proof)
	require.NoError(t, err)
	original := *forged.Spec.Proof
	bad := original
	bad.Signature = base64.RawURLEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	forged.Spec.Proof = &bad
	invalid := proof
	invalid.Spec = &forged.Spec
	_, err = srv.Runners().AuthWorker(ctx, token, invalid)
	require.Error(t, err)
	altered := proof
	altered.Metadata.Name = "tampered-name"
	_, err = srv.Runners().AuthWorker(ctx, token, altered)
	require.Error(t, err)
	registration, err := srv.Runners().AuthWorker(ctx, token, proof)
	require.NoError(t, err)
	worker, err := urth.NewWorkerInstance(registration.Worker)
	require.NoError(t, err)
	require.Equal(t, urth.WorkerFingerprint(key.Public().(ed25519.PublicKey)), worker.Status.Fingerprint)
	require.Nil(t, worker.Spec.Proof)
	serialized, err := json.Marshal(registration)
	require.NoError(t, err)
	require.NotContains(t, string(serialized), base64.RawURLEncoding.EncodeToString(key.Seed()))
	_, err = srv.Runners().AuthWorker(ctx, token, proof)
	require.Error(t, err, "a proof is single-use")
	// Renaming proves the same identity; a same-name different key is a conflict.
	entry.Metadata.Name = "renamed-worker"
	refreshed, err := srv.Runners().AuthWorker(ctx, token, signedWorker(t, srv, ctx, token, entry, key))
	require.NoError(t, err)
	require.Equal(t, worker.UID, refreshed.Worker.Metadata.UID)
	_, other, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, err = srv.Runners().AuthWorker(ctx, token, signedWorker(t, srv, ctx, token, entry, other))
	require.Error(t, err)
	require.NotContains(t, err.Error(), base64.RawURLEncoding.EncodeToString(key.Seed()))
	// Expired challenge, including a previously signed message, fails.
	expiring := signedWorker(t, srv, ctx, token, entry, key)
	exp, err := urth.NewWorkerInstance(expiring)
	require.NoError(t, err)
	require.NoError(t, db.Model(&urth.WorkerChallengeRecord{}).Where("id = ?", exp.Spec.Proof.Challenge).Update("expires_at", time.Now().Add(-time.Second)).Error)
	_, err = srv.Runners().AuthWorker(ctx, token, expiring)
	require.Error(t, err)
	// A current active session cannot claim after a conditional Runner block edit.
	runnerManifest, found, err := srv.Runners().Get(ctx, "test-runner")
	require.NoError(t, err)
	require.True(t, found)
	runner, err := urth.NewRunner(runnerManifest)
	require.NoError(t, err)
	runner.Spec.BlockedWorkers = []urth.BlockedWorker{{Identity: worker.Status.Fingerprint, Reason: "retired"}}
	blocked, err := srv.Runners().Update(urth.WithIfMatch(ctx, runner.Version), runner.GetVersionedID(), runner.ToManifest())
	require.NoError(t, err)
	_, err = srv.Runners().ChallengeWorker(ctx, token, proof)
	require.Error(t, err)
	run, err := srv.Results("test-scenario").Create(ctx, newRunRequest())
	require.NoError(t, err)
	_, err = srv.Results("").ClaimRun(ctx, run.UID, refreshed.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.Error(t, err)
	disposition, ok := urth.ClaimDispositionOf(err)
	require.True(t, ok)
	require.Equal(t, urth.ClaimForbidden, disposition)
	_, err = srv.Runners().Update(urth.WithIfMatch(ctx, runner.Version), runner.GetVersionedID(), runner.ToManifest())
	require.Error(t, err, "stale blocklist changes fail")
	latest, err := urth.NewRunner(blocked)
	require.NoError(t, err)
	latest.Spec.BlockedWorkers = nil
	_, err = srv.Runners().Update(urth.WithIfMatch(ctx, latest.Version), latest.GetVersionedID(), latest.ToManifest())
	require.NoError(t, err)
	unblocked, err := srv.Runners().AuthWorker(ctx, token, signedWorker(t, srv, ctx, token, entry, key))
	require.NoError(t, err)
	require.Equal(t, worker.UID, unblocked.Worker.Metadata.UID)
}

func TestConcurrentWorkerChallengeReplayHasOneWinner(t *testing.T) {
	srv, _, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)
	ctx := context.Background()
	token, _, err := srv.Runners().GetToken(ctx, "test-runner")
	require.NoError(t, err)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "concurrent"}, Spec: &urth.WorkerInstanceSpec{}}
	entry = signedWorker(t, srv, ctx, token, entry, key)
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := srv.Runners().AuthWorker(ctx, token, entry)
			if err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, successes.Load())
}

func TestWorkerChallengeCapacityIsBounded(t *testing.T) {
	srv, db, store := newTestService(t, &stubScheduler{})
	seedScenario(t, store)
	ctx := context.Background()
	token, _, err := srv.Runners().GetToken(ctx, "test-runner")
	require.NoError(t, err)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "bounded"}, Spec: &urth.WorkerInstanceSpec{Proof: &urth.WorkerProof{PublicKey: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}}}
	for i := 0; i < 128; i++ {
		_, err = srv.Runners().ChallengeWorker(ctx, token, entry)
		require.NoError(t, err)
	}
	_, err = srv.Runners().ChallengeWorker(ctx, token, entry)
	problem, ok := errors.AsType[*bark.ErrorResponse](err)
	require.True(t, ok)
	require.Equal(t, 429, problem.Code)
	require.NoError(t, db.Model(&urth.WorkerChallengeRecord{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Second)).Error)
	_, err = srv.Runners().ChallengeWorker(ctx, token, entry)
	require.NoError(t, err)
}
