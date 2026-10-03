package natsq_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"github.com/sre-norns/urth/pkg/natsq"
	"github.com/stretchr/testify/require"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func brokerTLS(t *testing.T) (*tls.Config, string, string, string) {
	t.Helper()
	public, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, key)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	cert := filepath.Join(dir, "client.pem")
	private := filepath.Join(dir, "client.key")
	require.NoError(t, os.WriteFile(ca, certPEM, 0600))
	require.NoError(t, os.WriteFile(cert, certPEM, 0600))
	require.NoError(t, os.WriteFile(private, keyPEM, 0600))
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM(certPEM))
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}, ClientCAs: pool, ClientAuth: tls.RequireAndVerifyClientCert}, ca, cert, private
}
func TestNATSSecurityConfigurationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		url      string
		insecure bool
		creds    string
		allowed  bool
	}{
		{"nats://localhost:4222", false, "", false}, {"nats://localhost:4222", true, "", true}, {"nats://example.com:4222", true, "", false}, {"tls://example.com:4222", false, "issued", true}, {"tls://example.com:4222", false, "", false}, {"nats://user:secret@localhost:4222", true, "", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			err := (natsq.ClientConfig{URL: tc.url, AllowInsecure: tc.insecure, UserCredentials: tc.creds}).ValidateSecurity()
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	require.Error(t, (natsq.ClientConfig{URL: "tls://localhost:4222", UserCredentials: "issued", TLSCertFile: "certificate"}).ValidateSecurity())
	require.Error(t, (natsq.Config{ClientConfig: natsq.ClientConfig{URL: "tls://localhost:4222", UserCredentials: "issued"}}).ValidateServiceRoles())
	cfg := testConfig()
	cfg.WorkerCredentialTTL = 6 * time.Minute
	require.Error(t, cfg.Validate())
	cfg.WorkerCredentialTTL = time.Millisecond
	require.Error(t, cfg.Validate())
}
