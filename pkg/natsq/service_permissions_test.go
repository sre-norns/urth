package natsq_test

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	ns "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/stretchr/testify/require"
)

func TestBrokerServiceRolePermissions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a := newBrokerAuthority(t)
	tlsConfig, ca, cert, key := brokerTLS(t)
	broker := startOperationsBroker(t, &ns.Options{
		TLSConfig: tlsConfig, Host: "127.0.0.1", Port: -1, JetStream: true,
		StoreDir: t.TempDir(), TrustedOperators: []*jwt.OperatorClaims{jwt.NewOperatorClaims(a.operatorPublic)},
		AccountResolver: a.resolver(t), SystemAccount: a.systemPublic,
	})
	client := natsq.ClientConfig{URL: brokerURL(broker), TLSCAFile: ca, TLSCertFile: cert, TLSKeyFile: key}
	transport, _ := a.scheduler(t, ctx, client, a.account, 1)
	worker, info := issuedBrokerClient(t, ctx, transport, client)
	logSubject := natsq.LogSubject("runner-operations", "result-operations")
	presenceSubject := natsq.PresenceSubject("runner-operations", "worker-operations")

	for _, role := range []string{"provisioner", "publisher", "observer"} {
		t.Run(role, func(t *testing.T) {
			denied := make(chan error, 32)
			conn, err := nats.Connect(client.URL, nats.RootCAs(ca), nats.ClientCert(cert, key),
				nats.UserCredentialBytes([]byte(brokerUser(t, a.account, a.accountPublic, role))),
				nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) { denied <- err }))
			require.NoError(t, err)
			defer conn.Close()
			js, err := jetstream.New(conn)
			require.NoError(t, err)

			// Exercise each role's intended work before testing its boundary.
			switch role {
			case "provisioner":
				stream, err := js.Stream(ctx, info.Stream)
				require.NoError(t, err)
				_, err = js.UpdateStream(ctx, stream.CachedInfo().Config)
				require.NoError(t, err)
				consumer, err := js.CreateOrUpdateConsumer(ctx, info.Stream, jetstream.ConsumerConfig{
					Durable: "role-check", AckPolicy: jetstream.AckExplicitPolicy, FilterSubject: natsq.JobSubject("11111111-1111-4111-8111-111111111111", "permission-test"),
				})
				require.NoError(t, err)
				require.NoError(t, js.DeleteConsumer(ctx, info.Stream, consumer.CachedInfo().Name))
			case "publisher":
				_, err := js.Publish(ctx, info.Subject, []byte("authorized-job"))
				require.NoError(t, err)
				workerJS, err := jetstream.New(worker)
				require.NoError(t, err)
				consumer, err := workerJS.Consumer(ctx, info.Stream, info.Consumer)
				require.NoError(t, err)
				msg, err := consumer.Next(jetstream.FetchMaxWait(time.Second))
				require.NoError(t, err)
				require.Equal(t, "authorized-job", string(msg.Data()))
				require.NoError(t, msg.DoubleAck(ctx))
			case "observer":
				for _, subject := range []string{logSubject, presenceSubject} {
					sub, err := conn.SubscribeSync(subject)
					require.NoError(t, err)
					require.NoError(t, conn.Flush())
					require.NoError(t, worker.Publish(subject, []byte("authorized-observation")))
					msg, err := sub.NextMsg(time.Second)
					require.NoError(t, err)
					require.Equal(t, "authorized-observation", string(msg.Data))
					require.NoError(t, sub.Unsubscribe())
				}
			}
			require.NoError(t, conn.Flush())
			select {
			case err := <-denied:
				t.Fatalf("permitted role operation denied: %v", err)
			default:
			}

			assertDenied := func(subject string, subscribe bool) {
				t.Helper()
				if subscribe {
					sub, err := conn.SubscribeSync(subject)
					require.NoError(t, err)
					defer sub.Unsubscribe() // the server rejects it asynchronously
				} else {
					require.NoError(t, conn.Publish(subject, []byte(`{}`)))
				}
				require.NoError(t, conn.Flush())
				select {
				case err := <-denied:
					require.ErrorIs(t, err, nats.ErrPermissionViolation)
					require.Contains(t, err.Error(), subject)
				case <-time.After(time.Second):
					t.Fatalf("%s accepted forbidden operation on %s", role, subject)
				}
			}
			for _, subject := range []string{info.Subject, "urth.v1.events.>"} {
				assertDenied(subject, true)
			}
			if role != "observer" {
				assertDenied(logSubject, true)
				assertDenied(presenceSubject, true)
			} else {
				assertDenied("_INBOX.foreign.>", true)
			}
			for _, subject := range []string{logSubject, presenceSubject, "$SYS.REQ.CLAIMS.UPDATE"} {
				assertDenied(subject, false)
			}
			if role != "publisher" {
				assertDenied(info.Subject, false)
			}
			// A provisioner may manage Urth's jobs assets, but may neither
			// delete the jobs stream nor manage an unrelated stream.
			assertDenied("$JS.API.STREAM.DELETE."+info.Stream, false)
			assertDenied("$JS.API.STREAM.CREATE.UNRELATED", false)
			if role != "provisioner" {
				for _, subject := range []string{
					"$JS.API.STREAM.CREATE." + info.Stream,
					"$JS.API.STREAM.UPDATE." + info.Stream,
					"$JS.API.CONSUMER.CREATE." + info.Stream + "." + info.Consumer,
					"$JS.API.CONSUMER.DELETE." + info.Stream + "." + info.Consumer,
				} {
					assertDenied(subject, false)
				}
			}
			assertDenied("$JS.API.CONSUMER.MSG.NEXT."+info.Stream+"."+info.Consumer, false)
		})
	}
}
