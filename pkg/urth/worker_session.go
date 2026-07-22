package urth

import (
	"context"
	"time"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

// Worker enrolment, in three credentials.
//
// ADR 0002 separates them because they answer different questions and want
// very different lifetimes:
//
//   - the *enrolment* credential proves "I am allowed to join this runner". An
//     operator holds it, it is reusable, and it is all a worker starts with.
//   - the *session* credential proves "I am this worker instance of that
//     runner". The server issues it at registration, it is short-lived, and it
//     is what authenticates a claim.
//   - the *run capability* proves "I am executing this Result". It is scoped to
//     one run and expires with that run's deadline.
//
// The prototype had only the first and the third, which is why the job claim
// read worker and runner identity out of the request body: there was nothing
// else to read it from. WorkerRegistrationResponse closes that gap.

// NATSCredentialType identifies how a worker should authenticate to NATS.
//
// The discriminator exists so the credential mechanism can change without the
// worker changing. ADR 0004 leaves the choice between Auth Callout and issued
// NKey/JWT open; whichever wins, the worker's job is to take the blob it is
// given and connect with it.
type NATSCredentialType string

const (
	// NATSCredentialNone is an unauthenticated connection. Local development
	// only -- it is what a bare `nats-server -js` accepts.
	NATSCredentialNone NATSCredentialType = "none"

	// NATSCredentialFile names a credentials file already present on the
	// worker's disk, provisioned by the operator out of band.
	NATSCredentialFile NATSCredentialType = "creds"

	// NATSCredentialJWT is a short-lived NATS user JWT minted by Urth and
	// scoped to this worker's runner. Not yet issued; see ADR 0004 section 8.
	NATSCredentialJWT NATSCredentialType = "jwt"
)

// NATSCredential carries whatever a worker needs to authenticate to NATS.
type NATSCredential struct {
	Type NATSCredentialType `form:"type" json:"type" yaml:"type" xml:"type"`

	// Value is interpreted according to Type: empty for "none", a path for
	// "creds", the credential itself for "jwt".
	Value string `form:"value,omitempty" json:"value,omitempty" yaml:"value,omitempty" xml:"value,omitempty"`

	// ExpiresAt is when the credential stops working, zero if it does not.
	ExpiresAt time.Time `form:"expiresAt,omitempty" json:"expiresAt,omitempty" yaml:"expiresAt,omitempty" xml:"expiresAt,omitempty"`
}

// NATSConnectionInfoVersion is the schema version of NATSConnectionInfo.
const NATSConnectionInfoVersion = 1

// NATSConnectionInfo tells a registered worker where its queue is.
//
// The worker is told the names of the stream and consumer rather than deriving
// them, so that the control plane keeps ownership of the topology. A worker
// that computed its own consumer name would be one release away from computing
// a different one than the server provisioned.
type NATSConnectionInfo struct {
	SchemaVersion int `form:"schemaVersion" json:"schemaVersion" yaml:"schemaVersion" xml:"schemaVersion"`

	// URLs of the NATS servers to connect to.
	URLs []string `form:"urls" json:"urls" yaml:"urls" xml:"urls"`

	// Stream and Consumer name the JetStream assets to bind to. The worker
	// binds; it never creates them.
	Stream   string `form:"stream" json:"stream" yaml:"stream" xml:"stream"`
	Consumer string `form:"consumer" json:"consumer" yaml:"consumer" xml:"consumer"`

	// Subject this runner's jobs arrive on. Carried for diagnostics -- a worker
	// pulls from the consumer, not the subject.
	Subject string `form:"subject" json:"subject" yaml:"subject" xml:"subject"`

	// LogSubjectPrefix is where this worker may publish run logs. Scoped to the
	// runner so a worker cannot inject lines into another runner's run.
	LogSubjectPrefix string `form:"logSubjectPrefix,omitempty" json:"logSubjectPrefix,omitempty" yaml:"logSubjectPrefix,omitempty" xml:"logSubjectPrefix,omitempty"`

	Credential NATSCredential `form:"credential" json:"credential" yaml:"credential" xml:"credential"`
}

// WorkerRegistrationResponse is what a worker gets back when it enrols.
//
// It replaces the prototype's practice of returning the Runner manifest and
// leaving the worker to dig its own identity out of Status.Instances[0] -- an
// arrangement that broke if the server ever returned more than one instance,
// and that carried no credential at all.
type WorkerRegistrationResponse struct {
	// Runner is the channel this worker joined.
	Runner manifest.ResourceManifest `form:"runner" json:"runner" yaml:"runner" xml:"runner"`

	// Worker is the WorkerInstance identity the server assigned.
	Worker manifest.ResourceManifest `form:"worker" json:"worker" yaml:"worker" xml:"worker"`

	// Session authenticates this worker's later claims. Short-lived by design;
	// the worker re-registers to renew it.
	Session APIToken `form:"session" json:"session" yaml:"session" xml:"session"`

	// SessionExpiresAt is when Session stops being accepted.
	SessionExpiresAt time.Time `form:"sessionExpiresAt" json:"sessionExpiresAt" yaml:"sessionExpiresAt" xml:"sessionExpiresAt"`

	// NATS is where to collect work.
	NATS NATSConnectionInfo `form:"nats" json:"nats" yaml:"nats" xml:"nats"`
}

// WorkerTransportProvider supplies the connection details a newly registered
// worker needs in order to join its runner's queue.
//
// It is an interface, and lives here rather than in the transport package,
// because the transport package imports this one -- pkg/natsq implements
// urth.Scheduler. Defining the shape here and letting the transport fill it in
// keeps the dependency pointing one way, and leaves the domain package free of
// any opinion about which broker is in use.
type WorkerTransportProvider interface {
	// ConnectionInfoFor returns the connection details for a runner, ensuring
	// any transport assets that runner needs exist.
	ConnectionInfoFor(ctx context.Context, runnerUID manifest.ResourceID) (NATSConnectionInfo, error)
}
