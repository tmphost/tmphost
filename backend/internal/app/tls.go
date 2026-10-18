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
func (a *App) setupTLS(cfg Config, enc encryption.Service, h hosts) error {
	certs, err := tlscert.New(tlscert.Config{
		AppHost:        h.app,
		AppCertificate: cfg.TLS.Certificate,
		AppPrivateKey:  cfg.TLS.PrivateKey,
		Lego: tlscert.LegoConfig{
			Enabled:            cfg.TLS.Lego.Enabled,
			Domain:             cfg.App.SitesDomain,
			Email:              cfg.TLS.Lego.Email,
			CloudflareAPIToken: cfg.TLS.Lego.CloudflareAPIToken,
		},
		CacheDir: filepath.Join(filepath.Dir(settingsFile(cfg)), "certs"),
	}, enc)
	if err != nil {
		return err
	}
	a.certs = certs
	a.tlsPort = cfg.TLS.Port
	a.tlsAddr = net.JoinHostPort("", strconv.Itoa(cfg.TLS.Port))
	a.redirect = &http.Server{Addr: a.addr, Handler: httpsRedirect(h, cfg.TLS.Port), ReadHeaderTimeout: _readHeaderTimeout}
	return nil
}

// listenTLS serves HTTPS on the TLS port and the redirect on the app port.
func (a *App) listenTLS() error {
	a.certs.Start()
	ln, err := tls.Listen("tcp", a.tlsAddr, &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: a.certs.GetCertificate,
		NextProtos:     []string{"http/1.1"},
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
