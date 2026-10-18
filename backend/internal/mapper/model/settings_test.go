// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

func TestSettingsRoundTrip(t *testing.T) {
	e := entity.Settings{
		Domain: entity.DomainSettings{AppURL: "https://app.tmphost.io", SitesDomain: "tmphost.io"},
		TLS: entity.TLSSettings{
			Enabled: true, Port: 443, Email: "ops@tmphost.io",
			Lego: entity.LegoSettings{Enabled: true, CloudflareAPIToken: "cf"},
		},
		Credits: entity.CreditSettings{MonthlyGrant: 300},
		Database: entity.DatabaseSettings{
			Driver:   "postgres",
			SQLite:   entity.SQLiteSettings{DSN: "x.db"},
			Postgres: entity.PostgresSettings{DSN: "postgres://u:p@h:5432/d?sslmode=require"},
		},
		Storage: entity.StorageSettings{S3: entity.S3Settings{
			Enabled: true, Endpoint: "https://s3", Region: "r", Bucket: "b", AccessKeyID: "a", SecretAccessKey: "s", UsePathStyle: true,
		}},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: "g", ClientSecret: "gs"},
			GitHub: entity.OAuthClientSettings{ClientID: "h", ClientSecret: "hs"},
			Providers: []entity.OAuthProviderSettings{{
				ID: "okta", Name: "Okta", Icon: "<svg/>", ClientID: "o", ClientSecret: "os", AuthURL: "https://okta.example.com",
			}},
		},
	}
	m := FromSettingsEntityToSettingsModel(e)
	assert.Equal(t, "s", m.Storage.S3.SecretAccessKey)
	assert.Equal(t, e, FromSettingsModelToSettingsEntity(m))
}

func TestSettingsRoundTrip_NoProviders(t *testing.T) {
	m := FromSettingsEntityToSettingsModel(entity.Settings{})
	assert.Equal(t, []model.OAuthProviderSettings{}, m.Auth.Providers, "saved as [] rather than null")
	assert.Nil(t, FromSettingsModelToSettingsEntity(m).Auth.Providers)
}
