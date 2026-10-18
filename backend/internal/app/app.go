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

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gorm.io/gorm"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	"github.com/tmphost/tmphost/backend/internal/handler/api"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/oauth"
	"github.com/tmphost/tmphost/backend/internal/service/scheduler"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

const _renewalTimeout = 10 * time.Minute

type (
	// Config is the server configuration, one field per config section.
	Config struct {
		App      AppConfig
		Auth     AuthConfig
		Credits  CreditsConfig
		SQLite   SQLiteConfig
		Postgres PostgresConfig
		S3       S3Config
		IDGen    IDGenConfig
	}

	// AppConfig is the "app" section.
	AppConfig struct {
		Port int `yaml:"port"`
		// BaseURL is the public URL of the API, used for OAuth callbacks.
		BaseURL string `yaml:"baseUrl"`
		// BasePath is the path prefix browsers reach the app under when a
		// proxy strips it before forwarding ("" or "/x").
		BasePath     string `yaml:"basePath"`
		CookieSecure bool   `yaml:"cookieSecure"`
		// SettingsFile is where the setup page saves settings. When it
		// exists, it overrides the matching config values.
		SettingsFile string `yaml:"settingsFile"`
	}

	// AuthConfig is the "auth" section.
	AuthConfig struct {
		JWTSecret string `yaml:"jwtSecret"`
		// RootAccessToken unlocks the setup page. A random one is generated
		// (and logged) at startup when it is empty.
		RootAccessToken string         `yaml:"rootAccessToken"`
		Google          OAuthAppConfig `yaml:"google"`
		GitHub          OAuthAppConfig `yaml:"github"`
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
		Enabled    bool   `yaml:"enabled"`
		Host       string `yaml:"host"`
		Port       int    `yaml:"port"`
		User       string `yaml:"user"`
		Password   string `yaml:"password"`
		DBName     string `yaml:"dbname"`
		SSLMode    string `yaml:"sslmode"`
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
	}
)

// New builds the server. web holds the built frontend; it may be empty.
func New(cfg Config, web fs.FS) (*App, error) {
	tokens, err := token.New(token.Config{Secret: cfg.Auth.JWTSecret})
	if err != nil {
		return nil, err
	}
	setup, err := setupctrl.New(setupctrl.Params{
		Repository:      repository.NewSettingsRepository(settingsFile(cfg)),
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

	f := fiber.New(fiber.Config{DisableStartupMessage: true})
	f.Use(recover.New())
	api.New(api.Params{Router: f, Auth: auth, Setup: setup, BasePath: basePath, CookieSecure: cfg.App.CookieSecure})
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
	return a, nil
}

// Handler exposes the HTTP handler, for tests.
func (a *App) Handler() *fiber.App { return a.fiber }

// Listen starts the background jobs and serves until Shutdown is called.
func (a *App) Listen() error {
	// Catch up on a renewal missed while the server was down.
	go a.renewCredits()
	a.scheduler.Start()
	return a.fiber.Listen(a.addr)
}

// Shutdown stops the server and the jobs gracefully, then closes the
// database.
func (a *App) Shutdown(ctx context.Context) error {
	err := a.fiber.ShutdownWithContext(ctx)
	a.scheduler.Stop()
	// The dialectors are backed by *sql.DB, so DB() cannot fail.
	sqlDB, _ := a.db.DB()
	return errors.Join(err, sqlDB.Close())
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
	return out
}
