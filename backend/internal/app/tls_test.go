package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

func selfSigned(t *testing.T, name string) (certPEM, keyPEM string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(90 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	pool = x509.NewCertPool()
	pool.AddCert(leaf)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), pool
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// tlsConfig saves TLS settings the way the setup page does, since the app
// certificate never comes from the config file.
func tlsConfig(t *testing.T, certPEM, keyPEM string) Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.App.Port = freePort(t)
	s := settingsFromConfig(cfg)
	s.Domain.AppURL = "https://app.example.com"
	s.TLS.Enabled = true
	s.TLS.Port = freePort(t)
	s.TLS.Certificate, s.TLS.PrivateKey = certPEM, keyPEM
	enc, err := encryption.New(cfg.MasterKey)
	require.NoError(t, err)
	require.NoError(t, repository.NewSettingsRepository(cfg.App.SettingsFile, enc).SaveSettings(mappermodel.FromSettingsEntityToSettingsModel(s)))
	return cfg
}

func TestNew_TLSBadCertificate(t *testing.T) {
	certPEM, _, _ := selfSigned(t, "app.example.com")
	_, otherKey, _ := selfSigned(t, "app.example.com")
	_, err := New(tlsConfig(t, certPEM, otherKey), fstest.MapFS{})
	assert.ErrorContains(t, err, "not a matching PEM pair")
}

func TestListenTLS(t *testing.T) {
	certPEM, keyPEM, pool := selfSigned(t, "app.example.com")
	a, err := New(tlsConfig(t, certPEM, keyPEM), fstest.MapFS{})
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- a.Listen() }()

	tlsAddr := net.JoinHostPort("127.0.0.1", itoaPort(a.tlsPort))
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "app.example.com"},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, tlsAddr)
		},
	}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	var res *http.Response
	require.Eventually(t, func() bool {
		res, err = client.Get("https://app.example.com/api/v1/auth/config")
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	res.Body.Close()

	// Plain http on the app port redirects to https.
	req, err := http.NewRequest(http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", itoaPort(appPort(t, a)))+"/setup", nil)
	require.NoError(t, err)
	req.Host = "app.example.com"
	require.Eventually(t, func() bool {
		res, err = (&http.Transport{}).RoundTrip(req)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, http.StatusMovedPermanently, res.StatusCode)
	assert.Equal(t, "https://app.example.com:"+itoaPort(a.tlsPort)+"/setup", res.Header.Get("Location"))
	res.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, a.Shutdown(ctx))
	assert.NoError(t, <-done)
}

func TestListenTLS_PortsInUse(t *testing.T) {
	certPEM, keyPEM, _ := selfSigned(t, "app.example.com")

	t.Run("https port", func(t *testing.T) {
		a, err := New(tlsConfig(t, certPEM, keyPEM), fstest.MapFS{})
		require.NoError(t, err)
		busy, err := net.Listen("tcp", a.tlsAddr)
		require.NoError(t, err)
		defer busy.Close()
		assert.Error(t, a.Listen())
		_ = a.Shutdown(context.Background())
	})

	t.Run("http port", func(t *testing.T) {
		a, err := New(tlsConfig(t, certPEM, keyPEM), fstest.MapFS{})
		require.NoError(t, err)
		busy, err := net.Listen("tcp", a.addr)
		require.NoError(t, err)
		defer busy.Close()
		assert.Error(t, a.Listen())
		_ = a.Shutdown(context.Background())
	})
}

func appPort(t *testing.T, a *App) int {
	t.Helper()
	_, port, err := net.SplitHostPort(a.addr)
	require.NoError(t, err)
	n, err := net.LookupPort("tcp", port)
	require.NoError(t, err)
	return n
}

func itoaPort(n int) string { return strconv.Itoa(n) }

func TestIgnoreClosed(t *testing.T) {
	assert.NoError(t, ignoreClosed(http.ErrServerClosed))
	assert.ErrorIs(t, ignoreClosed(net.ErrClosed), net.ErrClosed)
}
