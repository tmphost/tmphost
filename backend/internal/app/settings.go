package app

import (
	"crypto/rand"
	"encoding/hex"
	"log"

	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

const _defaultSettingsFile = "./_storage/settings.json"

func settingsFile(cfg Config) string {
	if cfg.App.SettingsFile != "" {
		return cfg.App.SettingsFile
	}
	return _defaultSettingsFile
}

// rootAccessToken returns the configured root token, or generates and logs
// one so the setup page can always be reached.
func rootAccessToken(cfg Config) string {
	if cfg.Auth.RootAccessToken != "" {
		return cfg.Auth.RootAccessToken
	}
	b := make([]byte, 24)
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	log.Printf("setup: generated root access token %s (set TMPHOST_AUTH_ROOT_ACCESS_TOKEN to keep one across restarts)", tok)
	return tok
}

// settingsFromConfig is what the setup page shows before anything is saved.
func settingsFromConfig(cfg Config) entity.Settings {
	driver := setupctrl.DriverSQLite
	if cfg.Postgres.Enabled {
		driver = setupctrl.DriverPostgres
	}
	grant := cfg.Credits.MonthlyGrant
	if grant == 0 {
		grant = credit.DefaultMonthlyGrant
	}
	pg := cfg.Postgres
	s3 := cfg.S3
	return entity.Settings{
		Credits: entity.CreditSettings{MonthlyGrant: grant},
		Database: entity.DatabaseSettings{
			Driver: driver,
			SQLite: entity.SQLiteSettings{DSN: cfg.SQLite.DSN},
			Postgres: entity.PostgresSettings{
				Host: pg.Host, Port: pg.Port, User: pg.User, Password: pg.Password, DBName: pg.DBName, SSLMode: pg.SSLMode,
			},
		},
		Storage: entity.StorageSettings{S3: entity.S3Settings{
			Enabled: s3.Enabled, Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
			AccessKeyID: s3.AccessKeyID, SecretAccessKey: s3.SecretAccessKey, UsePathStyle: s3.UsePathStyle,
		}},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: cfg.Auth.Google.ClientID, ClientSecret: cfg.Auth.Google.ClientSecret},
			GitHub: entity.OAuthClientSettings{ClientID: cfg.Auth.GitHub.ClientID, ClientSecret: cfg.Auth.GitHub.ClientSecret},
		},
	}
}

// applySettings overlays the effective settings onto the config.
func applySettings(cfg Config, s entity.Settings) Config {
	cfg.Credits.MonthlyGrant = s.Credits.MonthlyGrant
	cfg.SQLite.Enabled = s.Database.Driver == setupctrl.DriverSQLite
	cfg.SQLite.DSN = s.Database.SQLite.DSN
	pg := s.Database.Postgres
	cfg.Postgres.Enabled = s.Database.Driver == setupctrl.DriverPostgres
	cfg.Postgres.Host, cfg.Postgres.Port, cfg.Postgres.User = pg.Host, pg.Port, pg.User
	cfg.Postgres.Password, cfg.Postgres.DBName, cfg.Postgres.SSLMode = pg.Password, pg.DBName, pg.SSLMode
	s3 := s.Storage.S3
	cfg.S3 = S3Config{
		Enabled: s3.Enabled, Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
		AccessKeyID: s3.AccessKeyID, SecretAccessKey: s3.SecretAccessKey, UsePathStyle: s3.UsePathStyle,
	}
	cfg.Auth.Google = OAuthAppConfig{ClientID: s.Auth.Google.ClientID, ClientSecret: s.Auth.Google.ClientSecret}
	cfg.Auth.GitHub = OAuthAppConfig{ClientID: s.Auth.GitHub.ClientID, ClientSecret: s.Auth.GitHub.ClientSecret}
	return cfg
}
