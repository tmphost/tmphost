package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func runBody(t *testing.T, body string, fn func(c *fiber.Ctx)) {
	t.Helper()
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error {
		fn(c)
		return nil
	})
	_, err := app.Test(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)))
	require.NoError(t, err)
}

func TestFromHTTPRequestToRootLoginRequestEntity(t *testing.T) {
	cases := map[string]*entity.RootLoginRequest{
		`{"token":"t"}`: {Token: "t"},
		`{"token":""}`:  nil,
		`not json`:      nil,
	}
	for body, want := range cases {
		var got *entity.RootLoginRequest
		runBody(t, body, func(c *fiber.Ctx) { got = FromHTTPRequestToRootLoginRequestEntity(c) })
		assert.Equal(t, want, got, body)
	}
}

func TestFromHTTPRequestToAuthenticateRootRequestEntity(t *testing.T) {
	var got *entity.AuthenticateRootRequest
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: RootCookie, Value: "r"})
	run(t, "/", req, func(c *fiber.Ctx) { got = FromHTTPRequestToAuthenticateRootRequestEntity(c) })
	assert.Equal(t, &entity.AuthenticateRootRequest{SessionToken: "r"}, got)

	run(t, "/", httptest.NewRequest(http.MethodGet, "/", nil), func(c *fiber.Ctx) { got = FromHTTPRequestToAuthenticateRootRequestEntity(c) })
	assert.Nil(t, got)
}

const _settingsJSON = `{
	"credits": {"monthlyGrant": 300},
	"database": {"driver": "postgres", "sqlite": {"dsn": "x.db"},
		"postgres": {"dsn": "postgres://u:p@h/d"}},
	"storage": {"s3": {"enabled": true, "endpoint": "https://s3", "region": "r", "bucket": "b",
		"accessKeyId": "a", "secretAccessKey": "s", "usePathStyle": true}},
	"auth": {"google": {"clientId": "g", "clientSecret": "gs"}, "github": {"clientId": "h", "clientSecret": "hs"}}
}`

func TestSettingsMapping(t *testing.T) {
	var got *entity.UpdateSettingsRequest
	runBody(t, `{"settings":`+_settingsJSON+`}`, func(c *fiber.Ctx) { got = FromHTTPRequestToUpdateSettingsRequestEntity(c) })
	require.NotNil(t, got)
	assert.Equal(t, 300, got.Settings.Credits.MonthlyGrant)
	assert.Equal(t, "postgres://u:p@h/d", got.Settings.Database.Postgres.DSN)
	assert.Equal(t, "hs", got.Settings.Auth.GitHub.ClientSecret)

	runBody(t, `nope`, func(c *fiber.Ctx) { got = FromHTTPRequestToUpdateSettingsRequestEntity(c) })
	assert.Nil(t, got)

	// Entity -> view -> entity is lossless.
	settings := FromSettingsViewToSettingsEntity(FromSettingsEntityToSettingsView(entity.Settings{
		Database: entity.DatabaseSettings{Postgres: entity.PostgresSettings{DSN: "host=db"}},
		Storage:  entity.StorageSettings{S3: entity.S3Settings{UsePathStyle: true}},
	}))
	assert.Equal(t, "host=db", settings.Database.Postgres.DSN)
	assert.True(t, settings.Storage.S3.UsePathStyle)
}

func TestFromSettingsResponseEntityToHTTPResponse(t *testing.T) {
	rs := &entity.GetSettingsResponse{
		Settings:        FromSettingsViewToSettingsEntity(FromSettingsEntityToSettingsView(entity.Settings{Credits: entity.CreditSettings{MonthlyGrant: 7}})),
		SecretsSet:      entity.SecretsSet{PostgresDSN: true, S3SecretAccessKey: true, GoogleClientSecret: true, GitHubClientSecret: true},
		RestartRequired: true,
	}
	body := string(FromUpdateSettingsResponseEntityToHTTPResponse(rs))
	assert.Contains(t, body, `"monthlyGrant":7`)
	assert.Contains(t, body, `"secretsSet":{"postgresDsn":true,"s3SecretAccessKey":true,"googleClientSecret":true,"githubClientSecret":true}`)
	assert.Contains(t, body, `"restartRequired":true`)
}
