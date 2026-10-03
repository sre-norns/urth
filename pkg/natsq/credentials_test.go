package natsq_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	ns "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nkeys"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
)

func TestWorkerCredentialsEnforceAccountQueueIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	operator, err := nkeys.CreateOperator()
	require.NoError(t, err)
	opPub, err := operator.PublicKey()
	require.NoError(t, err)
	account, err := nkeys.CreateAccount()
	require.NoError(t, err)
	accountPub, err := account.PublicKey()
	require.NoError(t, err)
	ac := jwt.NewAccountClaims(accountPub)
	ac.Limits.JetStreamLimits = jwt.JetStreamLimits{MemoryStorage: 1 << 20, DiskStorage: 8 << 20, Streams: 10, Consumer: 20}
	accountJWT, err := ac.Encode(operator)
	require.NoError(t, err)
	resolver := &ns.MemAccResolver{}
	require.NoError(t, resolver.Store(accountPub, accountJWT))
	system, err := nkeys.CreateAccount()
	require.NoError(t, err)
	systemPub, err := system.PublicKey()
	require.NoError(t, err)
	systemJWT, err := jwt.NewAccountClaims(systemPub).Encode(operator)
	require.NoError(t, err)
	require.NoError(t, resolver.Store(systemPub, systemJWT))
	tlsConfig, caFile, certFile, keyFile := brokerTLS(t)
	broker, err := ns.NewServer(&ns.Options{TLSConfig: tlsConfig, Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), TrustedKeys: []string{opPub}, AccountResolver: resolver, SystemAccount: systemPub})
	require.NoError(t, err)

	go broker.Start()
	require.True(t, broker.ReadyForConnections(5*time.Second))
	t.Cleanup(broker.Shutdown)
	brokerURL := strings.Replace(broker.ClientURL(), "nats://", "tls://", 1)
	// Production TLS also rejects a missing trusted CA or missing client certificate.
	untrusted, err := nats.Connect(brokerURL, nats.Timeout(time.Second))
	if untrusted != nil {
		untrusted.Close()
	}
	require.Error(t, err)
	uncertified, err := nats.Connect(brokerURL, nats.RootCAs(caFile), nats.Timeout(time.Second))
	if uncertified != nil {
		uncertified.Close()
	}
	require.Error(t, err)
	admin, err := nkeys.CreateUser()
	require.NoError(t, err)
	adminPub, err := admin.PublicKey()
	require.NoError(t, err)
	adminClaims := jwt.NewUserClaims(adminPub)
	adminClaims.Permissions, err = natsq.ServicePermissions("provisioner")
	require.NoError(t, err)
	adminJWT, err := adminClaims.Encode(account)
	require.NoError(t, err)
	adminSeed, err := admin.Seed()
	require.NoError(t, err)
	creds, err := jwt.FormatUserConfig(adminJWT, adminSeed)
	require.NoError(t, err)
	seed, err := account.Seed()
	require.NoError(t, err)
	seedFile := filepath.Join(t.TempDir(), "account.seed")
	require.NoError(t, os.WriteFile(seedFile, seed, 0600))
	cfg := testConfig()
	cfg.URL = brokerURL
	cfg.AllowInsecure = false
	cfg.TLSCAFile = caFile
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile
	serviceDir := t.TempDir()
	cfg.CredsFile = filepath.Join(serviceDir, "provisioner.creds")
	require.NoError(t, os.WriteFile(cfg.CredsFile, creds, 0600))
	serviceCredential := func(role string) string {
		user, err := nkeys.CreateUser()
		require.NoError(t, err)
		public, err := user.PublicKey()
		require.NoError(t, err)
		claims := jwt.NewUserClaims(public)
		claims.Permissions, err = natsq.ServicePermissions(role)
		require.NoError(t, err)
		token, err := claims.Encode(account)
		require.NoError(t, err)
		seed, err := user.Seed()
		require.NoError(t, err)
		data, err := jwt.FormatUserConfig(token, seed)
		require.NoError(t, err)
		path := filepath.Join(serviceDir, role+".creds")
		require.NoError(t, os.WriteFile(path, data, 0600))
		return path
	}
	cfg.PublisherCredsFile = serviceCredential("publisher")
	cfg.ObserverCredsFile = serviceCredential("observer")
	cfg.WorkerAccountSeedFile = seedFile
	cfg.AllowInsecureWorkers = false
	runners := map[manifest.ResourceID]urth.Runner{
		"a": {ObjectMeta: manifest.ObjectMeta{UID: "a", Account: "11111111-1111-4111-8111-111111111111", Name: "same.name"}},
		"b": {ObjectMeta: manifest.ObjectMeta{UID: "b", Account: "22222222-2222-4222-8222-222222222222", Name: "same.name"}},
	}
	transport, err := natsq.NewScheduler(ctx, cfg, func(_ context.Context, id manifest.ResourceID) (urth.Runner, error) { return runners[id], nil })
	require.NoError(t, err)
	defer transport.Close()
	sessionExpiry := time.Now().Add(15 * time.Second)
	a, err := transport.ConnectionInfoFor(ctx, "a", "worker-a", sessionExpiry)
	require.NoError(t, err)
	b, err := transport.ConnectionInfoFor(ctx, "b", "worker-b", time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(5*time.Minute), b.Credential.ExpiresAt, 2*time.Second, "default broker revocation cap is five minutes")
	require.False(t, a.Credential.ExpiresAt.After(sessionExpiry), "broker authority must expire no later than the requested session")
	aBytes, err := natsq.WorkerCredentialBytes(a.Credential)
	require.NoError(t, err)
	require.NotEqual(t, a.Subject, b.Subject)
	require.NotEqual(t, a.Consumer, b.Consumer)
	// Exercise the same dynamic credential callback used by renewing workers.
	rotating, err := (natsq.ClientConfig{TLSCAFile: caFile, TLSCertFile: certFile, TLSKeyFile: keyFile, URL: brokerURL, InboxPrefix: a.InboxPrefix, CredentialSource: func() string { return string(aBytes) }}).Connect("renewable-worker")
	require.NoError(t, err)
	rotatingJS, err := jetstream.New(rotating)
	require.NoError(t, err)
	_, err = rotatingJS.Consumer(ctx, a.Stream, a.Consumer)
	require.NoError(t, err)
	rotating.Close()
	denied := make(chan error, 8)
	conn, err := nats.Connect(brokerURL, nats.RootCAs(caFile), nats.ClientCert(certFile, keyFile), nats.UserCredentialBytes([]byte(string(aBytes))), nats.CustomInboxPrefix(a.InboxPrefix), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) { denied <- err }))
	require.NoError(t, err)
	defer conn.Close()
	js, err := jetstream.New(conn)
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, a.Stream, a.Consumer)
	require.NoError(t, err)
	adminConn, err := cfg.PublisherConfig().Connect("test-publisher")
	require.NoError(t, err)
	defer adminConn.Close()
	adminJS, err := jetstream.New(adminConn)
	require.NoError(t, err)
	_, err = adminJS.Publish(ctx, a.Subject, []byte("own-account"))
	require.NoError(t, err)
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
	require.NoError(t, err)
	count := 0
	for msg := range batch.Messages() {
		count++
		require.Equal(t, "own-account", string(msg.Data()))
		require.NoError(t, msg.DoubleAck(ctx))
	}
	require.NoError(t, batch.Error())
	require.Equal(t, 1, count)
	assertDenied := func(action func() error) {
		t.Helper()
		require.NoError(t, action())
		require.NoError(t, conn.Flush())
		select {
		case err := <-denied:
			require.Contains(t, err.Error(), "Permissions Violation")
		case <-time.After(time.Second):
			t.Fatal("broker accepted a forbidden operation")
		}
	}
	assertDenied(func() error { _, err := conn.Subscribe(b.Subject, func(*nats.Msg) {}); return err })
	assertDenied(func() error {
		return conn.Publish("$JS.API.CONSUMER.MSG.NEXT."+b.Stream+"."+b.Consumer, []byte(`{"batch":1}`))
	})
	assertDenied(func() error { return conn.Publish(b.Subject, []byte("injection")) })
	assertDenied(func() error { _, err := conn.Subscribe("_INBOX.b.>", func(*nats.Msg) {}); return err })
	assertDenied(func() error { return conn.Publish(a.Subject, []byte("injection-own-runner")) })
	assertDenied(func() error { _, err := conn.Subscribe("urth.v1.events.>", func(*nats.Msg) {}); return err })
	assertDenied(func() error { return conn.Publish("$JS.API.STREAM.DELETE."+a.Stream, nil) })
	assertDenied(func() error { return conn.Publish("$JS.API.CONSUMER.DELETE."+a.Stream+"."+a.Consumer, nil) })
	for _, subject := range []string{
		"$JS.API.STREAM.CREATE." + a.Stream,
		"$JS.API.STREAM.UPDATE." + a.Stream,
		"$JS.API.CONSUMER.CREATE." + a.Stream + "." + a.Consumer,
	} {
		assertDenied(func() error { return conn.Publish(subject, []byte(`{}`)) })
	}
	assertDenied(func() error { return conn.Publish(natsq.PresenceSubject("a", "other-worker"), nil) })
	assertDenied(func() error { return conn.Publish(natsq.LogSubject("b", "result-b"), []byte("foreign-log")) })
	require.NoError(t, conn.Publish(natsq.PresenceSubject("a", "worker-a"), nil))
	require.NoError(t, conn.Publish("urth.v1.logs.a.result-a", []byte("own-log")))
	require.NoError(t, conn.Flush())
	select {
	case err := <-denied:
		t.Fatalf("broker denied permitted worker publication: %v", err)
	default:
	}
	// The broker disconnects an already-connected expired identity. It also
	// rejects that authority on new connections, not just in local token parsing.
	short, err := transport.ConnectionInfoFor(ctx, "a", "expiring-worker", time.Now().Add(3*time.Second))
	require.NoError(t, err)
	shortBytes, err := natsq.WorkerCredentialBytes(short.Credential)
	require.NoError(t, err)
	disconnected := make(chan struct{}, 1)
	expired, err := nats.Connect(brokerURL, nats.RootCAs(caFile), nats.ClientCert(certFile, keyFile), nats.UserCredentialBytes(shortBytes), nats.MaxReconnects(0), nats.ClosedHandler(func(*nats.Conn) { disconnected <- struct{}{} }))
	require.NoError(t, err)
	defer expired.Close()
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("broker retained expired connection")
	}
	require.Eventually(t, func() bool {
		connection, err := nats.Connect(brokerURL, nats.RootCAs(caFile), nats.ClientCert(certFile, keyFile), nats.UserCredentialBytes(shortBytes), nats.Timeout(time.Second))
		if err == nil {
			connection.Close()
			return false
		}
		return true
	}, 3*time.Second, 100*time.Millisecond)
	// The same callback the worker uses rotates credentials and reconnects the
	// live subscription to its existing consumer. Previously issued authority
	// does not regain validity.
	first, err := transport.ConnectionInfoFor(ctx, "a", "renewing-worker", time.Now().Add(3*time.Second))
	require.NoError(t, err)
	firstBytes, err := natsq.WorkerCredentialBytes(first.Credential)
	require.NoError(t, err)
	var credentialMu sync.RWMutex
	current := string(firstBytes)
	renewing, err := (natsq.ClientConfig{TLSCAFile: caFile, TLSCertFile: certFile, TLSKeyFile: keyFile, URL: brokerURL, InboxPrefix: first.InboxPrefix, CredentialSource: func() string { credentialMu.RLock(); defer credentialMu.RUnlock(); return current }}).Connect("renewing-worker")
	require.NoError(t, err)
	defer renewing.Close()
	renewed, err := transport.ConnectionInfoFor(ctx, "a", "renewing-worker", time.Now().Add(10*time.Second))
	require.NoError(t, err)
	renewedBytes, err := natsq.WorkerCredentialBytes(renewed.Credential)
	require.NoError(t, err)
	credentialMu.Lock()
	current = string(renewedBytes)
	credentialMu.Unlock()
	require.NoError(t, renewing.ForceReconnect())
	require.Eventually(t, renewing.IsConnected, 3*time.Second, 10*time.Millisecond)
	renewedJS, err := jetstream.New(renewing)
	require.NoError(t, err)
	_, err = renewedJS.Consumer(ctx, a.Stream, a.Consumer)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return time.Now().After(first.Credential.ExpiresAt) }, 4*time.Second, 10*time.Millisecond)
	require.True(t, renewing.IsConnected(), "renewed authority survives the original expiry")
	_, err = renewedJS.Consumer(ctx, a.Stream, a.Consumer)
	require.NoError(t, err)

}

func TestRunnerAddressEncodingKeepsNameDomainsSeparate(t *testing.T) {
	conn := startNATS(t)
	js := mustJetStream(t, conn)
	cfg := testConfig()
	ctx := context.Background()
	_, err := natsq.EnsureJobStream(ctx, js, cfg)
	require.NoError(t, err)
	account := manifest.ResourceID("11111111-1111-4111-8111-111111111111")
	long := strings.Repeat("a", 60) + "." + strings.Repeat("b", 60) + "." + strings.Repeat("c", 60)
	// This valid short name collided with the original truncation-plus-digest
	// implementation, which used a '-' marker from the short-name alphabet.
	encoded := strings.ReplaceAll(long, ".", "_")
	short := strings.ReplaceAll(fmt.Sprintf("%s-%x", encoded[:63], sha256.Sum256([]byte(long))), "_", ".")
	names := []string{"edge.eu", "edge-eu", long, short}
	seen := map[string]bool{}
	for _, name := range names {
		subject := natsq.JobSubject(account, manifest.ResourceName(name))
		require.Len(t, strings.Split(subject, "."), 5)
		require.False(t, seen[subject], "queue collision for %s", name)
		seen[subject] = true
		_, err := natsq.EnsureRunnerConsumer(ctx, js, cfg, account, manifest.ResourceName(name))
		require.NoError(t, err)
	}
}
