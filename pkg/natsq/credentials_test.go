package natsq_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	broker, err := ns.NewServer(&ns.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), TrustedKeys: []string{opPub}, AccountResolver: resolver, SystemAccount: systemPub})
	require.NoError(t, err)

	go broker.Start()
	require.True(t, broker.ReadyForConnections(5*time.Second))
	t.Cleanup(broker.Shutdown)
	admin, err := nkeys.CreateUser()
	require.NoError(t, err)
	adminPub, err := admin.PublicKey()
	require.NoError(t, err)
	adminJWT, err := jwt.NewUserClaims(adminPub).Encode(account)
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
	cfg.URL = broker.ClientURL()
	cfg.UserCredentials = string(creds)
	cfg.WorkerAccountSeedFile = seedFile
	cfg.AllowInsecureWorkers = false
	runners := map[manifest.ResourceID]urth.Runner{
		"a": {ObjectMeta: manifest.ObjectMeta{UID: "a", Account: "11111111-1111-4111-8111-111111111111", Name: "same.name"}},
		"b": {ObjectMeta: manifest.ObjectMeta{UID: "b", Account: "22222222-2222-4222-8222-222222222222", Name: "same.name"}},
	}
	transport, err := natsq.NewScheduler(ctx, cfg, func(_ context.Context, id manifest.ResourceID) (urth.Runner, error) { return runners[id], nil })
	require.NoError(t, err)
	defer transport.Close()
	a, err := transport.ConnectionInfoFor(ctx, "a")
	require.NoError(t, err)
	b, err := transport.ConnectionInfoFor(ctx, "b")
	require.NoError(t, err)
	require.NotEqual(t, a.Subject, b.Subject)
	require.NotEqual(t, a.Consumer, b.Consumer)
	// Exercise the same dynamic credential callback used by renewing workers.
	rotating, err := (natsq.ClientConfig{URL: broker.ClientURL(), InboxPrefix: a.InboxPrefix, CredentialSource: func() string { return a.Credential.Value }}).Connect("renewable-worker")
	require.NoError(t, err)
	rotatingJS, err := jetstream.New(rotating)
	require.NoError(t, err)
	_, err = rotatingJS.Consumer(ctx, a.Stream, a.Consumer)
	require.NoError(t, err)
	rotating.Close()
	denied := make(chan error, 8)
	conn, err := nats.Connect(broker.ClientURL(), nats.UserCredentialBytes([]byte(a.Credential.Value)), nats.CustomInboxPrefix(a.InboxPrefix), nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) { denied <- err }))
	require.NoError(t, err)
	defer conn.Close()
	js, err := jetstream.New(conn)
	require.NoError(t, err)
	consumer, err := js.Consumer(ctx, a.Stream, a.Consumer)
	require.NoError(t, err)
	adminConn, err := cfg.Connect("test-publisher")
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
