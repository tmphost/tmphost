package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		App: AppConfig{
			BaseURL: "http://localhost:3000/", BasePath: "/base/",
			SettingsFile: filepath.Join(t.TempDir(), "settings.json"),
		},
		Auth: AuthConfig{
			JWTSecret:       "secret",
			RootAccessToken: "root-token",
			Google:          OAuthAppConfig{ClientID: "g", ClientSecret: "gs"},
			GitHub:          OAuthAppConfig{ClientID: "h", ClientSecret: "hs"},
		},
		SQLite: SQLiteConfig{
			Enabled: true, DSN: filepath.Join(t.TempDir(), "nested", "tmphost.db"),
			PoolConfig: PoolConfig{MaxIdleConns: 2, MaxOpenConns: 4, MaxConnLifetime: time.Hour},
		},
	}
}

func get(t *testing.T, a *App, target string) (*http.Response, string) {
	t.Helper()
	res, err := a.Handler().Test(httptest.NewRequest(http.MethodGet, target, nil))
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func TestNew_WiresProvidersAndRoutes(t *testing.T) {
	a, err := New(testConfig(t), fstest.MapFS{})
	require.NoError(t, err)

	res, body := get(t, a, "/api/v1/auth/config")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"providers":["google","github"]}`, body)

	res, _ = get(t, a, "/api/v1/auth/github/login")
	assert.Equal(t, http.StatusFound, res.StatusCode)
	assert.Contains(t, res.Header.Get("Location"), "redirect_uri=http%3A%2F%2Flocalhost%3A3000%2Fapi%2Fv1%2Fauth%2Fgithub%2Fcallback")

	res, _ = get(t, a, "/api/v1/users/me")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestNew_NoProvidersConfigured(t *testing.T) {
	cfg := testConfig(t)
	cfg.Auth.Google, cfg.Auth.GitHub = OAuthAppConfig{}, OAuthAppConfig{}
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	_, body := get(t, a, "/api/v1/auth/config")
	var got struct{ Providers []string }
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	assert.Empty(t, got.Providers)
}

func TestNew_Errors(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))
	garbage := filepath.Join(t.TempDir(), "garbage.db")
	require.NoError(t, os.WriteFile(garbage, []byte("this is definitely not a sqlite database file at all"), 0o600))

	cases := map[string]func(*Config){
		"missing secret": func(c *Config) { c.Auth.JWTSecret = "" },
		"missing dsn":    func(c *Config) { c.SQLite.DSN = "" },
		"postgres host":  func(c *Config) { c.Postgres = PostgresConfig{Enabled: true} },
		"postgres down": func(c *Config) {
			c.Postgres = PostgresConfig{Enabled: true, Host: "127.0.0.1", Port: 1, User: "x", DBName: "x", SSLMode: "disable"}
		},
		"bad settings file": func(c *Config) { c.App.SettingsFile = t.TempDir() },
		"mkdir fails":       func(c *Config) { c.SQLite.DSN = filepath.Join(notADir, "db", "x.db") },
		"open fails":        func(c *Config) { c.SQLite.DSN = t.TempDir() },
		"not a database":    func(c *Config) { c.SQLite.DSN = garbage },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			mutate(&cfg)
			_, err := New(cfg, fstest.MapFS{})
			assert.Error(t, err)
		})
	}
}

func TestListenAndShutdown(t *testing.T) {
	cfg := testConfig(t)
	cfg.App.Port = 0
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- a.Listen() }()
	require.Eventually(t, func() bool { return a.fiber.Server() != nil }, time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, a.Shutdown(ctx))
	assert.NoError(t, <-done)
}

func TestSelectDialector(t *testing.T) {
	cfg := testConfig(t)
	cfg.Postgres = PostgresConfig{Enabled: true, Host: "db", PoolConfig: PoolConfig{MaxOpenConns: 9}}
	d, pool, err := selectDialector(cfg)
	require.NoError(t, err)
	assert.Equal(t, "postgres", d.Name(), "postgres wins when both are enabled")
	assert.Equal(t, 9, pool.MaxOpenConns)

	cfg.Postgres.Enabled = false
	cfg.SQLite.DSN = "file::memory:"
	d, _, err = selectDialector(cfg)
	require.NoError(t, err)
	assert.Equal(t, "sqlite", d.Name())

	_, _, err = selectDialector(Config{})
	assert.ErrorContains(t, err, "no database enabled")
}

func TestRenewCredits(t *testing.T) {
	cfg := testConfig(t)
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	due := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, a.db.Create(&model.User{ID: 1, Email: "a@x", Credits: 3, CreditsRenewAt: due}).Error)
	a.renewCredits()

	var u model.User
	require.NoError(t, a.db.First(&u, 1).Error)
	assert.Equal(t, credit.DefaultMonthlyGrant, u.Credits)
	assert.True(t, due.Add(credit.Period).Equal(u.CreditsRenewAt))

	// A failing run is logged, not fatal.
	sqlDB, err := a.db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	a.renewCredits()
}

func TestPostgresDSN(t *testing.T) {
	assert.Equal(t, "host='db' TimeZone=UTC", postgresDSN(PostgresConfig{Host: "db"}))
	assert.Equal(t,
		`host='db' port=5432 user='u' password='p a\'s\\s' dbname='d' sslmode='require' TimeZone=UTC`,
		postgresDSN(PostgresConfig{Host: "db", Port: 5432, User: "u", Password: `p a's\s`, DBName: "d", SSLMode: "require"}))
}

func TestSetupFlow_SettingsApplyAndPersist(t *testing.T) {
	cfg := testConfig(t)
	cfg.Credits.MonthlyGrant = 0 // falls back to the default
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	login := httptest.NewRequest(http.MethodPost, "/api/v1/setup/login", strings.NewReader(`{"token":"root-token"}`))
	res, err := a.Handler().Test(login)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	root := res.Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/settings", nil)
	req.AddCookie(root)
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	var got struct {
		Settings struct {
			Credits  struct{ MonthlyGrant int }
			Database struct{ Driver string }
		}
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, 250, got.Settings.Credits.MonthlyGrant)
	assert.Equal(t, "sqlite", got.Settings.Database.Driver)

	body := `{"settings":{"credits":{"monthlyGrant":500},"database":{"driver":"sqlite","sqlite":{"dsn":"` + cfg.SQLite.DSN + `"}},
		"auth":{"github":{"clientId":"h"}}}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/setup/settings", strings.NewReader(body))
	req.AddCookie(root)
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	msg, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusOK, res.StatusCode, string(msg))

	// The grant applies to the running server immediately...
	require.NoError(t, a.db.Create(&model.User{ID: 1, Email: "a@x", CreditsRenewAt: time.Now().Add(-time.Hour)}).Error)
	a.renewCredits()
	var u model.User
	require.NoError(t, a.db.First(&u, 1).Error)
	assert.Equal(t, 500, u.Credits)

	// ...and saved settings override the config on the next start.
	b, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	_, cfgBody := get(t, b, "/api/v1/auth/config")
	assert.JSONEq(t, `{"providers":["github"]}`, cfgBody, "google cleared, github secret kept")
}

func TestSettingsFromConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.Credits.MonthlyGrant = 42
	cfg.Postgres = PostgresConfig{Enabled: true, Host: "h", Port: 1, User: "u", Password: "p", DBName: "d", SSLMode: "disable"}
	cfg.S3 = S3Config{Enabled: true, Endpoint: "e", Region: "r", Bucket: "b", AccessKeyID: "a", SecretAccessKey: "s", UsePathStyle: true}
	s := settingsFromConfig(cfg)
	assert.Equal(t, 42, s.Credits.MonthlyGrant)
	assert.Equal(t, "postgres", s.Database.Driver)

	round := applySettings(Config{}, s)
	assert.Equal(t, cfg.Postgres.Host, round.Postgres.Host)
	assert.True(t, round.Postgres.Enabled)
	assert.False(t, round.SQLite.Enabled)
	assert.Equal(t, cfg.S3, round.S3)
	assert.Equal(t, cfg.Auth.Google, round.Auth.Google)
	assert.Equal(t, 42, round.Credits.MonthlyGrant)
}

func TestRootAccessToken(t *testing.T) {
	assert.Equal(t, "set", rootAccessToken(Config{Auth: AuthConfig{RootAccessToken: "set"}}))
	a, b := rootAccessToken(Config{}), rootAccessToken(Config{})
	assert.Len(t, a, 48)
	assert.NotEqual(t, a, b)
}

func TestSettingsFile(t *testing.T) {
	assert.Equal(t, "./_storage/settings.json", settingsFile(Config{}))
	assert.Equal(t, "/x.json", settingsFile(Config{App: AppConfig{SettingsFile: "/x.json"}}))
}
