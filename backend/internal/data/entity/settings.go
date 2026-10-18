// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

type (
	// Settings are the server settings root can change from the setup page.
	Settings struct {
		Credits  CreditSettings
		Database DatabaseSettings
		Storage  StorageSettings
		Auth     AuthSettings
	}

	// CreditSettings configure the operation credit grant.
	CreditSettings struct {
		MonthlyGrant int
	}

	// DatabaseSettings choose and configure the database.
	DatabaseSettings struct {
		// Driver is "sqlite" or "postgres".
		Driver   string
		SQLite   SQLiteSettings
		Postgres PostgresSettings
	}

	// SQLiteSettings configure SQLite.
	SQLiteSettings struct {
		DSN string
	}

	// PostgresSettings configure PostgreSQL.
	PostgresSettings struct {
		// DSN is a postgres:// URL or a key=value connection string. It
		// usually holds the password, so it is a secret.
		DSN string
	}

	// StorageSettings configure object storage.
	StorageSettings struct {
		S3 S3Settings
	}

	// S3Settings configure an S3-compatible bucket.
	S3Settings struct {
		Enabled         bool
		Endpoint        string
		Region          string
		Bucket          string
		AccessKeyID     string
		SecretAccessKey string
		UsePathStyle    bool
	}

	// AuthSettings configure the sign-in providers.
	AuthSettings struct {
		Google OAuthClientSettings
		GitHub OAuthClientSettings
	}

	// OAuthClientSettings are an OAuth app registration.
	OAuthClientSettings struct {
		ClientID     string
		ClientSecret string
	}

	// SecretsSet reports which secrets have a value, since secrets are never
	// returned.
	SecretsSet struct {
		PostgresDSN        bool
		S3SecretAccessKey  bool
		GoogleClientSecret bool
		GitHubClientSecret bool
	}

	// RootLoginRequest carries a root access token.
	RootLoginRequest struct {
		Token string
	}

	// RootLoginResponse carries a root session.
	RootLoginResponse struct {
		SessionToken string
	}

	// AuthenticateRootRequest carries a root session token.
	AuthenticateRootRequest struct {
		SessionToken string
	}

	// GetSettingsResponse carries the settings with secrets removed.
	GetSettingsResponse struct {
		Settings   Settings
		SecretsSet SecretsSet
		// RestartRequired is true when saved settings differ from those the
		// server started with in a way that only a restart applies.
		RestartRequired bool
	}

	// UpdateSettingsRequest replaces the settings. Empty secrets keep their
	// current value.
	UpdateSettingsRequest struct {
		Settings Settings
	}

	// UpdateSettingsResponse is the saved state.
	UpdateSettingsResponse = GetSettingsResponse
)
