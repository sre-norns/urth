package integration

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	ns "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/sre-norns/urth/pkg/apiserver"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/identity"
	im "github.com/sre-norns/wyrd/identity/model"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm/clause"
)

// securedWorkerHarness adds real operator/account JWTs and mutual TLS to the
// existing dispatch harness. Product endpoints still issue all worker authority.
func securedWorkerHarness(t *testing.T, ttl time.Duration) harnessOption {
	t.Helper()
	operator, err := nkeys.CreateOperator()
	require.NoError(t, err)
	op, err := operator.PublicKey()
	require.NoError(t, err)
	account, err := nkeys.CreateAccount()
	require.NoError(t, err)
	accountPublic, err := account.PublicKey()
	require.NoError(t, err)
	ac := jwt.NewAccountClaims(accountPublic)
	ac.Limits.JetStreamLimits = jwt.JetStreamLimits{MemoryStorage: 1 << 20, DiskStorage: 8 << 20, Streams: 10, Consumer: 20}
	token, err := ac.Encode(operator)
	require.NoError(t, err)
	resolver := &ns.MemAccResolver{}
	require.NoError(t, resolver.Store(accountPublic, token))
	system, err := nkeys.CreateAccount()
	require.NoError(t, err)
	systemPublic, err := system.PublicKey()
	require.NoError(t, err)
	systemJWT, err := jwt.NewAccountClaims(systemPublic).Encode(operator)
	require.NoError(t, err)
	require.NoError(t, resolver.Store(systemPublic, systemJWT))
	dir := t.TempDir()
	serviceFile := func(role string) string {
		user, err := nkeys.CreateUser()
		require.NoError(t, err)
		public, err := user.PublicKey()
		require.NoError(t, err)
		claims := jwt.NewUserClaims(public)
		claims.Permissions, err = natsq.ServicePermissions(role)
		require.NoError(t, err)
		encoded, err := claims.Encode(account)
		require.NoError(t, err)
		seed, err := user.Seed()
		require.NoError(t, err)
		creds, err := jwt.FormatUserConfig(encoded, seed)
		require.NoError(t, err)
		path := filepath.Join(dir, role+".creds")
		require.NoError(t, os.WriteFile(path, creds, 0600))
		return path
	}
	provisioner, publisher, observer := serviceFile("provisioner"), serviceFile("publisher"), serviceFile("observer")
	seed, err := account.Seed()
	require.NoError(t, err)
	seedFile := filepath.Join(dir, "account.seed")
	require.NoError(t, os.WriteFile(seedFile, seed, 0600))
	public, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, key)
	require.NoError(t, err)
	encodedKey, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	caFile, certFile, keyFile := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "client.pem"), filepath.Join(dir, "client.key")
	require.NoError(t, os.WriteFile(caFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(certFile, certPEM, 0600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0600))
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))
	return func(s *harnessSettings) {
		s.secureHTTP = true
		s.brokerOptions = func(opts *ns.Options) {
			opts.TrustedKeys = []string{op}
			opts.AccountResolver = resolver
			opts.SystemAccount = systemPublic
			opts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}
		}
		s.configure = append(s.configure, func(cfg *apiserver.Config) {
			cfg.NATS.AllowInsecure = false
			cfg.NATS.AllowInsecureWorkers = false
			cfg.NATS.CredsFile = provisioner
			cfg.NATS.PublisherCredsFile = publisher
			cfg.NATS.ObserverCredsFile = observer
			cfg.NATS.WorkerAccountSeedFile = seedFile
			cfg.NATS.WorkerCredentialTTL = ttl
			cfg.NATS.TLSCAFile = caFile
			cfg.NATS.TLSCertFile = certFile
			cfg.NATS.TLSKeyFile = keyFile
		})
	}
}

func TestWorkerBlockAndDeleteComposeWithSecuredBroker(t *testing.T) {
	h := newHarness(t, securedWorkerHarness(t, 3*time.Second))
	runner := h.applyRunner("secure-runner", nil)
	scenario := h.applyScenario("secure-probe", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "secure-worker"}, Spec: &urth.WorkerInstanceSpec{}}
	entry = h.signedWorker(token, entry, key)
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, entry)
	require.NoError(t, err)
	worker, err := urth.NewWorkerInstance(registration.Worker)
	require.NoError(t, err)
	require.Empty(t, registration.NATS.Credential.Value, "the API never sends a server credentials path")
	cfg := h.Config.NATS.ClientConfig
	cfg.CredsFile = ""
	cfg.InboxPrefix = registration.NATS.InboxPrefix
	data, err := natsq.WorkerCredentialBytes(registration.NATS.Credential)
	require.NoError(t, err)
	cfg.UserCredentials = string(data)
	closed := make(chan struct{}, 1)
	connection, err := nats.Connect(cfg.URL, nats.RootCAs(cfg.TLSCAFile), nats.ClientCert(cfg.TLSCertFile, cfg.TLSKeyFile), nats.UserCredentialBytes(data), nats.CustomInboxPrefix(cfg.InboxPrefix), nats.MaxReconnects(0), nats.ClosedHandler(func(*nats.Conn) { closed <- struct{}{} }))
	require.NoError(t, err)
	defer connection.Close()
	run := h.createRun(scenario.Name)
	capability, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(t, err)
	current, found, err := h.client("").Runners().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	updated, err := urth.NewRunner(current)
	require.NoError(t, err)
	updated.Spec.BlockedWorkers = []urth.BlockedWorker{{Identity: worker.Status.Fingerprint, Reason: "test revocation"}}
	blocked, err := h.client("").Runners().Update(h.ctx, current.Metadata.GetVersionedID(), updated.ToManifest())
	require.NoError(t, err)
	next := h.createRun(scenario.Name)
	request := urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(next.UID, next.Version), ResultVersion: next.Version}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	code, _ := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", next.UID), registration.Session, body)
	require.Equal(t, 403, code, "blocked active session attempted a new claim")
	entry.Status = nil
	entry.Spec = &urth.WorkerInstanceSpec{Proof: &urth.WorkerProof{PublicKey: workerPublicKey(key)}}
	_, err = h.client("").Runners().ChallengeWorker(h.ctx, token, entry)
	require.Error(t, err, "block prevents a fresh proof challenge")
	// An already claimed run reports under its own deadline after blocking.
	_, err = h.client("").Results(scenario.Name).UpdateStatus(h.ctx, capability.VersionedResourceID, capability.Token, urth.ResultStatus{Status: urth.JobCompleted})
	require.NoError(t, err)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked worker retained broker connection beyond issued expiry")
	}
	// Removing a block uses the latest version. Deletion revokes a registration,
	// while a still-authorized installation may create a new UID.
	latest, err := urth.NewRunner(blocked)
	require.NoError(t, err)
	latest.Spec.BlockedWorkers = nil
	_, err = h.client("").Runners().Update(h.ctx, blocked.Metadata.GetVersionedID(), latest.ToManifest())
	require.NoError(t, err)
	inFlight, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, next.UID, registration.Session, request)
	require.NoError(t, err)
	deleted, err := h.client("").Workers().Delete(h.ctx, worker.GetVersionedID())
	require.NoError(t, err)
	require.True(t, deleted)
	code, _ = h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", next.UID), registration.Session, body)
	require.Equal(t, 403, code)
	_, err = h.client("").Results(scenario.Name).UpdateStatus(h.ctx, inFlight.VersionedResourceID, inFlight.Token, urth.ResultStatus{Status: urth.JobCompleted})
	require.NoError(t, err, "deletion preserves the bounded in-flight capability")
	_, err = h.client("").Artifacts().Create(h.ctx, inFlight.Token, urth.Artifact{ObjectMeta: manifest.ObjectMeta{Name: "after-deletion-log"}, Spec: urth.ArtifactSpec{Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain", Content: []byte("bounded report after deletion")}}}.ToManifest())
	require.NoError(t, err, "deletion preserves bounded artifact reporting after completion")
	newRegistration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
	require.NoError(t, err)
	require.NotEqual(t, worker.UID, newRegistration.Worker.Metadata.UID)
}

func workerPublicKey(key ed25519.PrivateKey) string {
	return base64.RawURLEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
}

// A recording wrapper observes the production HTTP enrollment responses. It
// issues no credentials and preserves both proof and transport checks.
type registrationRecorder struct {
	urth.Service
	registrations chan urth.WorkerRegistrationResponse
}
type runnersRegistrationRecorder struct {
	urth.RunnersAPI
	registrations chan urth.WorkerRegistrationResponse
}

func (r registrationRecorder) Runners() urth.RunnersAPI {
	return runnersRegistrationRecorder{r.Service.Runners(), r.registrations}
}
func (r runnersRegistrationRecorder) AuthWorker(ctx context.Context, token urth.APIToken, entry manifest.ResourceManifest) (urth.WorkerRegistrationResponse, error) {
	response, err := r.RunnersAPI.AuthWorker(ctx, token, entry)
	if err == nil {
		select {
		case r.registrations <- response:
		default:
		}
	}
	return response, err
}
func TestWorkerRenewsAndReconnectsWithoutInterruptingInFlightRun(t *testing.T) {
	testWorkerRebind(t, false)
}
func TestWorkerRebindsDeletedRegistrationWithoutInterruptingInFlightRun(t *testing.T) {
	testWorkerRebind(t, true)
}
func testWorkerRebind(t *testing.T, deleteRegistration bool) {
	h := newHarness(t, securedWorkerHarness(t, 3*time.Second))
	runner := h.applyRunner("renewing-runner", nil)
	scenario := h.applyScenario("renewing-probe", testProbSpec{}, manifest.LabelSelector{})
	firstRun := h.createRun(scenario.Name)
	observer, err := h.Config.NATS.ObserverConfig().Connect("rebind-observer")
	require.NoError(t, err)
	defer observer.Close()
	presence, err := observer.SubscribeSync(natsq.AllPresenceSubjects)
	require.NoError(t, err)
	require.NoError(t, observer.Flush())
	registrations := make(chan urth.WorkerRegistrationResponse, 32)
	started := make(chan manifest.ResourceID, 8)
	reported := make(chan error, 8)
	release := make(chan struct{})
	defer close(release)
	h.startWorker(runner.Name, withClientWrapper(func(api urth.Service) urth.Service { return registrationRecorder{api, registrations} }), withProbeRunner(func(ctx context.Context, envelope natsq.DispatchEnvelope, auth urth.AuthJobResponse) {
		started <- envelope.ResultUID
		if envelope.ResultUID == firstRun.UID {
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
		}
		_, err := h.client("").Results(scenario.Name).UpdateStatus(ctx, auth.VersionedResourceID, auth.Token, urth.ResultStatus{Status: urth.JobCompleted})
		reported <- err
	}))
	_, err = h.relayOnce()
	require.NoError(t, err)
	select {
	case id := <-started:
		require.Equal(t, firstRun.UID, id)
	case <-time.After(10 * time.Second):
		t.Fatal("first run did not start")
	}
	initial := <-registrations
	if deleteRegistration {
		current, found, err := h.client("").Workers().Get(h.ctx, initial.Worker.Metadata.Name)
		require.NoError(t, err)
		require.True(t, found)
		deleted, err := h.client("").Workers().Delete(h.ctx, current.Metadata.GetVersionedID())
		require.NoError(t, err)
		require.True(t, deleted)
	}
	var renewed urth.WorkerRegistrationResponse
	deadline := time.After(10 * time.Second)
	waiting := true
	for waiting {
		select {
		case renewed = <-registrations:
			waiting = deleteRegistration && renewed.Worker.Metadata.UID == initial.Worker.Metadata.UID
		case <-deadline:
			t.Fatal("worker did not renew/re-enroll short broker authority")
		}
	}
	if deleteRegistration {
		require.NotEqual(t, initial.Worker.Metadata.UID, renewed.Worker.Metadata.UID)
	} else {
		require.Equal(t, initial.Worker.Metadata.UID, renewed.Worker.Metadata.UID)
	}
	require.Eventually(t, func() bool {
		message, err := presence.NextMsg(100 * time.Millisecond)
		return err == nil && message.Subject == natsq.PresenceSubject(runner.UID, renewed.Worker.Metadata.UID)
	}, 5*time.Second, 10*time.Millisecond, "presence must use the active registration")
	require.NotEqual(t, initial.NATS.Credential.JWT, renewed.NATS.Credential.JWT)
	require.NotEqual(t, initial.NATS.Credential.Seed, renewed.NATS.Credential.Seed)
	require.LessOrEqual(t, initial.NATS.Credential.ExpiresAt.Unix(), initial.SessionExpiresAt.Unix())
	require.Eventually(t, func() bool { return time.Now().After(initial.NATS.Credential.ExpiresAt.Add(time.Second)) }, 5*time.Second, 10*time.Millisecond)
	secondRun := h.createRun(scenario.Name)
	_, err = h.relayOnce()
	require.NoError(t, err)
	select {
	case id := <-started:
		require.Equal(t, secondRun.UID, id)
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not fetch after original broker authority expired")
	}
	select {
	case err := <-reported:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("second run did not report")
	}
	// The first run remains executing through rotation and reconnect. Its original
	// capability still reports after the original broker authority expires.
	release <- struct{}{}
	select {
	case err := <-reported:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight run did not report after credential renewal")
	}
	require.Equal(t, urth.JobCompleted, h.result(firstRun.UID).Status.Status)
	require.Equal(t, urth.JobCompleted, h.result(secondRun.UID).Status.Status)
	require.Equal(t, renewed.Worker.Metadata.UID, h.result(secondRun.UID).Status.Executor.WorkerID)
	select {
	case id := <-started:
		t.Fatalf("duplicate execution during renewal: %s", id)
	default:
	}
}

func TestBlocklistCommitPrecedesWaitingWorkerClaim(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("serial-runner", nil)
	scenario := h.applyScenario("serial-probe", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "serial-worker"}, Spec: &urth.WorkerInstanceSpec{}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
	require.NoError(t, err)
	worker, err := urth.NewWorkerInstance(registration.Worker)
	require.NoError(t, err)
	run := h.createRun(scenario.Name)
	tx := h.DB.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	var blocker int
	require.NoError(t, tx.Raw("SELECT pg_backend_pid()").Scan(&blocker).Error)
	var locked urth.Runner
	require.NoError(t, tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uid = ?", runner.UID).First(&locked).Error)
	locked.Spec.BlockedWorkers = []urth.BlockedWorker{{Identity: worker.Status.Fingerprint, Reason: "concurrent block"}}
	require.NoError(t, tx.Omit(clause.Associations).Save(&locked).Error)
	requestBody, err := json.Marshal(urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(t, err)
	result := make(chan int, 1)
	go func() {
		code, _ := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", run.UID), registration.Session, requestBody)
		result <- code
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		err := h.DB.Raw("SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid)))", blocker).Scan(&waiting).Error
		return err == nil && waiting
	}, 3*time.Second, 10*time.Millisecond, "claim must wait for the Runner blocklist write")
	require.NoError(t, tx.Commit().Error)
	select {
	case code := <-result:
		require.Equal(t, 403, code)
	case <-time.After(5 * time.Second):
		t.Fatal("claim did not finish after blocklist commit")
	}
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)
}

func TestExpiredWorkerSessionKeepsOnlyBoundedInFlightAuthority(t *testing.T) {
	h := newHarness(t, securedWorkerHarness(t, 3*time.Second))
	runner := h.applyRunner("expiring-runner", nil)
	scenario := h.applyScenario("expiring-probe", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "expiring-worker"}, Spec: &urth.WorkerInstanceSpec{RequestedTTL: 2 * time.Second}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
	require.NoError(t, err)
	require.False(t, registration.NATS.Credential.ExpiresAt.After(registration.SessionExpiresAt))
	run := h.createRun(scenario.Name)
	capability, err := h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return time.Now().After(registration.SessionExpiresAt.Add(time.Second)) }, 5*time.Second, 10*time.Millisecond)
	next := h.createRun(scenario.Name)
	request := urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(next.UID, next.Version), ResultVersion: next.Version}
	body, err := json.Marshal(request)
	require.NoError(t, err)
	code, _ := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", next.UID), registration.Session, body)
	require.Equal(t, 403, code)
	_, err = h.client("").Results(scenario.Name).UpdateStatus(h.ctx, capability.VersionedResourceID, capability.Token, urth.ResultStatus{Status: urth.JobCompleted})
	require.NoError(t, err)
	require.Equal(t, urth.JobCompleted, h.result(run.UID).Status.Status)
}

func TestWorkerBlocklistCLIUsesVersionedCanonicalRunner(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("cli-runner", nil)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	fingerprint := urth.WorkerFingerprint(key.Public().(ed25519.PublicKey))
	_, err = h.urthctl("", "runners", "block", string(runner.Name), fingerprint, "--reason=Retired host")
	require.NoError(t, err)
	blocked, found, err := h.client("").Runners().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	parsed, err := urth.NewRunner(blocked)
	require.NoError(t, err)
	require.Equal(t, []urth.BlockedWorker{{Identity: fingerprint, Reason: "Retired host"}}, parsed.Spec.BlockedWorkers)
	require.Greater(t, parsed.Version, runner.Version)
	_, err = h.urthctl("", "runners", "unblock", string(runner.Name), fingerprint)
	require.NoError(t, err)
	current, found, err := h.client("").Runners().Get(h.ctx, runner.Name)
	require.NoError(t, err)
	require.True(t, found)
	final, err := urth.NewRunner(current)
	require.NoError(t, err)
	require.Empty(t, final.Spec.BlockedWorkers)
	require.Greater(t, final.Version, parsed.Version)
}

func TestRunnerLockDatabaseFailureLeavesClaimRetryable(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("lock-failure-runner", nil)
	scenario := h.applyScenario("lock-failure-probe", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "lock-failure-worker"}, Spec: &urth.WorkerInstanceSpec{}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, nil))
	require.NoError(t, err)
	run := h.createRun(scenario.Name)
	body, err := json.Marshal(urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(t, err)
	const callback = "m9:fail-runner-lock"
	require.NoError(t, h.DB.Callback().Query().Before("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "runners" && db.Statement.Clauses["FOR"].Expression != nil {
			db.AddError(errors.New("injected runner lock database outage"))
		}
	}))
	defer h.DB.Callback().Query().Remove(callback)
	code, _ := h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", run.UID), registration.Session, body)
	require.Equal(t, 503, code, "database failure must leave the queued claim retryable")
	require.NoError(t, h.DB.Callback().Query().Remove(callback))
	require.Equal(t, urth.JobPending, h.result(run.UID).Status.Status)
	code, _ = h.httpRequest("POST", fmt.Sprintf("/v1/auth/runs/%s/claim", run.UID), registration.Session, body)
	require.Equal(t, 200, code, "the same dispatch succeeds after recovery")
}

func TestWorkerEnrollmentAuthenticatesBeforeManifestParsing(t *testing.T) {
	h := newHarness(t)
	runner := h.applyRunner("enrollment-gate-runner", nil)
	token := h.enrolmentToken(runner.Name)
	for _, path := range []string{"/v1/auth/workers/challenge", "/v1/auth/workers"} {
		for _, tc := range []struct {
			token urth.APIToken
			code  int
		}{{"", 401}, {h.token, 401}, {token, 400}} {
			request, err := http.NewRequest("POST", h.HTTP.URL+path, bytes.NewBufferString("malformed manifest"))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+string(tc.token))
			}
			response, err := h.HTTP.Client().Do(request)
			require.NoError(t, err)
			response.Body.Close()
			require.Equal(t, tc.code, response.StatusCode)
			require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
		}
	}
}

func TestMachineTokenRevocationDeniesFreshProofButPreservesSession(t *testing.T) {
	h := newHarness(t, securedWorkerHarness(t, 3*time.Second))
	runner := h.applyRunner("rotated-token-runner", nil)
	scenario := h.applyScenario("rotated-token-probe", testProbSpec{}, manifest.LabelSelector{})
	token := h.enrolmentToken(runner.Name)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	entry := manifest.ResourceManifest{TypeMeta: manifest.TypeMeta{APIVersion: urth.APIVersion, Kind: urth.KindWorkerInstance}, Metadata: manifest.ObjectMeta{Name: "rotated-token-worker"}, Spec: &urth.WorkerInstanceSpec{}}
	registration, err := h.client("").Runners().AuthWorker(h.ctx, token, h.signedWorker(token, entry, key))
	require.NoError(t, err)
	proof := h.signedWorker(token, entry, key)
	tokens, _, err := h.Server.Identity.MachineTokens().List(h.ctx, im.AgentIdentityID(runner.UID), manifest.SearchQuery{})
	require.NoError(t, err)
	require.Len(t, tokens, 1)
	revoked := tokens[0]
	revoked.Status = "revoked"
	ctx := identity.WithRequest(h.ctx, identity.Request{IfMatch: identity.ETag(revoked.Revision)})
	_, _, err = h.Server.Identity.MachineTokens().CreateOrUpdate(ctx, revoked)
	require.NoError(t, err)
	body, err := json.Marshal(proof)
	require.NoError(t, err)
	for _, path := range []string{"/v1/auth/workers/challenge", "/v1/auth/workers"} {
		code, _ := h.httpRequest("POST", path, token, body)
		require.Equal(t, 401, code, "revoked enrollment authority must fail even with an unused valid proof")
	}
	run := h.createRun(scenario.Name)
	_, err = h.client("").Results(scenario.Name).ClaimRun(h.ctx, run.UID, registration.Session, urth.ClaimJobRequest{DispatchID: urth.DispatchEventUID(run.UID, run.Version), ResultVersion: run.Version})
	require.NoError(t, err, "session authority is independent of its enrollment token")
	replacement := h.enrolmentToken(runner.Name)
	renewed, err := h.client("").Runners().AuthWorker(h.ctx, replacement, h.signedWorker(replacement, entry, key))
	require.NoError(t, err)
	require.Equal(t, registration.Worker.Metadata.UID, renewed.Worker.Metadata.UID)
}
