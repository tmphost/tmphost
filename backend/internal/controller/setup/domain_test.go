package setup

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

const _icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"></svg>`

// testPair is a self-signed PEM pair for names, expiring at notAfter.
func testPair(t *testing.T, notAfter time.Time, names ...string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-48 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}))
}

func okta() entity.OAuthProviderSettings {
	return entity.OAuthProviderSettings{
		Name: " Okta SSO ", Icon: " " + _icon + " ", ClientID: " oid ", ClientSecret: " osecret ",
		AuthURL: " https://acme.okta.com/ ",
	}
}

func update(t *testing.T, e *env, next entity.Settings) (*entity.UpdateSettingsResponse, error) {
	t.Helper()
	return e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
}

func TestUpdateSettings_DomainAndProviders(t *testing.T) {
	e := newEnv(t)
	next := defaults()
	next.Domain = entity.DomainSettings{AppURL: " HTTPS://App.TmpHost.io/ ", SitesDomain: " *.TmpHost.IO. "}
	next.Auth.Providers = []entity.OAuthProviderSettings{okta()}
	original := next.Auth.Providers[0]

	e.repo.EXPECT().SaveSettings(gomock.Any()).DoAndReturn(func(m model.Settings) error {
		require.Len(t, m.Auth.Providers, 1)
		assert.Equal(t, "osecret", m.Auth.Providers[0].ClientSecret)
		return nil
	})
	rs, err := update(t, e, next)
	require.NoError(t, err)
	assert.Equal(t, original, next.Auth.Providers[0], "the request is not changed in place")
	assert.True(t, rs.RestartRequired)
	assert.Equal(t, entity.DomainSettings{AppURL: "https://app.tmphost.io", SitesDomain: "tmphost.io"}, rs.Settings.Domain)
	assert.Equal(t, []entity.OAuthProviderSettings{{
		ID: "okta-sso", Name: "Okta SSO", Icon: _icon, ClientID: "oid", AuthURL: "https://acme.okta.com",
	}}, rs.Settings.Auth.Providers, "secret blanked in the response")
	assert.Equal(t, map[string]bool{"okta-sso": true}, rs.SecretsSet.ProviderClientSecrets)
	assert.Equal(t, "osecret", e.ctrl.Current().Auth.Providers[0].ClientSecret, "kept in the server's copy")

	// A blank secret keeps the one saved under the same ID.
	again := rs.Settings
	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
	_, err = update(t, e, again)
	require.NoError(t, err)
	assert.Equal(t, "osecret", e.ctrl.Current().Auth.Providers[0].ClientSecret)

	// Renamed, it is a new provider with no secret to keep.
	again.Auth.Providers[0].Name = "Okta 2"
	_, err = update(t, e, again)
	assert.ErrorIs(t, err, ErrInvalidSettings)
}

func TestUpdateSettings_TLS(t *testing.T) {
	appCert, appKey := testPair(t, time.Now().Add(90*24*time.Hour), "app.example.com")
	tlsOn := func(s *entity.Settings) {
		s.Domain = entity.DomainSettings{AppURL: "https://app.example.com", SitesDomain: "example.com"}
		s.TLS = entity.TLSSettings{Enabled: true, Certificate: appCert, PrivateKey: appKey}
	}

	t.Run("uploaded certificate", func(t *testing.T) {
		e := newEnv(t)
		next := defaults()
		tlsOn(&next)
		next.TLS.Port = 0
		e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil).Times(2)
		rs, err := update(t, e, next)
		require.NoError(t, err)
		assert.Equal(t, DefaultTLSPort, rs.Settings.TLS.Port)
		assert.Empty(t, rs.Settings.TLS.PrivateKey)
		assert.Equal(t, strings.TrimSpace(appCert), rs.Settings.TLS.Certificate)
		assert.True(t, rs.SecretsSet.TLSPrivateKey)

		// The key is kept while the certificate stays, and goes with it.
		rs, err = update(t, e, rs.Settings)
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSpace(appKey), e.ctrl.Current().TLS.PrivateKey)
		cleared := rs.Settings
		cleared.TLS = entity.TLSSettings{}
		e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
		rs, err = update(t, e, cleared)
		require.NoError(t, err)
		assert.False(t, rs.SecretsSet.TLSPrivateKey)
	})

	t.Run("lego covers the app host", func(t *testing.T) {
		e := newEnv(t)
		next := defaults()
		tlsOn(&next)
		next.TLS.Certificate, next.TLS.PrivateKey = "", ""
		next.TLS.Lego = entity.LegoSettings{Enabled: true, Email: "ops@example.com", CloudflareAPIToken: "cf"}
		e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil).Times(2)
		rs, err := update(t, e, next)
		require.NoError(t, err)
		assert.Empty(t, rs.Settings.TLS.Lego.CloudflareAPIToken)
		assert.True(t, rs.SecretsSet.CloudflareAPIToken)
		_, err = update(t, e, rs.Settings)
		require.NoError(t, err)
		assert.Equal(t, "cf", e.ctrl.Current().TLS.Lego.CloudflareAPIToken, "token kept")
	})

	t.Run("disabled TLS is not validated", func(t *testing.T) {
		e := newEnv(t)
		next := defaults()
		next.TLS = entity.TLSSettings{Certificate: "junk", PrivateKey: "junk", Lego: entity.LegoSettings{Enabled: true}}
		e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
		_, err := update(t, e, next)
		require.NoError(t, err)
	})

	expired, expiredKey := testPair(t, time.Now().Add(-time.Hour), "app.example.com")
	other, otherKey := testPair(t, time.Now().Add(time.Hour), "other.example.com")
	cases := map[string]func(*entity.Settings){
		"http app URL":  func(s *entity.Settings) { s.Domain.AppURL = "http://app.example.com" },
		"port too high": func(s *entity.Settings) { s.TLS.Port = 70000 },
		"lego without domain": func(s *entity.Settings) {
			s.Domain.SitesDomain = ""
			s.TLS.Lego = entity.LegoSettings{Enabled: true, CloudflareAPIToken: "cf"}
		},
		"lego without token": func(s *entity.Settings) { s.TLS.Lego = entity.LegoSettings{Enabled: true} },
		"lego bad email": func(s *entity.Settings) {
			s.TLS.Lego = entity.LegoSettings{Enabled: true, CloudflareAPIToken: "cf", Email: "not an email"}
		},
		"no certificate, no lego": func(s *entity.Settings) { s.TLS.Certificate, s.TLS.PrivateKey = "", "" },
		"lego cannot cover a nested app host": func(s *entity.Settings) {
			s.Domain.AppURL = "https://a.b.example.com"
			s.TLS.Certificate, s.TLS.PrivateKey = "", ""
			s.TLS.Lego = entity.LegoSettings{Enabled: true, CloudflareAPIToken: "cf"}
		},
		"certificate without key": func(s *entity.Settings) { s.TLS.PrivateKey = "" },
		"mismatched key":          func(s *entity.Settings) { s.TLS.PrivateKey = otherKey },
		"not covering app host":   func(s *entity.Settings) { s.TLS.Certificate, s.TLS.PrivateKey = other, otherKey },
		"expired":                 func(s *entity.Settings) { s.TLS.Certificate, s.TLS.PrivateKey = expired, expiredKey },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnvWith(t, func(s *entity.Settings) {})
			next := defaults()
			tlsOn(&next)
			mutate(&next)
			_, err := update(t, e, next)
			assert.ErrorIs(t, err, ErrInvalidSettings)
			assert.NotContains(t, err.Error(), "PRIVATE KEY")
		})
	}
}

func TestUpdateSettings_DomainAndProviderValidation(t *testing.T) {
	many := make([]entity.OAuthProviderSettings, _maxProviders+1)
	for i := range many {
		many[i] = okta()
		many[i].Name = "p" + strings.Repeat("x", i)
	}
	provider := func(mutate func(*entity.OAuthProviderSettings)) func(*entity.Settings) {
		return func(s *entity.Settings) {
			p := okta()
			mutate(&p)
			s.Auth.Providers = []entity.OAuthProviderSettings{p}
		}
	}
	cases := map[string]func(*entity.Settings){
		"app URL missing":       func(s *entity.Settings) { s.Domain.AppURL = "" },
		"app URL unparsable":    func(s *entity.Settings) { s.Domain.AppURL = "http://[::1" },
		"app URL with path":     func(s *entity.Settings) { s.Domain.AppURL = "https://x.io/app" },
		"app URL ftp":           func(s *entity.Settings) { s.Domain.AppURL = "ftp://x.io" },
		"sites domain invalid":  func(s *entity.Settings) { s.Domain.SitesDomain = "bad_domain.io" },
		"too many providers":    func(s *entity.Settings) { s.Auth.Providers = many },
		"provider without name": provider(func(p *entity.OAuthProviderSettings) { p.Name = " - " }),
		"provider name too long": provider(func(p *entity.OAuthProviderSettings) {
			p.Name = strings.Repeat("n", _maxProviderName+1)
		}),
		"provider built-in name": provider(func(p *entity.OAuthProviderSettings) { p.Name = "GitHub" }),
		"duplicate providers": func(s *entity.Settings) {
			s.Auth.Providers = []entity.OAuthProviderSettings{okta(), okta()}
		},
		"icon not svg": provider(func(p *entity.OAuthProviderSettings) { p.Icon = "<img>" }),
		"icon too big": provider(func(p *entity.OAuthProviderSettings) {
			p.Icon = "<svg>" + strings.Repeat(" ", _maxIconBytes) + "</svg>"
		}),
		"no client id":        provider(func(p *entity.OAuthProviderSettings) { p.ClientID = "" }),
		"no client secret":    provider(func(p *entity.OAuthProviderSettings) { p.ClientSecret = "" }),
		"auth URL http":       provider(func(p *entity.OAuthProviderSettings) { p.AuthURL = "http://idp.example.com" }),
		"auth URL ftp":        provider(func(p *entity.OAuthProviderSettings) { p.AuthURL = "ftp://idp.example.com" }),
		"auth URL query":      provider(func(p *entity.OAuthProviderSettings) { p.AuthURL = "https://idp.example.com?x=1" }),
		"auth URL no host":    provider(func(p *entity.OAuthProviderSettings) { p.AuthURL = "https://" }),
		"auth URL unparsable": provider(func(p *entity.OAuthProviderSettings) { p.AuthURL = "https://[::1" }),
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			next := defaults()
			mutate(&next)
			_, err := update(t, e, next)
			assert.ErrorIs(t, err, ErrInvalidSettings)
		})
	}

	t.Run("http auth URL on this machine", func(t *testing.T) {
		e := newEnv(t)
		next := defaults()
		provider(func(p *entity.OAuthProviderSettings) { p.AuthURL = "http://localhost:8080/realms/x" })(&next)
		e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
		_, err := update(t, e, next)
		require.NoError(t, err)
	})
}

func TestHelpers(t *testing.T) {
	assert.Equal(t, "okta-sso-2", ProviderID("  Okta SSO #2! "))
	assert.Equal(t, "app.tmphost.io", AppHost("https://app.tmphost.io:8443"))
	assert.Equal(t, "", AppHost("http://[::1"))
	assert.True(t, LegoCovers("tmphost.io", "app.tmphost.io"))
	assert.False(t, LegoCovers("tmphost.io", "a.b.tmphost.io"))
	assert.False(t, LegoCovers("tmphost.io", "tmphost.io"))
	assert.False(t, LegoCovers("", "app."))
	assert.Equal(t, "not a url", normalizeAppURL("not a url"))
	assert.Equal(t, "https://x.io:8443", normalizeAppURL("HTTPS://X.io:8443/"))
}
