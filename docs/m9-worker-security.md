# Worker identity and broker security

M9 uses the shared machine token to authorize enrollment into one Runner. It does not create a second enrollment-token store. The Worker proves possession of a persistent Ed25519 installation key before the API creates or renews a registration.

## Enrollment protocol

1. The Worker loads `--identity-key-file`. The default is `urth/worker.key` under the operating-system user configuration directory. The file contains a base64url Ed25519 seed. The Worker creates it atomically with private permissions and rejects public, nonregular, or symbolic-link key files.
2. The Worker sends its public key and proposed manifest to `POST /v1/auth/workers/challenge` with the Runner machine token. Authentication runs before manifest parsing. The API limits the request body to 64 KiB.
3. The API stores a random challenge for two minutes. Its signed message contains the enrollment audience, account, Runner UID, challenge ID, canonical manifest digest, and expiry. The digest includes the name, labels, capabilities, requested session duration, and public key. It excludes the challenge and signature. The API rejects supplied resource UID, version, and status.
4. The Worker checks the returned audience, challenge, manifest digest, and expiry. It signs the exact message and sends the proof to `POST /v1/auth/workers`.
5. The API verifies the signature and atomically consumes the challenge. The transaction locks the Runner to serialize admission, concurrent replay, name conflicts, and blocklist changes. One challenge admits at most one request across API replicas.

The database limits outstanding challenges to 128 per Runner. A request that reaches this limit returns HTTP 429. Challenge creation removes expired records. Deleting an account also removes its challenge records. This bounds live challenge storage; there is no background cleanup requirement for correctness.

The API records `status.fingerprint` as `sha256:` followed by the lowercase SHA-256 digest of the verified public key. The display name does not establish identity. The same installation key can renew or rename its registration without changing its UID. A different key cannot take an existing display name. Each independently operated Worker must use a distinct key file or volume. Do not clone a seed into several installations.

## Blocking and deletion

Runner `spec.blockedWorkers` contains fingerprint and optional reason entries. It accepts at most 1024 distinct fingerprints and at most 1024 bytes per reason. Runner writes use the existing version precondition. CLI `urthctl runners block NAME FINGERPRINT --reason=TEXT` and `urthctl runners unblock NAME FINGERPRINT` use this contract. The fleet UI exposes the verified fingerprint and the same blocklist actions.

A committed block denies new proof challenges, enrollment, renewal, heartbeats, dispatch-failure reporting, and the next API run claim for that fingerprint. A production claim takes a Runner share lock before reading authorization state. A blocklist write and a claim therefore have a defined commit order: a claim that follows the block commit cannot use stale blocklist state. Database failures return service unavailable so the Worker can retry the dispatch.

Deleting a Worker registration revokes its old session. It does not permanently ban the installation. A valid machine token and the same installation key can create a new registration UID. The running Worker rebuilds its broker connection, inbox, consumer binding, and presence publisher when renewal returns that new UID. It retains the report capabilities for runs that it already claimed.

Blocking, deleting a Worker, or ending its session does not revoke an already issued run capability. ADR 0002 bounds that capability by its Result, executor, scopes, deadline, and artifact-reporting grace. New work requires an active session. Machine-token revocation prevents enrollment and renewal; an existing session remains bounded by its own expiry unless the registration is deleted or its fingerprint is blocked.

## Broker lifetime and reconnect

The API issues a separate NATS user JWT and user seed for each registration response. The response does not expose a server credentials path. It uses connection schema version 2 and sets `Cache-Control: no-store`. Credential formatting redacts the seed and JWT.

Broker authority expires no later than the Worker session. It also has a five-minute maximum, configurable with `--nats.worker-credential-ttl` from one second to five minutes. Zero in a programmatically constructed configuration selects five minutes. The JWT expiry uses whole seconds and rounds down. The API refuses credentials whose resulting expiry is already elapsed.

API block/deletion denial is immediate at the next authorization check. An existing broker connection can retain access to queued dispatch envelopes, log publication, and presence until the issued JWT expires. The five-minute default is the maximum broker revocation delay. Operators can choose a shorter limit at the cost of more enrollment traffic. NATS expires already connected users as well as refusing expired credentials on new connections. Urth does not promise immediate broker revocation.

The Worker renews against the earlier session or broker expiry and updates the JWT/NKey source used for reconnect. It forces reconnect after renewal so the broker uses the new credential. Failed renewal uses bounded retry backoff up to one minute. Existing runs continue under their own API capability. An identity change replaces the transport because the NATS inbox and presence permissions name the Worker UID.

## Production configuration

Worker enrollment requires an HTTPS API URL. TLS can terminate at the API reverse proxy. `--allow-insecure-api` permits HTTP only for an explicit loopback development URL.

NATS clients require `tls://` URLs and authenticated identities by default. Configure `--nats.tls-ca-file` for a private CA. Configure `--nats.tls-cert-file` and `--nats.tls-key-file` together for mutual TLS. Client key files and credential files must be regular private files. Embedded URL credentials are refused. `--nats.allow-insecure` permits a loopback development broker and cannot accompany TLS configuration. The API also requires `--nats.allow-insecure-workers` to issue unauthenticated development connection information.

The API uses three distinct service user identities: `--nats.creds-file` for stream and consumer provisioning, `--nats.publisher-creds-file` for outbox publication, and `--nats.observer-creds-file` for logs, presence, and advisory observation. `natsq.ServicePermissions` supplies the permission templates exercised by the secured broker tests. Provisioner permissions target `URTH_JOBS` administration. Publisher permissions target job publication. Observer permissions target log, presence, and advisory subscriptions. These roles do not share a user identity.

`--nats.worker-account-seed-file` supplies the private NATS account signing seed. Worker JWT permissions grant only the assigned Runner consumer operations, the exact Worker inbox, its exact presence subject, and the Runner log prefix. They deny cross-Runner jobs, other Worker inboxes and presence, job publication, and stream or consumer administration. The operator/account resolver and TLS listener remain broker deployment responsibilities.

## Verification and scope

Tests exercise proof forgery, altered manifests, expiry, concurrent one-use replay, name collision, stable rename, bounded challenge capacity, versioned blocklist updates, claim/block serialization, transient database errors, token revocation with unused valid proofs, and CLI block/unblock behavior. Secured integration tests use a real NATS operator/account JWT broker with mutual TLS and the production HTTP enrollment endpoint. They exercise active connection expiry, denied new connections and invalid TLS clients, credential rotation, reconnect, blocked and deleted sessions, and bounded in-flight status and artifact reporting.

These checks establish the application contract. They do not establish a deployed production certificate rotation procedure, account signing-key rotation, a three-node broker failover exercise, or all historical task 004 and task 024 acceptance criteria. Those operational criteria remain open until their separate evidence exists. This is a fresh protocol: existing unsigned enrollment requests have no compatibility fallback or migration path.
