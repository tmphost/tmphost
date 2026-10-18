package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

// RootCookie holds the root (setup) session token.
const RootCookie = "tmphost_root"

// FromHTTPRequestToRootLoginRequestEntity reads POST /setup/login.
func FromHTTPRequestToRootLoginRequestEntity(c *fiber.Ctx) *entity.RootLoginRequest {
	var rq view.RootLoginRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil || rq.Token == "" {
		return nil
	}
	return &entity.RootLoginRequest{Token: rq.Token}
}

// FromHTTPRequestToAuthenticateRootRequestEntity reads the root cookie.
func FromHTTPRequestToAuthenticateRootRequestEntity(c *fiber.Ctx) *entity.AuthenticateRootRequest {
	tok := c.Cookies(RootCookie)
	if tok == "" {
		return nil
	}
	return &entity.AuthenticateRootRequest{SessionToken: tok}
}

// FromHTTPRequestToUpdateSettingsRequestEntity reads PUT /setup/settings.
func FromHTTPRequestToUpdateSettingsRequestEntity(c *fiber.Ctx) *entity.UpdateSettingsRequest {
	var rq view.SettingsRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil {
		return nil
	}
	return &entity.UpdateSettingsRequest{Settings: FromSettingsViewToSettingsEntity(rq.Settings)}
}

// FromGetSettingsResponseEntityToHTTPResponse renders GET /setup/settings.
func FromGetSettingsResponseEntityToHTTPResponse(rs *entity.GetSettingsResponse) []byte {
	return marshal(view.SettingsResponse{
		Settings: FromSettingsEntityToSettingsView(rs.Settings),
		SecretsSet: view.SecretsSet{
			PostgresPassword:   rs.SecretsSet.PostgresPassword,
			S3SecretAccessKey:  rs.SecretsSet.S3SecretAccessKey,
			GoogleClientSecret: rs.SecretsSet.GoogleClientSecret,
			GitHubClientSecret: rs.SecretsSet.GitHubClientSecret,
		},
		RestartRequired: rs.RestartRequired,
	})
}

// FromUpdateSettingsResponseEntityToHTTPResponse renders PUT /setup/settings.
func FromUpdateSettingsResponseEntityToHTTPResponse(rs *entity.UpdateSettingsResponse) []byte {
	return FromGetSettingsResponseEntityToHTTPResponse(rs)
}

// FromSettingsViewToSettingsEntity maps request settings to the domain.
func FromSettingsViewToSettingsEntity(v view.Settings) entity.Settings {
	return entity.Settings{
		Credits: entity.CreditSettings{MonthlyGrant: v.Credits.MonthlyGrant},
		Database: entity.DatabaseSettings{
			Driver: v.Database.Driver,
			SQLite: entity.SQLiteSettings{DSN: v.Database.SQLite.DSN},
			Postgres: entity.PostgresSettings{
				Host:     v.Database.Postgres.Host,
				Port:     v.Database.Postgres.Port,
				User:     v.Database.Postgres.User,
				Password: v.Database.Postgres.Password,
				DBName:   v.Database.Postgres.DBName,
				SSLMode:  v.Database.Postgres.SSLMode,
			},
		},
		Storage: entity.StorageSettings{S3: entity.S3Settings{
			Enabled:         v.Storage.S3.Enabled,
			Endpoint:        v.Storage.S3.Endpoint,
			Region:          v.Storage.S3.Region,
			Bucket:          v.Storage.S3.Bucket,
			AccessKeyID:     v.Storage.S3.AccessKeyID,
			SecretAccessKey: v.Storage.S3.SecretAccessKey,
			UsePathStyle:    v.Storage.S3.UsePathStyle,
		}},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: v.Auth.Google.ClientID, ClientSecret: v.Auth.Google.ClientSecret},
			GitHub: entity.OAuthClientSettings{ClientID: v.Auth.GitHub.ClientID, ClientSecret: v.Auth.GitHub.ClientSecret},
		},
	}
}

// FromSettingsEntityToSettingsView maps domain settings to the response.
func FromSettingsEntityToSettingsView(e entity.Settings) view.Settings {
	return view.Settings{
		Credits: view.CreditSettings{MonthlyGrant: e.Credits.MonthlyGrant},
		Database: view.DatabaseSettings{
			Driver: e.Database.Driver,
			SQLite: view.SQLiteSettings{DSN: e.Database.SQLite.DSN},
			Postgres: view.PostgresSettings{
				Host:     e.Database.Postgres.Host,
				Port:     e.Database.Postgres.Port,
				User:     e.Database.Postgres.User,
				Password: e.Database.Postgres.Password,
				DBName:   e.Database.Postgres.DBName,
				SSLMode:  e.Database.Postgres.SSLMode,
			},
		},
		Storage: view.StorageSettings{S3: view.S3Settings{
			Enabled:         e.Storage.S3.Enabled,
			Endpoint:        e.Storage.S3.Endpoint,
			Region:          e.Storage.S3.Region,
			Bucket:          e.Storage.S3.Bucket,
			AccessKeyID:     e.Storage.S3.AccessKeyID,
			SecretAccessKey: e.Storage.S3.SecretAccessKey,
			UsePathStyle:    e.Storage.S3.UsePathStyle,
		}},
		Auth: view.AuthSettings{
			Google: view.OAuthClientSettings{ClientID: e.Auth.Google.ClientID, ClientSecret: e.Auth.Google.ClientSecret},
			GitHub: view.OAuthClientSettings{ClientID: e.Auth.GitHub.ClientID, ClientSecret: e.Auth.GitHub.ClientSecret},
		},
	}
}
