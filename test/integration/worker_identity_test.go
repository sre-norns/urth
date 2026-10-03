package integration

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func (h *harness) signedWorker(token urth.APIToken, entry manifest.ResourceManifest, key ed25519.PrivateKey) manifest.ResourceManifest {
	h.t.Helper()
	if key == nil {
		_, key, _ = ed25519.GenerateKey(rand.Reader)
	}
	worker, err := urth.NewWorkerInstance(entry)
	require.NoError(h.t, err)
	worker.Spec.Proof = &urth.WorkerProof{PublicKey: base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))}
	entry.Spec = &worker.Spec
	entry.Status = nil
	challenge, err := h.client("").Runners().ChallengeWorker(h.ctx, token, entry)
	require.NoError(h.t, err)
	worker.Spec.Proof.Challenge = challenge.Challenge
	worker.Spec.Proof.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(challenge.Message)))
	return entry
}
