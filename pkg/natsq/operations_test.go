package natsq_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	mathrand "math/rand/v2"
	"net"
	"net/url"
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

// These tests run real broker servers in this test process. Every listener
// and store belongs to the test; no external broker or database is used.
type brokerAuthority struct {
	operator, account, system                   nkeys.KeyPair
	accountPublic, systemPublic, operatorPublic string
	claims                                      *jwt.AccountClaims
}

func newBrokerAuthority(t *testing.T) *brokerAuthority {
	t.Helper()
	a := &brokerAuthority{}
	var err error
	a.operator, err = nkeys.CreateOperator()
	require.NoError(t, err)
	a.account, err = nkeys.CreateAccount()
	require.NoError(t, err)
	a.system, err = nkeys.CreateAccount()
	require.NoError(t, err)
	t.Cleanup(func() { a.operator.Wipe(); a.account.Wipe(); a.system.Wipe() })
	a.operatorPublic, err = a.operator.PublicKey()
	require.NoError(t, err)
	a.accountPublic, err = a.account.PublicKey()
	require.NoError(t, err)
	a.systemPublic, err = a.system.PublicKey()
	require.NoError(t, err)
	a.claims = jwt.NewAccountClaims(a.accountPublic)
	a.claims.Limits.JetStreamLimits = jwt.JetStreamLimits{MemoryStorage: 1 << 20, DiskStorage: 32 << 20, Streams: 10, Consumer: 20}
	return a
}

func (a *brokerAuthority) resolver(t *testing.T) *ns.DirAccResolver {
	t.Helper()
	r, err := ns.NewDirAccResolver(t.TempDir(), 100, time.Minute, ns.NoDelete)
	require.NoError(t, err)
	for public, claims := range map[string]*jwt.AccountClaims{a.accountPublic: a.claims, a.systemPublic: jwt.NewAccountClaims(a.systemPublic)} {
		token, err := claims.Encode(a.operator)
		require.NoError(t, err)
		require.NoError(t, r.Store(public, token))
	}
	return r
}

func brokerUser(t *testing.T, signer nkeys.KeyPair, account, role string) string {
	t.Helper()
	user, err := nkeys.CreateUser()
	require.NoError(t, err)
	defer user.Wipe()
	public, err := user.PublicKey()
	require.NoError(t, err)
	claims := jwt.NewUserClaims(public)
	claims.IssuerAccount = account
	if role != "" {
		claims.Permissions, err = natsq.ServicePermissions(role)
		require.NoError(t, err)
	}
	token, err := claims.Encode(signer)
	require.NoError(t, err)
	seed, err := user.Seed()
	require.NoError(t, err)
	data, err := jwt.FormatUserConfig(token, seed)
	require.NoError(t, err)
	return string(data)
}

func startOperationsBroker(t *testing.T, opts *ns.Options) *ns.Server {
	t.Helper()
	s, err := ns.NewServer(opts)
	require.NoError(t, err)
	t.Cleanup(func() { s.Shutdown(); s.WaitForShutdown() })
	// ReadyForConnections says only "no". The server's own errors say why --
	// a listener that failed to bind, for one -- and are otherwise discarded.
	errs := &brokerErrors{}
	s.SetLoggerV2(errs, false, false, false)
	go s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second), "broker %s is not ready: %s", opts.ServerName, errs)
	return s
}

// brokerErrors keeps the first errors an embedded broker reports.
type brokerErrors struct {
	mu    sync.Mutex
	lines []string
}

func (e *brokerErrors) add(format string, v ...any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.lines) < 32 {
		e.lines = append(e.lines, fmt.Sprintf(format, v...))
	}
}

func (e *brokerErrors) String() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.Join(e.lines, "; ")
}

func (e *brokerErrors) Fatalf(format string, v ...any) { e.add(format, v...) }
func (e *brokerErrors) Errorf(format string, v ...any) { e.add(format, v...) }
func (e *brokerErrors) Noticef(string, ...any)         {}
func (e *brokerErrors) Warnf(string, ...any)           {}
func (e *brokerErrors) Debugf(string, ...any)          {}
func (e *brokerErrors) Tracef(string, ...any)          {}

// loadAccountEverywhere has every broker load the account, with its JetStream,
// before any client of it connects.
//
// nats-server (2.15.0) loads an account lazily, on the first thing to name it.
// On a node the client did not dial, that is two things at once: the client's
// subscription interest crossing a route, and a JetStream request or stream
// assignment for the account. The concurrent loads can expose the account
// half-built, and measured outcomes include 503 "JetStream not enabled for
// account", 503 "account not found" from a replica, and a stream create that is
// never answered. These tests are about failover and route rotation, not first
// contact, so they start from a cluster that already knows the account. Loaded
// from here, with nothing else asking, the load is not concurrent.
func loadAccountEverywhere(t *testing.T, account string, servers ...*ns.Server) {
	t.Helper()
	for _, s := range servers {
		acc, err := s.LookupAccount(account)
		require.NoError(t, err)
		require.True(t, acc.JetStreamEnabled(), "%s did not enable JetStream for the account", s.Name())
	}
}

// clusterRoutes chooses route addresses for brokers that must name each other
// before any of them starts; a JetStream cluster node refuses to start with no
// configured route, so a node cannot simply bind port 0 and be told about later.
//
// The ports come from below Linux's ephemeral range (32768-60999). Taking one
// from the kernel with port 0 and releasing it, as these tests used to, leaves
// it free for the kernel to hand straight to another listener -- this test's
// own client ports, or another package's under `go test ./...` -- before the
// broker binds it. The route listener then fails, and the broker never becomes
// ready. nats-server's own cluster tests avoid that range for the same reason.
func clusterRoutes(t *testing.T, n int) []*url.URL {
	t.Helper()
	var routes []*url.URL
	seen := map[int]bool{}
	for attempt := 0; len(routes) < n; attempt++ {
		require.Less(t, attempt, 1000, "no free route port below the ephemeral range")
		port := 20000 + mathrand.IntN(12000)
		if seen[port] {
			continue
		}
		// Skip a port something long-lived already holds.
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		require.NoError(t, listener.Close())
		seen[port] = true
		routes = append(routes, &url.URL{Scheme: "nats-route", Host: listener.Addr().String()})
	}
	return routes
}

func brokerURL(s *ns.Server) string { return strings.Replace(s.ClientURL(), "nats://", "tls://", 1) }

func (a *brokerAuthority) scheduler(t *testing.T, ctx context.Context, client natsq.ClientConfig, signer nkeys.KeyPair, replicas int) (natsq.Transport, natsq.Config) {
	t.Helper()
	cfg := testConfig()
	cfg.ClientConfig = client
	cfg.AllowInsecureWorkers = false
	cfg.WorkerAccountPublicKey = a.accountPublic
	cfg.Replicas = replicas
	dir := t.TempDir()
	seed, err := signer.Seed()
	require.NoError(t, err)
	cfg.WorkerAccountSeedFile = filepath.Join(dir, "worker-signer.seed")
	require.NoError(t, os.WriteFile(cfg.WorkerAccountSeedFile, seed, 0600))
	for role, target := range map[string]*string{"provisioner": &cfg.CredsFile, "publisher": &cfg.PublisherCredsFile, "observer": &cfg.ObserverCredsFile} {
		*target = filepath.Join(dir, role+".creds")
		require.NoError(t, os.WriteFile(*target, []byte(brokerUser(t, a.account, a.accountPublic, role)), 0600))
	}
	runner := urth.Runner{ObjectMeta: manifest.ObjectMeta{UID: "runner-operations", Account: "11111111-1111-4111-8111-111111111111", Name: "operations"}}
	transport, err := natsq.NewScheduler(ctx, cfg, func(context.Context, manifest.ResourceID) (urth.Runner, error) { return runner, nil })
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	return transport, cfg
}

func issuedBrokerClient(t *testing.T, ctx context.Context, transport natsq.Transport, client natsq.ClientConfig) (*nats.Conn, urth.NATSConnectionInfo) {
	t.Helper()
	return issuedBrokerClientUntil(t, ctx, transport, client, time.Now().Add(time.Minute))
}

func issuedBrokerClientUntil(t *testing.T, ctx context.Context, transport natsq.Transport, client natsq.ClientConfig, expiry time.Time) (*nats.Conn, urth.NATSConnectionInfo) {
	t.Helper()
	info, err := transport.ConnectionInfoFor(ctx, "runner-operations", "worker-operations", expiry)
	require.NoError(t, err)
	data, err := natsq.WorkerCredentialBytes(info.Credential)
	require.NoError(t, err)
	client.UserCredentials = string(data)
	client.InboxPrefix = info.InboxPrefix
	conn, err := client.Connect("operations-worker")
	require.NoError(t, err)
	t.Cleanup(conn.Close)
	return conn, info
}

func TestBrokerAccountSigningKeyRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a := newBrokerAuthority(t)
	oldSigner, err := nkeys.CreateAccount()
	require.NoError(t, err)
	defer oldSigner.Wipe()
	newSigner, err := nkeys.CreateAccount()
	require.NoError(t, err)
	defer newSigner.Wipe()
	oldPublic, err := oldSigner.PublicKey()
	require.NoError(t, err)
	newPublic, err := newSigner.PublicKey()
	require.NoError(t, err)
	a.claims.SigningKeys.Add(oldPublic, newPublic)
	tlsConfig, ca, cert, key := brokerTLS(t)
	s := startOperationsBroker(t, &ns.Options{TLSConfig: tlsConfig, Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), TrustedOperators: []*jwt.OperatorClaims{jwt.NewOperatorClaims(a.operatorPublic)}, AccountResolver: a.resolver(t), SystemAccount: a.systemPublic})
	client := natsq.ClientConfig{URL: brokerURL(s), TLSCAFile: ca, TLSCertFile: cert, TLSKeyFile: key}
	transport, cfg := a.scheduler(t, ctx, client, oldSigner, 1)
	old, info := issuedBrokerClient(t, ctx, transport, client)
	js, err := jetstream.New(old)
	require.NoError(t, err)
	_, err = js.Consumer(ctx, info.Stream, info.Consumer)
	require.NoError(t, err)
	oldBytes, err := natsq.WorkerCredentialBytes(info.Credential)
	require.NoError(t, err)
	var credentialMu sync.RWMutex
	current := string(oldBytes)
	rotatingClient := client
	rotatingClient.InboxPrefix = info.InboxPrefix
	rotatingClient.CredentialSource = func() string { credentialMu.RLock(); defer credentialMu.RUnlock(); return current }
	renewing, err := rotatingClient.Connect("signing-key-renewal")
	require.NoError(t, err)
	defer renewing.Close()
	// The API reads the signing seed for each issue. Replace it atomically, as
	// an operator does, without restarting the API or changing the account.
	seed, err := newSigner.Seed()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfg.WorkerAccountSeedFile+".next", seed, 0600))
	require.NoError(t, os.Rename(cfg.WorkerAccountSeedFile+".next", cfg.WorkerAccountSeedFile))
	newConn, newInfo := issuedBrokerClient(t, ctx, transport, client)
	claims, err := jwt.DecodeUserClaims(newInfo.Credential.JWT)
	require.NoError(t, err)
	require.Equal(t, newPublic, claims.Issuer)
	require.Equal(t, a.accountPublic, claims.IssuerAccount)
	require.True(t, old.IsConnected(), "overlap keeps old authority valid")
	newBytes, err := natsq.WorkerCredentialBytes(newInfo.Credential)
	require.NoError(t, err)
	credentialMu.Lock()
	current = string(newBytes)
	credentialMu.Unlock()
	require.NoError(t, renewing.ForceReconnect())
	require.Eventually(t, renewing.IsConnected, 3*time.Second, 10*time.Millisecond)
	// Apply the signed resolver update through the broker's operator protocol.
	systemClient := client
	systemClient.UserCredentials = brokerUser(t, a.system, a.systemPublic, "")
	system, err := systemClient.Connect("operations-system")
	require.NoError(t, err)
	defer system.Close()
	a.claims.SigningKeys.Remove(oldPublic)
	a.claims.IssuedAt++ // account update must be newer than the resolver entry
	accountJWT, err := a.claims.Encode(a.operator)
	require.NoError(t, err)
	reply, err := system.RequestWithContext(ctx, "$SYS.REQ.CLAIMS.UPDATE", []byte(accountJWT))
	require.NoError(t, err)
	var response ns.ServerAPIClaimUpdateResponse
	require.NoError(t, json.Unmarshal(reply.Data, &response))
	require.Nil(t, response.Error)
	require.Eventually(t, func() bool { return !old.IsConnected() }, 3*time.Second, 10*time.Millisecond, "removing signing key disconnects active authority")
	rejected, err := nats.Connect(client.URL, nats.RootCAs(ca), nats.ClientCert(cert, key), nats.UserCredentialBytes(oldBytes), nats.NoReconnect(), nats.Timeout(time.Second))
	if rejected != nil {
		rejected.Close()
	}
	require.Error(t, err, "retired signer must not reconnect")
	require.True(t, newConn.IsConnected())
	require.True(t, renewing.IsConnected())
	newJS, err := jetstream.New(renewing)
	require.NoError(t, err)
	consumer, err := newJS.Consumer(ctx, newInfo.Stream, newInfo.Consumer)
	require.NoError(t, err)
	publisher, err := cfg.PublisherConfig().Connect("rotation-publisher")
	require.NoError(t, err)
	defer publisher.Close()
	pubJS, err := jetstream.New(publisher)
	require.NoError(t, err)
	_, err = pubJS.Publish(ctx, info.Subject, []byte("after-signer-rotation"))
	require.NoError(t, err)
	batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
	require.NoError(t, err)
	count := 0
	for msg := range batch.Messages() {
		count++
		require.Equal(t, "after-signer-rotation", string(msg.Data()))
		require.NoError(t, msg.DoubleAck(ctx))
	}
	require.NoError(t, batch.Error())
	require.Equal(t, 1, count)
	t.Log("overlap, atomic issuance switch, live credential renewal, resolver key retirement, active disconnect, retired reconnect denial, delivery and confirmed ack pass")
}

type operationCA struct {
	cert *x509.Certificate
	key  ed25519.PrivateKey
	pem  []byte
}

func newOperationCA(t *testing.T, serial int64) operationCA {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, public, key)
	require.NoError(t, err)
	cert, err = x509.ParseCertificate(der)
	require.NoError(t, err)
	return operationCA{cert: cert, key: key, pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

func (ca operationCA) leaf(t *testing.T, serial int64) ([]byte, []byte) {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	cert := &x509.Certificate{SerialNumber: big.NewInt(serial), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, cert, ca.cert, public, ca.key)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
}

func replaceOperationFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path+".next", data, 0600))
	require.NoError(t, os.Rename(path+".next", path))
}

func TestBrokerCertificateRotation(t *testing.T) {
	a := newBrokerAuthority(t)
	oldCA, nextCA := newOperationCA(t, 1), newOperationCA(t, 2)
	dir := t.TempDir()
	caFile, serverCert, serverKey := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "server.pem"), filepath.Join(dir, "server.key")
	clientCert, clientKey := filepath.Join(dir, "client.pem"), filepath.Join(dir, "client.key")
	oldServerPEM, oldServerKey := oldCA.leaf(t, 11)
	oldClientPEM, oldClientKey := oldCA.leaf(t, 12)
	for path, data := range map[string][]byte{caFile: oldCA.pem, serverCert: oldServerPEM, serverKey: oldServerKey, clientCert: oldClientPEM, clientKey: oldClientKey} {
		replaceOperationFile(t, path, data)
	}
	resolver := a.resolver(t)
	// Reload uses an actual server configuration file, as a SIGHUP does.
	config := filepath.Join(dir, "nats.conf")
	require.NoError(t, os.WriteFile(config, []byte(fmt.Sprintf("host: 127.0.0.1\nport: -1\ntls { cert_file: %q, key_file: %q, ca_file: %q, verify: true }\n", serverCert, serverKey, caFile)), 0600))
	opts, err := ns.ProcessConfigFile(config)
	require.NoError(t, err)
	opts.TrustedOperators = []*jwt.OperatorClaims{jwt.NewOperatorClaims(a.operatorPublic)}
	opts.AccountResolver = resolver
	opts.SystemAccount = a.systemPublic
	s := startOperationsBroker(t, opts)
	client := natsq.ClientConfig{URL: brokerURL(s), TLSCAFile: caFile, TLSCertFile: clientCert, TLSKeyFile: clientKey, UserCredentials: brokerUser(t, a.account, a.accountPublic, "publisher")}
	conn, err := client.Connect("certificate-rotation")
	require.NoError(t, err)
	defer conn.Close()
	monitorClient := client
	monitorClient.UserCredentials = brokerUser(t, a.account, a.accountPublic, "")
	monitor, err := monitorClient.Connect("certificate-positive-control")
	require.NoError(t, err)
	defer monitor.Close()
	subject := natsq.JobSubject("11111111-1111-4111-8111-111111111111", "operations")
	subscription, err := monitor.SubscribeSync(subject)
	require.NoError(t, err)
	require.NoError(t, monitor.FlushTimeout(time.Second))
	reconnect := func(serial int64) {
		t.Helper()
		require.NoError(t, conn.ForceReconnect())
		require.Eventually(t, func() bool {
			if !conn.IsConnected() {
				return false
			}
			state, err := conn.TLSConnectionState()
			return err == nil && state.PeerCertificates[0].SerialNumber.Int64() == serial
		}, 5*time.Second, 10*time.Millisecond)
		require.NoError(t, conn.Publish(subject, []byte("certificate-positive-control")))
		require.NoError(t, conn.FlushTimeout(time.Second))
		message, err := subscription.NextMsg(time.Second)
		require.NoError(t, err)
		require.Equal(t, "certificate-positive-control", string(message.Data))
	}
	reload := func() {
		t.Helper()
		next, err := ns.ProcessConfigFile(config)
		require.NoError(t, err)
		next.TrustedOperators = opts.TrustedOperators
		next.AccountResolver = resolver
		next.SystemAccount = a.systemPublic
		require.NoError(t, s.ReloadOptions(next))
	}
	// Add trust before either endpoint changes its certificate.
	replaceOperationFile(t, caFile, append(append([]byte{}, oldCA.pem...), nextCA.pem...))
	reload()
	nextClientPEM, nextClientKey := nextCA.leaf(t, 22)
	replaceOperationFile(t, clientCert, nextClientPEM)
	replaceOperationFile(t, clientKey, nextClientKey)
	reconnect(11)
	nextServerPEM, nextServerKey := nextCA.leaf(t, 21)
	replaceOperationFile(t, serverCert, nextServerPEM)
	replaceOperationFile(t, serverKey, nextServerKey)
	reload()
	reconnect(21)
	// Remove old trust. Successful reconnect proves both client cert and CA
	// callbacks reload their files; an old client is now refused by the broker.
	replaceOperationFile(t, caFile, nextCA.pem)
	reload()
	reconnect(21)
	oldClient := filepath.Join(dir, "old-client.pem")
	oldKey := filepath.Join(dir, "old-client.key")
	replaceOperationFile(t, oldClient, oldClientPEM)
	replaceOperationFile(t, oldKey, oldClientKey)
	rejected, err := nats.Connect(client.URL, nats.RootCAs(caFile), nats.ClientCert(oldClient, oldKey), nats.UserCredentialBytes([]byte(client.UserCredentials)), nats.NoReconnect(), nats.Timeout(time.Second))
	if rejected != nil {
		rejected.Close()
	}
	require.Error(t, err)
	// The current client must also reject a server under the removed root.
	pair, err := tls.X509KeyPair(oldServerPEM, oldServerKey)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(nextCA.pem))
	oldServer := startOperationsBroker(t, &ns.Options{Host: "127.0.0.1", Port: -1, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}, TrustedOperators: opts.TrustedOperators, AccountResolver: a.resolver(t), SystemAccount: a.systemPublic})
	rejected, err = nats.Connect(brokerURL(oldServer), nats.RootCAs(caFile), nats.ClientCert(clientCert, clientKey), nats.UserCredentialBytes([]byte(client.UserCredentials)), nats.NoReconnect(), nats.Timeout(time.Second))
	if rejected != nil {
		rejected.Close()
	}
	require.Error(t, err)
	t.Log("CA overlap, client replacement, server reload, old-root retirement, old client denial and old server denial pass")
}

func TestBrokerSecuredJetStreamFailover(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a := newBrokerAuthority(t)
	tlsConfig, ca, cert, key := brokerTLS(t)
	routeTLS := tlsConfig.Clone()
	routeTLS.RootCAs = routeTLS.ClientCAs
	routeTLS.ServerName = "localhost"
	var servers []*ns.Server
	routes := clusterRoutes(t, 3)
	for i := range 3 {
		address, err := net.ResolveTCPAddr("tcp", routes[i].Host)
		require.NoError(t, err)
		peers := append(append([]*url.URL{}, routes[:i]...), routes[i+1:]...)
		opts := &ns.Options{ServerName: fmt.Sprintf("m9-broker-%d", i), Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), TLSConfig: tlsConfig.Clone(), TrustedOperators: []*jwt.OperatorClaims{jwt.NewOperatorClaims(a.operatorPublic)}, AccountResolver: a.resolver(t), SystemAccount: a.systemPublic, Cluster: ns.ClusterOpts{Name: "m9-secured", Host: "127.0.0.1", Port: address.Port, TLSConfig: routeTLS.Clone(), PoolSize: 1}, Routes: peers}
		s := startOperationsBroker(t, opts)
		servers = append(servers, s)
	}
	require.Eventually(t, func() bool {
		for _, s := range servers {
			if s.NumRoutes() < 2 {
				return false
			}
		}
		for _, s := range servers {
			if s.JetStreamIsLeader() && len(s.JetStreamClusterPeers()) == 3 {
				return true
			}
		}
		return false
	}, 10*time.Second, 25*time.Millisecond)
	loadAccountEverywhere(t, a.accountPublic, servers...)
	client := natsq.ClientConfig{URL: brokerURL(servers[0]), TLSCAFile: ca, TLSCertFile: cert, TLSKeyFile: key}
	transport, cfg := a.scheduler(t, ctx, client, a.account, 3)
	worker, info := issuedBrokerClient(t, ctx, transport, client)
	js, err := jetstream.New(worker)
	require.NoError(t, err)
	_, err = js.Consumer(ctx, info.Stream, info.Consumer)
	require.NoError(t, err)
	publisher, err := cfg.PublisherConfig().Connect("failover-publisher")
	require.NoError(t, err)
	defer publisher.Close()
	pubJS, err := jetstream.New(publisher)
	require.NoError(t, err)
	_, err = pubJS.Publish(ctx, info.Subject, []byte("durable-before-failure"), jetstream.WithMsgID("m9-before-failure"))
	require.NoError(t, err)
	// Stop the stream leader, and force the client to exercise discovery too
	// when its connected broker differs from that leader.
	provisioner, err := cfg.Connect("failover-inspector")
	require.NoError(t, err)
	defer provisioner.Close()
	adminJS, err := jetstream.New(provisioner)
	require.NoError(t, err)
	stream, err := adminJS.Stream(ctx, info.Stream)
	require.NoError(t, err)
	state, err := stream.Info(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Cluster)
	require.Len(t, state.Cluster.Replicas, 2)
	var leader *ns.Server
	for _, s := range servers {
		if s.Name() == state.Cluster.Leader {
			leader = s
		}
	}
	require.NotNil(t, leader)
	// Dial only the node which is about to fail. Replacement endpoints must
	// come from authenticated broker discovery, rather than a supplied list.
	worker.Close()
	client.URL = brokerURL(leader)
	worker, info = issuedBrokerClient(t, ctx, transport, client)
	js, err = jetstream.New(worker)
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, info.Stream, info.Consumer)
	require.NoError(t, err)
	leader.Shutdown()
	leader.WaitForShutdown()
	require.Eventually(t, worker.IsConnected, 10*time.Second, 25*time.Millisecond)
	// Retry on transient election errors with a stable message ID. The durable
	// positive control verifies pre-failure data, replacement writes and acks.
	require.Eventually(t, func() bool {
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		_, err := pubJS.Publish(attempt, info.Subject, []byte("after-failure"), jetstream.WithMsgID("m9-after-failure"))
		return err == nil
	}, 15*time.Second, 100*time.Millisecond)
	// The consumer elects separately from the stream, and usually lost its
	// leader too: a pull before that election finishes has no responder.
	require.Eventually(t, func() bool {
		attempt, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		state, err := consumer.Info(attempt)
		return err == nil && state.Cluster != nil && state.Cluster.Leader != "" && state.Cluster.Leader != leader.Name()
	}, 15*time.Second, 100*time.Millisecond)
	batch, err := consumer.Fetch(2, jetstream.FetchMaxWait(5*time.Second))
	require.NoError(t, err)
	var received []string
	for msg := range batch.Messages() {
		received = append(received, string(msg.Data()))
		require.NoError(t, msg.DoubleAck(ctx))
	}
	require.NoError(t, batch.Error())
	require.Equal(t, []string{"durable-before-failure", "after-failure"}, received)
	t.Logf("stream leader %s stopped; secured client discovery, replacement publish, both durable deliveries and confirmed acks pass", leader.Name())
}
