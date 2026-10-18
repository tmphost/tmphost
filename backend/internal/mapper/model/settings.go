package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// FromSettingsModelToSettingsEntity maps stored settings to the domain.
func FromSettingsModelToSettingsEntity(m model.Settings) entity.Settings {
	return entity.Settings{
		Credits: entity.CreditSettings{MonthlyGrant: m.Credits.MonthlyGrant},
		Database: entity.DatabaseSettings{
			Driver: m.Database.Driver,
			SQLite: entity.SQLiteSettings{DSN: m.Database.SQLite.DSN},
			Postgres: entity.PostgresSettings{
				Host:     m.Database.Postgres.Host,
				Port:     m.Database.Postgres.Port,
				User:     m.Database.Postgres.User,
				Password: m.Database.Postgres.Password,
				DBName:   m.Database.Postgres.DBName,
				SSLMode:  m.Database.Postgres.SSLMode,
			},
		},
		Storage: entity.StorageSettings{S3: entity.S3Settings{
			Enabled:         m.Storage.S3.Enabled,
			Endpoint:        m.Storage.S3.Endpoint,
			Region:          m.Storage.S3.Region,
			Bucket:          m.Storage.S3.Bucket,
			AccessKeyID:     m.Storage.S3.AccessKeyID,
			SecretAccessKey: m.Storage.S3.SecretAccessKey,
			UsePathStyle:    m.Storage.S3.UsePathStyle,
		}},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: m.Auth.Google.ClientID, ClientSecret: m.Auth.Google.ClientSecret},
			GitHub: entity.OAuthClientSettings{ClientID: m.Auth.GitHub.ClientID, ClientSecret: m.Auth.GitHub.ClientSecret},
		},
	}
}

// FromSettingsEntityToSettingsModel maps domain settings to storage.
func FromSettingsEntityToSettingsModel(e entity.Settings) model.Settings {
	return model.Settings{
		Credits: model.CreditSettings{MonthlyGrant: e.Credits.MonthlyGrant},
		Database: model.DatabaseSettings{
			Driver: e.Database.Driver,
			SQLite: model.SQLiteSettings{DSN: e.Database.SQLite.DSN},
			Postgres: model.PostgresSettings{
				Host:     e.Database.Postgres.Host,
				Port:     e.Database.Postgres.Port,
				User:     e.Database.Postgres.User,
				Password: e.Database.Postgres.Password,
				DBName:   e.Database.Postgres.DBName,
				SSLMode:  e.Database.Postgres.SSLMode,
			},
		},
		Storage: model.StorageSettings{S3: model.S3Settings{
			Enabled:         e.Storage.S3.Enabled,
			Endpoint:        e.Storage.S3.Endpoint,
			Region:          e.Storage.S3.Region,
			Bucket:          e.Storage.S3.Bucket,
			AccessKeyID:     e.Storage.S3.AccessKeyID,
			SecretAccessKey: e.Storage.S3.SecretAccessKey,
			UsePathStyle:    e.Storage.S3.UsePathStyle,
		}},
		Auth: model.AuthSettings{
			Google: model.OAuthClientSettings{ClientID: e.Auth.Google.ClientID, ClientSecret: e.Auth.Google.ClientSecret},
			GitHub: model.OAuthClientSettings{ClientID: e.Auth.GitHub.ClientID, ClientSecret: e.Auth.GitHub.ClientSecret},
		},
	}
}
