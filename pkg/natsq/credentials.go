package natsq

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

func validateRunnerAddress(account manifest.ResourceID, name manifest.ResourceName) error {
	if _, err := uuid.Parse(string(account)); err != nil {
		return fmt.Errorf("invalid runner account: %w", err)
	}
	return manifest.ValidateSubdomainName(string(name))
}

func (s *scheduler) workerCredential(runner urth.Runner) (urth.NATSCredential, error) {
	if err := validateRunnerAddress(runner.Account, runner.Name); err != nil {
		return urth.NATSCredential{}, err
	}
	if s.cfg.WorkerAccountSeedFile == "" {
		if !s.cfg.AllowInsecureWorkers {
			return urth.NATSCredential{}, fmt.Errorf("configure --nats.worker-account-seed-file; unauthenticated workers require --nats.allow-insecure-workers for an isolated development broker")
		}
		return urth.NATSCredential{Type: urth.NATSCredentialNone}, nil
	}
	seed, err := os.ReadFile(s.cfg.WorkerAccountSeedFile)
	if err != nil {
		return urth.NATSCredential{}, fmt.Errorf("read worker signing seed: %w", err)
	}
	account, err := nkeys.FromSeed([]byte(strings.TrimSpace(string(seed))))
	if err != nil {
		return urth.NATSCredential{}, fmt.Errorf("invalid worker signing seed")
	}
	defer account.Wipe()
	public, err := account.PublicKey()
	if err != nil || !nkeys.IsValidPublicAccountKey(public) {
		return urth.NATSCredential{}, fmt.Errorf("worker signing seed must be a NATS account key")
	}
	user, err := nkeys.CreateUser()
	if err != nil {
		return urth.NATSCredential{}, err
	}
	defer user.Wipe()
	userPublic, err := user.PublicKey()
	if err != nil {
		return urth.NATSCredential{}, err
	}
	claims := jwt.NewUserClaims(userPublic)
	claims.Name = "urth-worker-" + string(runner.UID)
	expires := time.Now().Add(time.Hour)
	claims.Expires = expires.Unix()
	consumer := RunnerConsumerName(runner.Account, runner.Name)
	claims.Pub.Allow = jwt.StringList{
		"$JS.API.CONSUMER.INFO." + JobsStreamName + "." + consumer,
		"$JS.API.CONSUMER.MSG.NEXT." + JobsStreamName + "." + consumer,
		"$JS.ACK." + JobsStreamName + "." + consumer + ".>",
		"$JS.ACK.*.*." + JobsStreamName + "." + consumer + ".>",
		RunnerLogSubjectPrefix(runner.UID), RunnerPresenceSubjectPrefix(runner.UID),
	}
	claims.Sub.Allow = jwt.StringList{"_INBOX." + string(runner.UID) + ".>"}
	token, err := claims.Encode(account)
	if err != nil {
		return urth.NATSCredential{}, err
	}
	userSeed, err := user.Seed()
	if err != nil {
		return urth.NATSCredential{}, err
	}
	creds, err := jwt.FormatUserConfig(token, userSeed)
	return urth.NATSCredential{Type: urth.NATSCredentialJWT, Value: string(creds), ExpiresAt: expires}, err
}
