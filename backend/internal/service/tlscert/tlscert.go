// Package tlscert serves the certificates for HTTPS: the uploaded one for
// the app host, and a wildcard one for the dynamic subdomains that lego
// obtains (and renews) from Let's Encrypt over the Cloudflare DNS-01
// challenge.
package tlscert

//go:generate mockgen -source=tlscert.go -destination=mocks/tlscert.go -package=mocks

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

const (
	// RenewBefore is how long before expiry the wildcard certificate is
	// replaced.
	RenewBefore = 30 * 24 * time.Hour

	// The wildcard certificate is checked daily, plus a random 1 to 5
	// minutes so instances sharing a cache do not all check at once.
	_checkEvery     = 24 * time.Hour
	_checkJitterMin = time.Minute
	_checkJitterMax = 5 * time.Minute
)

// acmeDirectoryURL is the CA certificates are obtained from.
var acmeDirectoryURL = lego.LEDirectoryProduction

type (
	// Config chooses the certificates to serve.
	Config struct {
		// AppHost is the app's host name, served AppCertificate.
		AppHost string
		// AppCertificate and AppPrivateKey are an optional PEM pair.
		AppCertificate string
		AppPrivateKey  string
		Lego           LegoConfig
		// CacheDir keeps the obtained certificate, encrypted, across
		// restarts.
		CacheDir string
	}

	// LegoConfig obtains a certificate for *.Domain.
	LegoConfig struct {
		Enabled bool
		Domain  string
		// Email is the ACME account email; admin@Domain when empty.
		Email              string
		CloudflareAPIToken string
	}

	// Service picks the certificate for each TLS handshake.
	Service interface {
		// GetCertificate is a tls.Config.GetCertificate.
		GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
		// Start loads the cached wildcard certificate and keeps it renewed
		// in the background, when lego is enabled.
		Start()
		// Stop ends the renewal loop.
		Stop()
	}

	certObtainer interface {
		Obtain(request certificate.ObtainRequest) (*certificate.Resource, error)
	}

	service struct {
		cfg     Config
		enc     encryption.Service
		appCert *tls.Certificate

		mu       sync.RWMutex
		legoCert *tls.Certificate

		// obtainer is built on the first certificate request.
		obtainer    certObtainer
		newObtainer func() (certObtainer, error)

		stop     chan struct{}
		stopOnce sync.Once
		done     chan struct{}
	}

	// cachedPair is the plaintext of the cache file.
	cachedPair struct {
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"privateKey"`
	}

	legoUser struct {
		email        string
		registration *registration.Resource
		key          crypto.PrivateKey
	}
)

func (u *legoUser) GetEmail() string                        { return u.email }
func (u *legoUser) GetRegistration() *registration.Resource { return u.registration }
func (u *legoUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// New parses the app certificate, if any. enc seals the cached wildcard
// certificate.
func New(cfg Config, enc encryption.Service) (Service, error) {
	cfg.AppHost = strings.ToLower(cfg.AppHost)
	cfg.Lego.Domain = strings.ToLower(cfg.Lego.Domain)
	s := &service{cfg: cfg, enc: enc, stop: make(chan struct{}), done: make(chan struct{})}
	s.newObtainer = s.newACMEObtainer
	if cfg.AppCertificate != "" {
		// The error is not wrapped: it could quote the private key.
		pair, err := tls.X509KeyPair([]byte(cfg.AppCertificate), []byte(cfg.AppPrivateKey))
		if err != nil {
			return nil, errors.New("tls: the app certificate and private key are not a matching PEM pair")
		}
		s.appCert = &pair
	}
	return s, nil
}

func (s *service) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.ToLower(hello.ServerName)
	if name == "" {
		// Without SNI (e.g. a client dialing an IP) the app is meant.
		name = s.cfg.AppHost
	}
	if name == s.cfg.AppHost && s.appCert != nil {
		return s.appCert, nil
	}
	s.mu.RLock()
	legoCert := s.legoCert
	s.mu.RUnlock()
	for _, c := range []*tls.Certificate{legoCert, s.appCert} {
		if c != nil && c.Leaf.VerifyHostname(name) == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("tls: no certificate for %q", name)
}

func (s *service) Start() {
	if !s.cfg.Lego.Enabled {
		close(s.done)
		return
	}
	if err := s.loadCache(); err != nil {
		log.Printf("tls: no cached wildcard certificate (%v), obtaining one", err)
	}
	go s.manage()
}

func (s *service) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
}

// manage checks the wildcard certificate at once, then daily.
func (s *service) manage() {
	defer close(s.done)
	for {
		s.refresh()
		select {
		case <-s.stop:
			return
		case <-time.After(checkInterval()):
		}
	}
}

func checkInterval() time.Duration {
	return _checkEvery + _checkJitterMin + mathrand.N(_checkJitterMax-_checkJitterMin+1)
}

func (s *service) wildcard() string { return "*." + s.cfg.Lego.Domain }

// refresh obtains a new wildcard certificate unless the current one is
// usable.
func (s *service) refresh() {
	s.mu.RLock()
	curr := s.legoCert
	s.mu.RUnlock()
	if s.usable(curr) {
		return
	}
	log.Printf("tls: obtaining a certificate for %s", s.wildcard())
	res, err := s.obtain()
	if err != nil {
		log.Printf("tls: obtaining a certificate for %s failed: %v", s.wildcard(), err)
		return
	}
	if err := s.install(res.Certificate, res.PrivateKey); err != nil {
		log.Printf("tls: installing the certificate for %s failed: %v", s.wildcard(), err)
		return
	}
	log.Printf("tls: certificate for %s installed", s.wildcard())
}

// usable reports whether cert covers the wildcard and is not due for
// renewal.
func (s *service) usable(cert *tls.Certificate) bool {
	if cert == nil {
		return false
	}
	return time.Until(cert.Leaf.NotAfter) >= RenewBefore && cert.Leaf.VerifyHostname("x"+s.wildcard()[1:]) == nil
}

// obtain asks the CA for the wildcard certificate. The ACME client is set
// up, and the account registered, only the first time one is needed.
func (s *service) obtain() (*certificate.Resource, error) {
	if s.obtainer == nil {
		obtainer, err := s.newObtainer()
		if err != nil {
			return nil, err
		}
		s.obtainer = obtainer
	}
	return s.obtainer.Obtain(certificate.ObtainRequest{Domains: []string{s.wildcard()}, Bundle: true})
}

func (s *service) newACMEObtainer() (certObtainer, error) {
	email := s.cfg.Lego.Email
	if email == "" {
		email = "admin@" + s.cfg.Lego.Domain
	}
	// P-256 key generation from crypto/rand cannot fail.
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	user := &legoUser{email: email, key: key}

	config := lego.NewConfig(user)
	config.CADirURL = acmeDirectoryURL
	config.Certificate.KeyType = certcrypto.EC256
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("lego client: %w", err)
	}

	cf := cloudflare.NewDefaultConfig()
	cf.AuthToken = s.cfg.Lego.CloudflareAPIToken
	dns, err := cloudflare.NewDNSProviderConfig(cf)
	if err != nil {
		return nil, fmt.Errorf("cloudflare DNS provider: %w", err)
	}
	// SetDNS01Provider never fails.
	_ = client.Challenge.SetDNS01Provider(dns)
	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		return nil, fmt.Errorf("ACME registration: %w", err)
	}
	user.registration = reg
	return client.Certificate, nil
}

// install serves the pair and caches it. A pair that does not parse is
// refused before anything changes.
func (s *service) install(certPEM, keyPEM []byte) error {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return err
	}
	if err := s.saveCache(certPEM, keyPEM); err != nil {
		return err
	}
	s.mu.Lock()
	s.legoCert = &pair
	s.mu.Unlock()
	return nil
}

func (s *service) cachePath() string {
	return filepath.Join(s.cfg.CacheDir, s.cfg.Lego.Domain+".cert")
}

// cacheAAD binds the cache file to its domain, so one domain's certificate
// cannot be swapped in for another's.
func (s *service) cacheAAD() []byte {
	return []byte("tmphost.io/tls/v1/" + s.cfg.Lego.Domain)
}

func (s *service) loadCache() error {
	sealed, err := os.ReadFile(s.cachePath())
	if err != nil {
		return err
	}
	plain, err := s.enc.Decrypt(sealed, s.cacheAAD())
	if err != nil {
		return err
	}
	var pair cachedPair
	if err := json.Unmarshal(plain, &pair); err != nil {
		return err
	}
	cert, err := tls.X509KeyPair([]byte(pair.Certificate), []byte(pair.PrivateKey))
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.legoCert = &cert
	s.mu.Unlock()
	return nil
}

func (s *service) saveCache(certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(s.cfg.CacheDir, 0o700); err != nil {
		return err
	}
	// A struct of strings always encodes.
	plain, _ := json.Marshal(cachedPair{Certificate: string(certPEM), PrivateKey: string(keyPEM)})
	path := s.cachePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, s.enc.Encrypt(plain, s.cacheAAD()), 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)
	return os.Rename(tmp, path)
}
