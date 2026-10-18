package app

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
	"github.com/tmphost/tmphost/backend/internal/service/tlscert"
)

const _readHeaderTimeout = 10 * time.Second

// setupTLS prepares the certificates and the http-to-https redirect.
func (a *App) setupTLS(cfg Config, enc encryption.Service, h hosts) {
	certs := tlscert.New(tlscert.Config{
		AppHost: h.app,
		Email:   cfg.TLS.Email,
		Sites: tlscert.SitesConfig{
			Enabled:            cfg.TLS.Lego.Enabled,
			Domain:             cfg.App.SitesDomain,
			CloudflareAPIToken: cfg.TLS.Lego.CloudflareAPIToken,
			Allow:              h.isSite,
		},
		CacheDir: filepath.Join(filepath.Dir(settingsFile(cfg)), "certs"),
	}, enc)
	a.certs = certs
	a.tlsPort = cfg.TLS.Port
	a.tlsAddr = net.JoinHostPort("", strconv.Itoa(cfg.TLS.Port))
	a.redirect = &http.Server{Addr: a.addr, Handler: httpsRedirect(h, cfg.TLS.Port), ReadHeaderTimeout: _readHeaderTimeout}
}

// listenTLS serves HTTPS on the TLS port and the redirect on the app port.
func (a *App) listenTLS() error {
	a.certs.Start()
	ln, err := tls.Listen("tcp", a.tlsAddr, &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: a.certs.GetCertificate,
		// Let's Encrypt validates certificates over this port, with ALPN.
		NextProtos: []string{"http/1.1", tlscert.ACMEProtocol},
	})
	if err != nil {
		return err
	}
	// Whichever server stops first ends Listen: a failure, or Shutdown.
	errs := make(chan error, 2)
	go func() { errs <- ignoreClosed(a.redirect.ListenAndServe()) }()
	go func() { errs <- a.fiber.Listener(ln) }()
	return <-errs
}

// ignoreClosed drops the error a server returns once Shutdown closes it.
func ignoreClosed(err error) error {
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
