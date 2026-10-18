// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package app wires the services, controllers and handlers into a server.
package app

import (
	"context"
	"errors"
	"io/fs"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"gorm.io/gorm"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/handler/api"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/oauth"
	"github.com/tmphost/tmphost/backend/internal/service/scheduler"
	"github.com/tmphost/tmphost/backend/internal/service/tlscert"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

const _renewalTimeout = 10 * time.Minute

// _listenConfig is how every server listens; main logs the address.
var _listenConfig = fiber.ListenConfig{DisableStartupMessage: true}

type (
	// Config is the server configuration, one field per config section.
	Config struct {
		// MasterKey encrypts the settings file. It comes only from the
		// TMPHOSTIO_MASTER_KEY environment variable, never from YAML.
		MasterKey string
		App       AppConfig
		TLS       TLSConfig
		Auth      AuthConfig
		Credits   CreditsConfig
		SQLite    SQLiteConfig
		Postgres  PostgresConfig
		S3        S3Config
		IDGen     IDGenConfig
	}

	// AppConfig is the "app" section.
	AppConfig struct {
		Port int `yaml:"port"`
		// BaseURL is the public URL of the app. Its host is the app host,
		// and OAuth callbacks are built on it.
		BaseURL string `yaml:"baseUrl"`
		// SitesDomain is the parent domain of the dynamic subdomains
		// (f<11 characters>.<SitesDomain>); empty disables them.
		SitesDomain string `yaml:"sitesDomain"`
		// BasePath is the path prefix browsers reach the app under when a
		// proxy strips it before forwarding ("" or "/x").
		BasePath     string `yaml:"basePath"`
		CookieSecure bool   `yaml:"cookieSecure"`
		// SettingsFile is where the setup page saves settings. When it
		// exists, it overrides the matching config values.
		SettingsFile string `yaml:"settingsFile"`
	}

	// TLSConfig is the "tls" section: HTTPS with Let's Encrypt
	// certificates, obtained and renewed automatically.
	TLSConfig struct {
		Enabled bool `yaml:"enabled"`
		// Port is the HTTPS port; the app port then redirects to it.
		Port int `yaml:"port"`
		// Email is the optional ACME account email.
		Email string     `yaml:"email"`
		Lego  LegoConfig `yaml:"lego"`
	}

	// LegoConfig is the "tls.lego" section: certificates for the dynamic
	// subdomains, one per subdomain, or a wildcard over Cloudflare DNS-01
	// when the token is set.
	LegoConfig struct {
		Enabled            bool   `yaml:"enabled"`
		CloudflareAPIToken string `yaml:"cloudflareApiToken"`
	}

	// AuthConfig is the "auth" section.
	AuthConfig struct {
		JWTSecret string `yaml:"jwtSecret"`
		// RootAccessToken unlocks the setup page. A random one is generated
		// (and logged) at startup when it is empty.
		RootAccessToken string         `yaml:"rootAccessToken"`
		Google          OAuthAppConfig `yaml:"google"`
		GitHub          OAuthAppConfig `yaml:"github"`
		// Providers are the custom OpenID Connect providers, set only on
		// the setup page.
		Providers []entity.OAuthProviderSettings `yaml:"-"`
	}

	// OAuthAppConfig is an OAuth client registration. A provider without a
	// client ID is disabled.
	OAuthAppConfig struct {
		ClientID     string `yaml:"clientId"`
		ClientSecret string `yaml:"clientSecret"`
	}

	// CreditsConfig is the "credits" section.
	CreditsConfig struct {
		MonthlyGrant int `yaml:"monthlyGrant"`
	}

	// PoolConfig tunes a database connection pool.
	PoolConfig struct {
		MaxIdleConns    int           `yaml:"maxIdleConns"`
		MaxOpenConns    int           `yaml:"maxOpenConns"`
		MaxConnLifetime time.Duration `yaml:"maxConnLifetime"`
	}

	// SQLiteConfig is the "sqlite" section.
	SQLiteConfig struct {
		Enabled    bool   `yaml:"enabled"`
		DSN        string `yaml:"dsn"`
		PoolConfig `yaml:",inline"`
	}

	// PostgresConfig is the "postgres" section. PostgreSQL wins when both
	// databases are enabled.
	PostgresConfig struct {
		Enabled bool `yaml:"enabled"`
		// DSN is a postgres:// URL or a key=value connection string.
		DSN        string `yaml:"dsn"`
		PoolConfig `yaml:",inline"`
	}

	// S3Config is the "s3" section (an S3-compatible bucket).
	S3Config struct {
		Enabled         bool   `yaml:"enabled"`
		Endpoint        string `yaml:"endpoint"`
		Region          string `yaml:"region"`
		Bucket          string `yaml:"bucket"`
		AccessKeyID     string `yaml:"accessKeyId"`
		SecretAccessKey string `yaml:"secretAccessKey"`
		UsePathStyle    bool   `yaml:"usePathStyle"`
	}

	// IDGenConfig is the "idgen" section.
	IDGenConfig struct {
		Node uint16 `yaml:"node"`
	}

	// App is a configured, runnable server.
	App struct {
		fiber     *fiber.App
		addr      string
		db        *gorm.DB
		credits   credit.Controller
		scheduler scheduler.Service
		// The rest is set only with TLS enabled.
		tlsAddr  string
		certs    tlscert.Service
		tlsPort  int
		redirect *fiber.App
	}
)

// New builds the server. web holds the built frontend; it may be empty.
func New(cfg Config, web fs.FS) (*App, error) {
	tokens, err := token.New(token.Config{Secret: cfg.Auth.JWTSecret})
	if err != nil {
		return nil, err
	}
	enc, err := encryption.New(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	setup, err := setupctrl.New(setupctrl.Params{
		Repository:      repository.NewSettingsRepository(settingsFile(cfg), enc),
		Token:           tokens,
		RootAccessToken: rootAccessToken(cfg),
		Defaults:        settingsFromConfig(cfg),
	})
	if err != nil {
		return nil, err
	}
	// Saved settings override the config file and environment.
	cfg = applySettings(cfg, setup.Current())

	db, err := openDatabase(cfg)
	if err != nil {
		return nil, err
	}

	users := repository.NewUserRepository(db)
	basePath := strings.TrimRight(cfg.App.BasePath, "/")
	auth := authctrl.New(authctrl.Params{
		Providers: providers(cfg),
		Token:     tokens,
		Users:     users,
		IDGen:     idgen.New(cfg.IDGen.Node),
		Grant:     setup.MonthlyGrant,
		BasePath:  basePath,
	})

	h := newHosts(cfg.App.BaseURL, cfg.App.SitesDomain)
	f := fiber.New()
	f.Use(recover.New())
	f.Use(hostRouter(h))
	// A session cookie for an https app must never travel over plain http.
	cookieSecure := cfg.App.CookieSecure || strings.HasPrefix(cfg.App.BaseURL, "https://")
	api.New(api.Params{Router: f, Auth: auth, Setup: setup, BasePath: basePath, CookieSecure: cookieSecure})
	f.Get("/*", spaHandler(web, basePath))

	a := &App{
		fiber:     f,
		addr:      net.JoinHostPort("", strconv.Itoa(cfg.App.Port)),
		db:        db,
		credits:   credit.New(credit.Params{Users: users, Grant: setup.MonthlyGrant}),
		scheduler: scheduler.New(),
	}
	// RenewalSchedule is a valid constant spec, so this cannot fail.
	_ = a.scheduler.AddFunc(credit.RenewalSchedule, a.renewCredits)
	if cfg.TLS.Enabled {
		a.setupTLS(cfg, enc, h)
	}
	return a, nil
}

// Handler exposes the HTTP handler, for tests.
func (a *App) Handler() *fiber.App { return a.fiber }

// Listen starts the background jobs and serves until Shutdown is called.
// With TLS enabled the app serves HTTPS, and the app port redirects to it.
func (a *App) Listen() error {
	// Catch up on a renewal missed while the server was down.
	go a.renewCredits()
	a.scheduler.Start()
	if a.certs == nil {
		return a.fiber.Listen(a.addr, _listenConfig)
	}
	return a.listenTLS()
}

// Shutdown stops the server and the jobs gracefully, then closes the
// database.
func (a *App) Shutdown(ctx context.Context) error {
	err := a.fiber.ShutdownWithContext(ctx)
	if a.certs != nil {
		err = errors.Join(err, a.redirect.ShutdownWithContext(ctx))
		a.certs.Stop()
	}
	a.scheduler.Stop()
	return errors.Join(err, a.closeDB())
}

func (a *App) closeDB() error {
	// The dialectors are backed by *sql.DB, so DB() cannot fail.
	sqlDB, _ := a.db.DB()
	return sqlDB.Close()
}

func (a *App) renewCredits() {
	ctx, cancel := context.WithTimeout(context.Background(), _renewalTimeout)
	defer cancel()
	rs, err := a.credits.RenewDueCredits(ctx)
	if err != nil {
		log.Printf("credit renewal: renewed %d before failing: %v", rs.Renewed, err)
		return
	}
	log.Printf("credit renewal: renewed %d accounts", rs.Renewed)
}

func providers(cfg Config) []oauth.Provider {
	callback := func(name string) string {
		return strings.TrimRight(cfg.App.BaseURL, "/") + api.RoutePrefix + "/auth/" + name + "/callback"
	}
	var out []oauth.Provider
	if g := cfg.Auth.Google; g.ClientID != "" {
		out = append(out, oauth.NewGoogle(oauth.Credentials{ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: callback(oauth.ProviderGoogle)}))
	}
	if g := cfg.Auth.GitHub; g.ClientID != "" {
		out = append(out, oauth.NewGitHub(oauth.Credentials{ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: callback(oauth.ProviderGitHub)}))
	}
	for _, p := range cfg.Auth.Providers {
		out = append(out, oauth.NewOIDC(oauth.OIDCParams{
			Name: p.ID, Label: p.Name, Icon: p.Icon, Issuer: p.AuthURL,
			Credentials: oauth.Credentials{ClientID: p.ClientID, ClientSecret: p.ClientSecret, RedirectURL: callback(p.ID)},
		}))
	}
	return out
}
