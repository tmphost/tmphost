package model

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func TestSettingsRoundTrip(t *testing.T) {
	e := entity.Settings{
		Credits: entity.CreditSettings{MonthlyGrant: 300},
		Database: entity.DatabaseSettings{
			Driver:   "postgres",
			SQLite:   entity.SQLiteSettings{DSN: "x.db"},
			Postgres: entity.PostgresSettings{Host: "h", Port: 5432, User: "u", Password: "p", DBName: "d", SSLMode: "require"},
		},
		Storage: entity.StorageSettings{S3: entity.S3Settings{
			Enabled: true, Endpoint: "https://s3", Region: "r", Bucket: "b", AccessKeyID: "a", SecretAccessKey: "s", UsePathStyle: true,
		}},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: "g", ClientSecret: "gs"},
			GitHub: entity.OAuthClientSettings{ClientID: "h", ClientSecret: "hs"},
		},
	}
	m := FromSettingsEntityToSettingsModel(e)
	assert.Equal(t, "s", m.Storage.S3.SecretAccessKey)
	assert.Equal(t, e, FromSettingsModelToSettingsEntity(m))
}
