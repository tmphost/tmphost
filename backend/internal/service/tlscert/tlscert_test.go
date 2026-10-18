package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

func newPair(t *testing.T, validFor time.Duration, names ...string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func newEnc(t *testing.T) encryption.Service {
	t.Helper()
	enc, err := encryption.New("a-master-key-of-at-least-32-characters")
	require.NoError(t, err)
	return enc
}

type fakeObtainer struct {
	res      *certificate.Resource
	err      error
	requests []certificate.ObtainRequest
	called   chan struct{}
}

func (f *fakeObtainer) Obtain(req certificate.ObtainRequest) (*certificate.Resource, error) {
	f.requests = append(f.requests, req)
	if f.called != nil {
		f.called <- struct{}{}
	}
	return f.res, f.err
}

func legoService(t *testing.T, obtainer certObtainer) *service {
	t.Helper()
	s, err := New(Config{
		AppHost:  "App.Example.com",
		Lego:     LegoConfig{Enabled: true, Domain: "Example.com", CloudflareAPIToken: "cf"},
		CacheDir: filepath.Join(t.TempDir(), "certs"),
	}, newEnc(t))
	require.NoError(t, err)
	svc := s.(*service)
	svc.newObtainer = func() (certObtainer, error) { return obtainer, nil }
	return svc
}

func served(t *testing.T, s Service, name string) []string {
	t.Helper()
	c, err := s.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
	require.NoError(t, err)
	return c.Leaf.DNSNames
}

func TestNew_AppCertificate(t *testing.T) {
	certPEM, keyPEM := newPair(t, time.Hour, "app.example.com")
	_, otherKey := newPair(t, time.Hour, "x")
	_, err := New(Config{AppCertificate: string(certPEM), AppPrivateKey: string(otherKey)}, nil)
	assert.ErrorContains(t, err, "not a matching PEM pair")
	assert.NotContains(t, err.Error(), "PRIVATE KEY")

	s, err := New(Config{AppHost: "APP.example.com", AppCertificate: string(certPEM), AppPrivateKey: string(keyPEM)}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"app.example.com"}, served(t, s, "App.Example.com"))
	assert.Equal(t, []string{"app.example.com"}, served(t, s, ""), "no SNI means the app")
	_, err = s.GetCertificate(&tls.ClientHelloInfo{ServerName: "evil.com"})
	assert.ErrorContains(t, err, `no certificate for "evil.com"`)

	// Without lego, Start and Stop do nothing.
	s.Start()
	s.Stop()
	s.Stop()
}

func TestGetCertificate_WildcardUploadCoversSites(t *testing.T) {
	certPEM, keyPEM := newPair(t, time.Hour, "*.example.com")
	s, err := New(Config{AppHost: "app.example.com", AppCertificate: string(certPEM), AppPrivateKey: string(keyPEM)}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"*.example.com"}, served(t, s, "f0123456789a.example.com"))
}

func TestRefresh_ObtainsInstallsAndCaches(t *testing.T) {
	certPEM, keyPEM := newPair(t, 90*24*time.Hour, "*.example.com")
	obtainer := &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}}
	s := legoService(t, obtainer)

	_, err := s.GetCertificate(&tls.ClientHelloInfo{ServerName: "f0123456789a.example.com"})
	assert.Error(t, err, "nothing yet")

	s.refresh()
	require.Len(t, obtainer.requests, 1)
	assert.Equal(t, []string{"*.example.com"}, obtainer.requests[0].Domains)
	assert.True(t, obtainer.requests[0].Bundle)
	assert.Equal(t, []string{"*.example.com"}, served(t, s, "f0123456789a.example.com"))
	assert.Equal(t, []string{"*.example.com"}, served(t, s, "app.example.com"), "covers the app host without an upload")

	// The cache is encrypted and owner-only.
	raw, err := os.ReadFile(s.cachePath())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "PRIVATE KEY")
	info, err := os.Stat(s.cachePath())
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Still valid: the CA is not asked again.
	s.refresh()
	assert.Len(t, obtainer.requests, 1)

	// A restart serves the cached certificate.
	restarted := legoService(t, &fakeObtainer{err: errors.New("the CA must not be asked")})
	restarted.cfg.CacheDir = s.cfg.CacheDir
	require.NoError(t, restarted.loadCache())
	assert.Equal(t, []string{"*.example.com"}, served(t, restarted, "f0123456789a.example.com"))
}

func TestRefresh_RenewsExpiringOrWrong(t *testing.T) {
	for name, names := range map[string][]string{"expiring": {"*.example.com"}, "wrong domain": {"*.other.com"}} {
		t.Run(name, func(t *testing.T) {
			validFor := 90 * 24 * time.Hour
			if name == "expiring" {
				validFor = 24 * time.Hour
			}
			oldCert, oldKey := newPair(t, validFor, names...)
			newCert, newKey := newPair(t, 90*24*time.Hour, "*.example.com")
			obtainer := &fakeObtainer{res: &certificate.Resource{Certificate: newCert, PrivateKey: newKey}}
			s := legoService(t, obtainer)
			require.NoError(t, s.install(oldCert, oldKey))
			s.refresh()
			assert.Len(t, obtainer.requests, 1)
		})
	}
}

func TestRefresh_Failures(t *testing.T) {
	t.Run("obtainer setup fails", func(t *testing.T) {
		s := legoService(t, nil)
		s.newObtainer = func() (certObtainer, error) { return nil, errors.New("no CA") }
		s.refresh()
		assert.Nil(t, s.obtainer)
	})

	t.Run("CA refuses", func(t *testing.T) {
		s := legoService(t, &fakeObtainer{err: errors.New("refused")})
		s.refresh()
		assert.Nil(t, s.legoCert)
	})

	t.Run("CA returns garbage", func(t *testing.T) {
		s := legoService(t, &fakeObtainer{res: &certificate.Resource{Certificate: []byte("x"), PrivateKey: []byte("y")}})
		s.refresh()
		assert.Nil(t, s.legoCert)
	})

	t.Run("cache not writable", func(t *testing.T) {
		certPEM, keyPEM := newPair(t, 90*24*time.Hour, "*.example.com")
		s := legoService(t, &fakeObtainer{res: &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}})
		notADir := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(notADir, nil, 0o600))
		s.cfg.CacheDir = filepath.Join(notADir, "certs")
		s.refresh()
		assert.Nil(t, s.legoCert, "not served when it cannot be cached")
	})

	t.Run("cache file not writable", func(t *testing.T) {
		certPEM, keyPEM := newPair(t, 90*24*time.Hour, "*.example.com")
		s := legoService(t, nil)
		require.NoError(t, os.MkdirAll(s.cachePath()+".tmp", 0o700))
		assert.Error(t, s.install(certPEM, keyPEM))
	})
}

func TestLoadCache_Failures(t *testing.T) {
	s := legoService(t, nil)
	assert.ErrorIs(t, s.loadCache(), os.ErrNotExist)

	require.NoError(t, os.MkdirAll(s.cfg.CacheDir, 0o700))
	write := func(plain string) {
		require.NoError(t, os.WriteFile(s.cachePath(), s.enc.Encrypt([]byte(plain), s.cacheAAD()), 0o600))
	}
	require.NoError(t, os.WriteFile(s.cachePath(), []byte("tampered"), 0o600))
	assert.ErrorIs(t, s.loadCache(), encryption.ErrDecrypt)
	write("not json")
	assert.Error(t, s.loadCache())
	write(`{"certificate":"x","privateKey":"y"}`)
	assert.Error(t, s.loadCache())

	// Another domain's cache does not open.
	other := legoService(t, nil)
	other.cfg.Lego.Domain = "other.com"
	other.cfg.CacheDir = s.cfg.CacheDir
	write(`{}`)
	require.NoError(t, os.Rename(s.cachePath(), other.cachePath()))
	assert.ErrorIs(t, other.loadCache(), encryption.ErrDecrypt)
}

func TestStartChecksAtOnceAndStops(t *testing.T) {
	obtainer := &fakeObtainer{err: errors.New("CA unavailable"), called: make(chan struct{}, 1)}
	s := legoService(t, obtainer)
	s.Start()
	select {
	case <-obtainer.called:
	case <-time.After(5 * time.Second):
		t.Fatal("the certificate was not checked at start")
	}
	s.Stop()
	s.Stop()
}

func TestCheckIntervalIsADayPlusJitter(t *testing.T) {
	for range 50 {
		d := checkInterval()
		assert.GreaterOrEqual(t, d, 24*time.Hour+time.Minute)
		assert.LessOrEqual(t, d, 24*time.Hour+5*time.Minute)
	}
}

func TestLegoUser(t *testing.T) {
	u := &legoUser{email: "a@b.c", key: "k"}
	assert.Equal(t, "a@b.c", u.GetEmail())
	assert.Nil(t, u.GetRegistration())
	assert.Equal(t, "k", u.GetPrivateKey())
}

// fakeACME is just enough of an ACME server for an account to register.
// Orders are refused.
func fakeACME(t *testing.T, accountStatus int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", time.Now().UnixNano()))
		switch r.URL.Path {
		case "/directory":
			fmt.Fprintf(w, `{"newNonce":%q,"newAccount":%q,"newOrder":%q,"revokeCert":%q,"keyChange":%q}`,
				srv.URL+"/nonce", srv.URL+"/account", srv.URL+"/order", srv.URL+"/revoke", srv.URL+"/key")
		case "/nonce":
			w.WriteHeader(http.StatusOK)
		case "/account":
			if accountStatus != http.StatusCreated {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(accountStatus)
				fmt.Fprint(w, `{"type":"urn:ietf:params:acme:error:malformed","detail":"registration refused"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Location", srv.URL+"/account/1")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"status":"valid"}`)
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"type":"urn:ietf:params:acme:error:unauthorized","detail":"orders refused"}`)
		}
	}))
	t.Cleanup(srv.Close)
	// lego insists on HTTPS, and trusts the roots this variable names.
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	t.Setenv("LEGO_CA_CERTIFICATES", ca)
	return srv
}

func useACMEDirectory(t *testing.T, url string) {
	t.Helper()
	old := acmeDirectoryURL
	acmeDirectoryURL = url
	t.Cleanup(func() { acmeDirectoryURL = old })
	// A Cloudflare setting from the environment must not leak in.
	for _, k := range []string{"CLOUDFLARE_TTL", "CF_API_TOKEN", "CLOUDFLARE_DNS_API_TOKEN", "CLOUDFLARE_EMAIL", "CLOUDFLARE_API_KEY"} {
		t.Setenv(k, "")
	}
}

func acmeService(t *testing.T, email string) *service {
	t.Helper()
	s, err := New(Config{Lego: LegoConfig{Enabled: true, Domain: "example.com", Email: email, CloudflareAPIToken: "cf"}}, newEnc(t))
	require.NoError(t, err)
	return s.(*service)
}

func TestObtain_SetsUpTheACMEClientOnce(t *testing.T) {
	srv := fakeACME(t, http.StatusCreated)
	useACMEDirectory(t, srv.URL+"/directory")
	s := acmeService(t, "")

	_, err := s.obtain()
	assert.ErrorContains(t, err, "orders refused")
	first := s.obtainer
	require.NotNil(t, first)
	_, _ = s.obtain()
	assert.Same(t, first, s.obtainer)
}

func TestNewACMEObtainer_Failures(t *testing.T) {
	t.Run("directory unreachable", func(t *testing.T) {
		srv := fakeACME(t, http.StatusCreated)
		useACMEDirectory(t, srv.URL+"/no-directory-here")
		_, err := acmeService(t, "ops@example.com").newACMEObtainer()
		assert.ErrorContains(t, err, "lego client")
	})
	t.Run("cloudflare misconfigured", func(t *testing.T) {
		srv := fakeACME(t, http.StatusCreated)
		useACMEDirectory(t, srv.URL+"/directory")
		t.Setenv("CLOUDFLARE_TTL", "1")
		_, err := acmeService(t, "").newACMEObtainer()
		assert.ErrorContains(t, err, "cloudflare DNS provider")
	})
	t.Run("registration refused", func(t *testing.T) {
		srv := fakeACME(t, http.StatusBadRequest)
		useACMEDirectory(t, srv.URL+"/directory")
		_, err := acmeService(t, "").newACMEObtainer()
		assert.ErrorContains(t, err, "ACME registration")
	})
}
