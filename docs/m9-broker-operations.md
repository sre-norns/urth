# M9 broker rotation and failover

This runbook covers NATS account signing keys, TLS certificates and JetStream
failover. It records broker-level evidence. The maintainer must validate the
complete release candidate with the API, Worker and PostgreSQL before release.
Task 004 remains open until its other acceptance criteria pass.

## Deployment requirements

Use a new NATS store for a fresh installation. Run three JetStream servers with
separate persistent volumes and failure domains. Set `--nats.replicas=3` on the
API. Require TLS and peer certificate verification on clients and cluster routes.
Protect the broker monitoring interface and system-account credentials separately.

Use distinct provisioner, publisher and observer user identities. Configure the
permissions returned by `natsq.ServicePermissions` for each role. These service
JWTs and the Worker JWT signer must belong to the same NATS account. An Urth
resource account UID is not a NATS account public key.

Configure a delegated account signing seed on every API replica:

```text
--nats.worker-account-public-key=<owning NATS account public key>
--nats.worker-account-seed-file=<private delegated signing seed file>
```

The owning account key starts with `A`. The broker's operator-signed account JWT
must authorize the delegated key in `signing_keys`. The API sets `issuer_account`
to the owning account and signs each Worker JWT with the configured seed. Store
the operator and primary account private keys outside the API process.

The public-key option is optional when the seed is the primary account key.
Existing primary-key configurations keep their current behavior. A delegated
seed needs the public-key option. The broker rejects a delegated credential
without its owning-account claim.

Keep seed, credential and client private-key files private. Urth requires regular
private files without group or other access. Do not put seeds, user JWTs or
private keys in resource manifests, evidence records, command output or logs.

## Rotate the delegated Worker signing key

1. Generate the replacement account signing key outside the API. Retain the
   owning account public key. Add the replacement public signing key to the
   operator-signed account JWT. Keep the old signing key during the overlap.
2. Publish the account JWT with the broker resolver's account-update tool. Verify
   that every broker accepts it. Protect the system-account credential that
   authorizes this update. Do not grant this authority to Urth service roles.
3. Write the replacement signing seed to a private staging file. Atomically
   replace the configured seed file on every API replica. The API reads this
   file for each credential issue; an API restart is not required for the seed
   change. Set the owning-account option before the first delegated issuance.
   Changing that option requires an API configuration restart.
4. Enroll or renew a Worker. Check that it reconnects and consumes its existing
   Runner consumer. Confirm a new dispatch and its acknowledgment. Keep the
   Worker installation identity and Runner identity unchanged.
5. Wait until all API replicas use the replacement seed and Workers renew.
   The configured broker lifetime is at most five minutes and never exceeds
   the Worker session expiry. Allow for the deployment's clock tolerance.
6. Remove the old public signing key from the account JWT. Publish the newer
   signed JWT to every resolver. Verify that old connections close and old
   credentials cannot reconnect. Check that renewed Workers still consume.

Retiring a signing key can affect every user JWT that it signs. Keep Worker and
service signing-key inventories separate. The tests use the primary account key
for service identities and delegated keys for Workers. A compromised signer
requires prompt retirement; the planned overlap is a routine rotation procedure.

Do not restore the old seed after its public key is retired. Restore its account
authorization first only when that rollback is explicitly approved and safe.

## Rotate certificates and their CA

1. Issue replacement server and client certificates. Include the broker hostnames
   and addresses in each server certificate. Keep private keys private.
2. Add the replacement root to the trust bundle on all clients and brokers. Keep
   the old root during the overlap. Reload the broker TLS configuration and
   verify its client and route trust settings.
3. Replace client certificates and keys as complete pairs. Use a versioned
   directory and an atomic parent-directory link change when paths stay fixed.
   This prevents a reconnect from reading a certificate and key from different
   generations. Urth's NATS client reads certificate and CA files on reconnect.
4. Replace server certificates and keys as complete pairs. Reload each broker
   one at a time with the broker's reload command. Verify the presented server
   certificate and the cluster quorum after each change.
5. Verify Worker renewal, consumer access and a new dispatch. Observe TLS and
   authentication failures during the overlap. A broker reload alone does not
   force all established clients to reauthenticate.
6. Remove the old root after all client, server and route certificates move to
   the replacement root. Reload each broker and reconnect clients. Verify that
   the current certificates work and both old clients and old servers fail.

The client/server certificate test covers verified leaf certificates, trust
overlap and old-root rejection. The cluster-route test covers a separate route
CA, trust overlap, rolling route leaf reload and old-root retirement on three
JetStream nodes. It verifies current replicas, cross-node delivery and confirmed
acknowledgments after each transition.

A route TLS reload does not reauthenticate established routes. The test uses a
public route-compression configuration reload to force fresh handshakes. It
resets the configuration to `off`, then reloads each node to `accept`. Route
compression stays `off` on the wire. It checks that pooled routes and dedicated
system-account routes to both peers have new IDs and start times. This is a test
mechanism, not a recommendation to change production compression settings.
It waits for route and JetStream leader convergence after each node reload.
Validate the deployment's planned route reconnection procedure before release.
The generated test CA does not validate a production certificate authority.

## Exercise one broker failure

1. Verify that all three JetStream peers are current. Verify three replicas for
   the jobs stream and its Runner consumer. Record the stream leader.
2. Publish a dispatch with a stable message ID. Confirm the broker acknowledgment
   before stopping a node. Do not remove its store or alter resource state.
3. Stop the stream leader. Use a task-owned process or container in validation.
   Keep the remaining two nodes and volumes intact.
4. Verify client reconnection and a replacement stream leader. The API client
   reconnects indefinitely. Urth retries dispatch through the transactional
   outbox; use the same message ID when a publish response is uncertain.
5. Confirm that the earlier dispatch and a new dispatch both arrive. Confirm
   their broker acknowledgments. Verify API claim and run reporting separately.
6. Restore the stopped node with its existing store. Wait until its replicas are
   current before stopping another node. Do not test a second failure without
   restored quorum.

A three-node cluster needs two nodes for quorum. This test covers one node
failure. It does not claim recovery from two failures, storage loss, a network
partition, or a complete site failure. Core NATS live logs remain best effort;
stored run logs and reporting require separate API checks.

## Reproduce the broker evidence

Use the Go version in `go.mod` and published dependencies. Run from the repository:

```sh
GOWORK=off go test -race -count=10 \
  -run '^TestBroker(AccountSigningKeyRotation|CertificateRotation|SecuredJetStreamFailover)$' \
  -v ./pkg/natsq
GOWORK=off go test -race -count=10 \
  -run '^TestBrokerClusterRouteCertificateRotation$' -v ./pkg/natsq
GOWORK=off go test -race -count=3 \
  -run '^Test(BrokerServiceRolePermissions|WorkerCredentialsEnforceAccountQueueIsolation)$' \
  -v ./pkg/natsq
GOWORK=off go test -race -count=1 -v ./pkg/natsq
GOWORK=off go vet ./pkg/natsq
GOWORK=off go mod verify
```

The tests run real NATS 2.15.0 servers inside the test process. They use ephemeral
loopback listeners, temporary stores, generated operator/account keys and private
TLS files. They need no external broker, database or container. Cleanup stops
only the test's servers and removes its private temporary files.

| Test | Positive control | Required denial or fault |
| --- | --- | --- |
| `TestBrokerAccountSigningKeyRotation` | Overlapping delegated keys; atomic seed replacement; live credential callback reconnect; delivery and confirmed ack with replacement authority | Signed resolver key removal disconnects old authority and refuses its new connection |
| `TestBrokerCertificateRotation` | Trust overlap; replacement client leaf; reloaded server leaf; reconnect and delivered publication at each stage | Removed root rejects an old client and an old server |
| `TestBrokerClusterRouteCertificateRotation` | Separate client and route CAs; old/new route trust overlap; three route leaves reloaded in sequence without server restart; fresh pooled and system routes; current replicas; publication and Worker delivery on different nodes; confirmed acks | Each route listener refuses an old-root client after retirement; each outgoing route TLS configuration refuses an old-root listener with an explicit certificate verification error; overlap trust accepts that same listener with a current client certificate |
| `TestBrokerSecuredJetStreamFailover` | Three-node account-authenticated cluster with mutual TLS on clients and routes; replica count; single-endpoint client discovery; pre-failure and post-failure messages; confirmed acks | Stops the connected stream leader; two peers retain durable work and accept new publication |
| `TestBrokerServiceRolePermissions` | Real provisioner stream update and consumer management; publisher delivery and confirmed Worker ack; observer logs and presence | Role credentials cannot consume jobs, cross role boundaries, publish logs or presence, update account claims, delete the jobs stream, or create an unrelated stream |
| `TestWorkerCredentialsEnforceAccountQueueIsolation` | Issued Worker credentials consume their queue and publish their own logs and presence | Denies foreign queues and concrete foreign log subjects, stream create/update/delete and consumer create/delete operations |
| `TestWorkerSigningAccountConfiguration` | Primary-key default and explicit owning account | Rejects malformed keys, user keys and account configuration without a signing seed |

## Local validation record

Validation date: 2026-10-03. Base commit: `6039e96`. Source commit: `09547ce`.
The delegated-signing regression fails before the fix with
`nats: Authorization Violation`. The fix supplies the owning-account claim.

| Check | Result |
| --- | --- |
| Three operation tests, race detector, ten consecutive runs | Pass; 17.880 seconds |
| Complete `pkg/natsq` suite, race detector, one run | Pass; 29.389 seconds |
| `go vet ./pkg/natsq` | Pass |
| Staticcheck 2026.2.1, repository check selection, `./pkg/natsq` | Pass |
| `go mod verify`, `GOWORK=off`, published dependencies | Pass; all modules verified |
| `git diff --check` | Pass |

These checks use Go 1.27.1. The test stores and listeners are removed at cleanup.
The existing development services on ports 4222, 8222 and 5432 remain intact.
This record describes the earlier source commit. The follow-up below adds
cluster-route reload and permission-boundary evidence. Exact merged-head CI and
deployment-specific operations remain release gates. No task or release is
closed by this record.

## Cluster-route and permission follow-up

Validation date: 2026-10-03. Base commit: `d60d902`. The follow-up changes tests
and this runbook. It does not change production code, configuration defaults,
resource formats or dependencies.

The route test reads the broker's route `INFO` after mutual TLS completes.
This prevents a TLS 1.3 client handshake from hiding a later server refusal of
the client certificate. The outgoing denial uses each node's parsed route TLS
configuration against a real isolated route listener. It requires an unknown
certificate-authority error. An overlap-trust positive control first accepts
the same listener with a current client certificate. These probes do not join
an additional broker to the JetStream cluster.

Sensitivity checks temporarily omit the leaf reload and retain the old root.
Both variants must fail. The correct test is restored before validation.

The initial handshake trigger reloads all three nodes without a convergence
check between nodes. A diagnostic run shows fresh route IDs, current route
start times and the expected compression mode on every node, but no JetStream
metadata leader within the ten-second deadline. Initial serialized triggers
also reach a ten-second readiness deadline; their cleanup snapshots show fresh
routes and a three-peer leader. The final fixture uses `off` and `accept`,
waits for routes and current metadata after each node reload, and confirms
current stream replicas, cross-node delivery and acknowledgment before it
changes the next node.

The final three-peer inventory also requires current NATS server statistics.
NATS publishes their heartbeat at intervals of up to ten seconds. The fixture
allows twenty seconds for that inventory after routes reconnect. It retains
the final fresh-route ID, start time, compression, three-peer leader, replica
and delivery requirements. Its context is three minutes and its issued Worker
credential lasts four minutes, within the production five-minute cap. These
test limits prevent credential expiry from masking a valid convergence wait.
The failed and diagnostic logs remain in the review evidence. These findings
concern the test's handshake trigger and readiness timing. They do not establish
a production TLS defect.

| Follow-up check | Result |
| --- | --- |
| Cluster-route rollover, race detector, ten consecutive runs | Pass; 287.249 seconds |
| Complete `pkg/natsq` suite through `make audit/postgres`, race detector | Pass; 64.745 seconds |
| Complete PostgreSQL audit, including API/Worker integration | Pass; integration package 144.609 seconds |
| Repository-wide vet, Staticcheck 2026.2.1 and module verification | Pass |
| Final-source omitted-leaf-reload sensitivity check | Expected failure; listener presents serial 100 instead of 200 |
| Final-source retained-old-root sensitivity check | Expected failure; old route client connects |
| `git diff --check` | Pass |

The checks use Go 1.27.1 with `GOWORK=off`. The PostgreSQL audit uses a dedicated
task database on port 18833. The broker tests use only test-owned loopback
listeners and temporary stores. The existing services on ports 4222, 8222 and
5432 remain intact. Sanitized logs include the earlier fixture failures and
the final passing checks. No private keys, user credentials or test stores are
retained in the review evidence.

The follow-up does not prove production route reconnection, multi-site recovery,
certificate expiry behavior, API resource authorization or deployment CA
operations. It does not claim that an established route authenticates again
when TLS files reload.
