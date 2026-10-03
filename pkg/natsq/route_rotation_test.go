package natsq_test

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	ns "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/stretchr/testify/require"
)

// routeHandshake reads the route INFO only after mutual TLS succeeds. Reading
// matters with TLS 1.3: the client handshake can finish before the peer refuses
// the client's certificate. It does not register a synthetic NATS route.
func routeHandshake(address string, config *tls.Config) (int64, error) {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, config)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		return 0, err
	}
	info, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(info, "INFO ") {
		return 0, fmt.Errorf("route did not return INFO")
	}
	return conn.ConnectionState().PeerCertificates[0].SerialNumber.Int64(), nil
}

func TestBrokerClusterRouteCertificateRotation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	a := newBrokerAuthority(t)
	oldCA, nextCA := newOperationCA(t, 10), newOperationCA(t, 20)
	// Client-listener certificates use an independent CA. This test changes
	// only the cluster-route certificates and trust, without restarting brokers.
	_, clientCA, clientCert, clientKey := brokerTLS(t)
	type node struct {
		server                          *ns.Server
		config, trust, cert, key, store string
		address                         *url.URL
		resolver                        *ns.DirAccResolver
		compression                     string
		serial                          int64
	}
	nodes := make([]node, 3)
	for i := range nodes {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		address, err := url.Parse("nats-route://" + listener.Addr().String())
		require.NoError(t, err)
		require.NoError(t, listener.Close())
		dir := t.TempDir()
		nodes[i] = node{config: filepath.Join(dir, "nats.conf"), trust: filepath.Join(dir, "route-ca.pem"), cert: filepath.Join(dir, "route.pem"), key: filepath.Join(dir, "route.key"), store: filepath.Join(dir, "store"), address: address, resolver: a.resolver(t), compression: "off", serial: int64(100 + i)}
		cert, key := oldCA.leaf(t, nodes[i].serial)
		for path, data := range map[string][]byte{nodes[i].trust: oldCA.pem, nodes[i].cert: cert, nodes[i].key: key} {
			replaceOperationFile(t, path, data)
		}
	}
	options := func(i int) *ns.Options {
		t.Helper()
		n := &nodes[i]
		var peers []string
		for j := range nodes {
			if i != j {
				peers = append(peers, fmt.Sprintf("%q", nodes[j].address.String()))
			}
		}
		config := fmt.Sprintf(`server_name: m9-route-%d
host: 127.0.0.1
port: -1
jetstream { store_dir: %q }
tls { cert_file: %q, key_file: %q, ca_file: %q, verify: true }
cluster {
  name: m9-route-rotation
  host: 127.0.0.1
  port: %s
  pool_size: 1
  compression: %s
  routes: [%s]
  tls { cert_file: %q, key_file: %q, ca_file: %q, verify: true }
}
`, i, n.store, clientCert, clientKey, clientCA, n.address.Port(), n.compression, strings.Join(peers, ","), n.cert, n.key, n.trust)
		require.NoError(t, os.WriteFile(n.config, []byte(config), 0600))
		opts, err := ns.ProcessConfigFile(n.config)
		require.NoError(t, err)
		opts.TrustedOperators = []*jwt.OperatorClaims{jwt.NewOperatorClaims(a.operatorPublic)}
		opts.AccountResolver = n.resolver
		opts.SystemAccount = a.systemPublic
		require.False(t, opts.Cluster.TLSConfig.InsecureSkipVerify)
		require.Equal(t, tls.RequireAndVerifyClientCert, opts.Cluster.TLSConfig.ClientAuth)
		return opts
	}
	for i := range nodes {
		nodes[i].server = startOperationsBroker(t, options(i))
	}
	var refreshCutoff time.Time
	var previousRoutes []map[uint64]bool
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		for i := range nodes {
			s := nodes[i].server
			t.Logf("%s leader=%v peers=%d expected-compression=%s", s.Name(), s.JetStreamIsLeader(), len(s.JetStreamClusterPeers()), nodes[i].compression)
			routes, err := s.Routez(nil)
			if err != nil {
				t.Logf("route inspection failed: %v", err)
				continue
			}
			for _, route := range routes.Routes {
				oldID := len(previousRoutes) > i && previousRoutes[i][route.Rid]
				t.Logf("peer=%s system=%v rid=%d prior-id=%v start=%s cutoff=%s compression=%s", route.RemoteName, route.Account == a.systemPublic, route.Rid, oldID, route.Start.Format(time.RFC3339Nano), refreshCutoff.Format(time.RFC3339Nano), route.Compression)
			}
		}
	})
	routesReady := func(after time.Time, compression string) bool {
		for i := range nodes {
			s := nodes[i].server
			routes, err := s.Routez(nil)
			if err != nil || routes.NumRoutes < 2 {
				return false
			}
			peers := make(map[string]bool)
			pooled, system := make(map[string]bool), make(map[string]bool)
			for _, route := range routes.Routes {
				if !route.Start.After(after) || route.Compression != compression {
					return false
				}
				peers[route.RemoteID] = true
				if route.Account == "" {
					pooled[route.RemoteID] = true
				} else if route.Account == a.systemPublic {
					system[route.RemoteID] = true
				}
			}
			if len(peers) != 2 || len(pooled) != 2 || len(system) != 2 {
				return false
			}
		}
		return true
	}
	metadataCurrent := func() bool {
		leader := false
		for i := range nodes {
			if !nodes[i].server.JetStreamIsCurrent() {
				return false
			}
			leader = leader || nodes[i].server.JetStreamIsLeader()
		}
		return leader
	}
	clusterReady := func(after time.Time, compression string) bool {
		if !routesReady(after, compression) || !metadataCurrent() {
			return false
		}
		for i := range nodes {
			s := nodes[i].server
			if s.JetStreamIsLeader() && len(s.JetStreamClusterPeers()) == 3 {
				return true
			}
		}
		return false
	}
	require.Eventually(t, func() bool { return clusterReady(time.Time{}, "off") }, 10*time.Second, 25*time.Millisecond)
	client := natsq.ClientConfig{URL: brokerURL(nodes[0].server), TLSCAFile: clientCA, TLSCertFile: clientCert, TLSKeyFile: clientKey}
	transport, cfg := a.scheduler(t, ctx, client, a.account, 3)
	client.URL = brokerURL(nodes[2].server)
	// Allow the fixture's valid route/stats convergence waits to finish before
	// this credential expires. The production five-minute cap remains intact.
	worker, info := issuedBrokerClientUntil(t, ctx, transport, client, time.Now().Add(4*time.Minute))
	workerJS, err := jetstream.New(worker)
	require.NoError(t, err)
	consumer, err := workerJS.Consumer(ctx, info.Stream, info.Consumer)
	require.NoError(t, err)
	publisher, err := cfg.PublisherConfig().Connect("route-rotation-publisher")
	require.NoError(t, err)
	defer publisher.Close()
	pubJS, err := jetstream.New(publisher)
	require.NoError(t, err)
	inspector, err := cfg.Connect("route-rotation-inspector")
	require.NoError(t, err)
	defer inspector.Close()
	adminJS, err := jetstream.New(inspector)
	require.NoError(t, err)
	stream, err := adminJS.Stream(ctx, info.Stream)
	require.NoError(t, err)
	delivery := func(stage string) {
		t.Helper()
		require.Equal(t, nodes[0].server.ID(), publisher.ConnectedServerId())
		require.Equal(t, nodes[2].server.ID(), worker.ConnectedServerId())
		require.Eventually(t, func() bool {
			attempt, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			state, err := stream.Info(attempt)
			if err != nil || state.Cluster == nil || len(state.Cluster.Replicas) != 2 {
				return false
			}
			for _, replica := range state.Cluster.Replicas {
				if !replica.Current || replica.Offline {
					return false
				}
			}
			return true
		}, 5*time.Second, 25*time.Millisecond)
		_, err := pubJS.Publish(ctx, info.Subject, []byte(stage), jetstream.WithMsgID(stage))
		require.NoError(t, err)
		batch, err := consumer.Fetch(1, jetstream.FetchMaxWait(time.Second))
		require.NoError(t, err)
		count := 0
		for msg := range batch.Messages() {
			count++
			require.Equal(t, stage, string(msg.Data()))
			require.NoError(t, msg.DoubleAck(ctx))
		}
		require.NoError(t, batch.Error())
		require.Equal(t, 1, count)
	}
	probeConfig := func(ca []byte, cert, key []byte) *tls.Config {
		t.Helper()
		pool := x509.NewCertPool()
		require.True(t, pool.AppendCertsFromPEM(ca))
		pair, err := tls.X509KeyPair(cert, key)
		require.NoError(t, err)
		return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool, Certificates: []tls.Certificate{pair}, ServerName: "localhost"}
	}
	oldProbeCert, oldProbeKey := oldCA.leaf(t, 300)
	nextProbeCert, nextProbeKey := nextCA.leaf(t, 400)
	overlap := append(append([]byte{}, oldCA.pem...), nextCA.pem...)
	oldProbe := probeConfig(overlap, oldProbeCert, oldProbeKey)
	nextProbe := probeConfig(overlap, nextProbeCert, nextProbeKey)
	checkLeaves := func(probes ...*tls.Config) {
		t.Helper()
		for i := range nodes {
			for _, probe := range probes {
				serial, err := routeHandshake(nodes[i].address.Host, probe)
				require.NoError(t, err)
				require.Equal(t, nodes[i].serial, serial)
			}
		}
	}
	delivery("before-route-rotation")
	for i := range nodes {
		replaceOperationFile(t, nodes[i].trust, overlap)
		require.NoError(t, nodes[i].server.ReloadOptions(options(i)))
	}
	checkLeaves(oldProbe, nextProbe)
	delivery("route-ca-overlap")
	// A TLS reload does not reauthenticate established routes. The public
	// compression reload closes both pooled and dedicated system routes. Use
	// it only as a test handshake trigger, without a server or client restart.
	refreshSequence := 0
	refresh := func() {
		t.Helper()
		refreshSequence++
		// Reset accept to off first. Both configurations negotiate off on the
		// wire, so this phase does not close routes. Reloading off to accept
		// then forces negotiation and closes every route, without enabling a
		// compressor or requiring different wire modes on different peers.
		for i := range nodes {
			nodes[i].compression = "off"
			require.NoError(t, nodes[i].server.ReloadOptions(options(i)))
		}
		previous := make([]map[uint64]bool, len(nodes))
		for i := range nodes {
			previous[i] = make(map[uint64]bool)
			routes, err := nodes[i].server.Routez(nil)
			require.NoError(t, err)
			for _, route := range routes.Routes {
				previous[i][route.Rid] = true
			}
		}
		cutoff := time.Now()
		refreshCutoff, previousRoutes = cutoff, previous
		for i := range nodes {
			nodes[i].compression = "accept"
			require.NoError(t, nodes[i].server.ReloadOptions(options(i)))
			// Changing all three at once also drops every metadata Raft
			// route. Preserve quorum by waiting for route and leader convergence
			// after each node before changing the next node.
			require.Eventually(t, func() bool {
				if !routesReady(time.Time{}, "off") || !metadataCurrent() {
					return false
				}
				routes, err := nodes[i].server.Routez(nil)
				if err != nil {
					return false
				}
				for _, route := range routes.Routes {
					if previous[i][route.Rid] || !route.Start.After(cutoff) {
						return false
					}
				}
				return true
			}, 10*time.Second, 25*time.Millisecond, "rolling route refresh must restore quorum before the next node changes")
			delivery(fmt.Sprintf("route-refresh-%d-node-%d", refreshSequence, i))
		}
		// JetStreamClusterPeers also requires current server stats. NATS sends
		// their heartbeat at up to ten-second intervals, so the final peer
		// inventory deadline must allow a heartbeat after routes reconnect.
		require.Eventually(t, func() bool {
			if !clusterReady(cutoff, "off") {
				return false
			}
			for i := range nodes {
				routes, err := nodes[i].server.Routez(nil)
				if err != nil {
					return false
				}
				for _, route := range routes.Routes {
					if previous[i][route.Rid] {
						return false
					}
				}
			}
			return true
		}, 20*time.Second, 25*time.Millisecond, "every pooled and system route must have a new ID and start time, with three current peers")
	}
	for i := range nodes {
		nodes[i].serial = int64(200 + i)
		cert, key := nextCA.leaf(t, nodes[i].serial)
		replaceOperationFile(t, nodes[i].cert, cert)
		replaceOperationFile(t, nodes[i].key, key)
		require.NoError(t, nodes[i].server.ReloadOptions(options(i)))
		checkLeaves(oldProbe, nextProbe)
		refresh()
		delivery(fmt.Sprintf("route-leaf-%d-reloaded", i))
	}
	for i := range nodes {
		replaceOperationFile(t, nodes[i].trust, nextCA.pem)
		require.NoError(t, nodes[i].server.ReloadOptions(options(i)))
	}
	refresh()
	nextOnlyProbe := probeConfig(nextCA.pem, nextProbeCert, nextProbeKey)
	checkLeaves(nextOnlyProbe)
	for i := range nodes {
		_, err := routeHandshake(nodes[i].address.Host, oldProbe)
		require.Error(t, err, "old route client must fail after root retirement")
	}
	// Also exercise the outgoing route TLS configuration against an old-root
	// listener. A current client certificate is accepted there, so failure must
	// be verification of the old server certificate, not mutual-auth rejection.
	oldServerCert, oldServerKey := oldCA.leaf(t, 500)
	pair, err := tls.X509KeyPair(oldServerCert, oldServerKey)
	require.NoError(t, err)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(nextCA.pem))
	oldServer := startOperationsBroker(t, &ns.Options{Host: "127.0.0.1", Port: -1, Cluster: ns.ClusterOpts{Name: "m9-old-route", Host: "127.0.0.1", Port: -1, PoolSize: 1, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientCAs: pool, RootCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}}})
	serial, err := routeHandshake(oldServer.ClusterAddr().String(), nextProbe)
	require.NoError(t, err, "overlap trust and a current client must accept the old route server")
	require.Equal(t, int64(500), serial)
	for i := range nodes {
		outgoing := options(i).Cluster.TLSConfig.Clone()
		outgoing.ServerName = "localhost"
		_, err := routeHandshake(oldServer.ClusterAddr().String(), outgoing)
		var verification *tls.CertificateVerificationError
		require.ErrorAs(t, err, &verification, "outgoing route must refuse an old-root server")
		var unknownAuthority x509.UnknownAuthorityError
		require.ErrorAs(t, verification.Err, &unknownAuthority)
	}
	delivery("retired-route-root-positive-control")
	t.Log("three route leaves reload in sequence; old/new CA overlap, fresh pooled/system routes, current replicas, cross-node delivery and confirmed acks pass; retired-root incoming and outgoing TLS are refused")
}
