package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func envFor(t *testing.T, port int) map[string]string {
	t.Helper()
	return map[string]string{
		"PORT":                    strconv.Itoa(port),
		"TMPHOST_AUTH_JWT_SECRET": "secret",
		"TMPHOST_SQLITE_DSN":      filepath.Join(t.TempDir(), "tmphost.db"),
		"TMPHOST_SETTINGS_FILE":   filepath.Join(t.TempDir(), "settings.json"),
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	raw, err := bundled.ReadFile("_config/base.yaml")
	require.NoError(t, err)
	cfg, err := loadConfig(raw, func(string) string { return "" })
	require.NoError(t, err)
	assert.Equal(t, 3000, cfg.App.Port)
	assert.Equal(t, "http://localhost:3000", cfg.App.BaseURL)
	assert.False(t, cfg.App.CookieSecure)
	assert.True(t, cfg.SQLite.Enabled)
	assert.Contains(t, cfg.SQLite.DSN, "./_storage/tmphost.db?")
	assert.Equal(t, time.Hour, cfg.SQLite.MaxConnLifetime)
	assert.False(t, cfg.Postgres.Enabled)
	assert.Equal(t, "localhost", cfg.Postgres.Host)
	assert.Equal(t, 5432, cfg.Postgres.Port)
	assert.Equal(t, "disable", cfg.Postgres.SSLMode)
	assert.Equal(t, 250, cfg.Credits.MonthlyGrant)
	assert.False(t, cfg.S3.Enabled)
	assert.Equal(t, "./_storage/settings.json", cfg.App.SettingsFile)
	assert.Equal(t, "", cfg.Auth.Google.ClientID)
}

func TestLoadConfig_Errors(t *testing.T) {
	_, err := loadConfig([]byte("app: [unclosed"), func(string) string { return "" })
	assert.Error(t, err)

	raw, _ := bundled.ReadFile("_config/base.yaml")
	_, err = loadConfig(raw, func(k string) string {
		if k == "PORT" {
			return "not-a-port"
		}
		return ""
	})
	assert.Error(t, err)
}

func TestRun_ServesUntilCancelled(t *testing.T) {
	port := freePort(t)
	env := envFor(t, port)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }) }()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/api/v1/auth/config"
	require.Eventually(t, func() bool {
		res, err := http.Get(url)
		if err != nil {
			return false
		}
		_ = res.Body.Close()
		return res.StatusCode == http.StatusOK
	}, 5*time.Second, 20*time.Millisecond)

	cancel()
	assert.NoError(t, <-done)
}

func TestRun_Errors(t *testing.T) {
	t.Run("bad config", func(t *testing.T) {
		assert.Error(t, run(context.Background(), func(k string) string {
			if k == "PORT" {
				return "x"
			}
			return ""
		}))
	})

	t.Run("app fails", func(t *testing.T) {
		assert.Error(t, run(context.Background(), func(string) string { return "" }), "missing JWT secret")
	})

	t.Run("listen fails", func(t *testing.T) {
		l, err := net.Listen("tcp", ":0")
		require.NoError(t, err)
		defer l.Close()
		env := envFor(t, l.Addr().(*net.TCPAddr).Port)
		assert.Error(t, run(context.Background(), func(k string) string { return env[k] }))
	})
}

func TestMain_StopsOnSignal(t *testing.T) {
	port := freePort(t)
	for k, v := range envFor(t, port) {
		t.Setenv(k, v)
	}
	var fatalErr []any
	fatal = func(v ...any) { fatalErr = v }
	t.Cleanup(func() { fatal = log.Fatal })

	done := make(chan struct{})
	go func() { main(); close(done) }()
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 5*time.Second, 20*time.Millisecond)

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))
	<-done
	assert.Nil(t, fatalErr)
}

func TestMain_Fatal(t *testing.T) {
	t.Setenv("TMPHOST_AUTH_JWT_SECRET", "")
	var fatalErr []any
	fatal = func(v ...any) { fatalErr = v }
	t.Cleanup(func() { fatal = log.Fatal })
	main()
	assert.NotEmpty(t, fatalErr)
}
