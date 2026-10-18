package model

type (
	// EncryptedSettings is the settings file: the JSON-encoded Settings,
	// sealed with the master key.
	EncryptedSettings struct {
		Version   int    `json:"version"`
		Algorithm string `json:"algorithm"`
		// Data is base64 of the nonce followed by the ciphertext.
		Data string `json:"data"`
	}

	// Settings is the plaintext (JSON) form of the setup page settings.
	Settings struct {
		Credits  CreditSettings   `json:"credits"`
		Database DatabaseSettings `json:"database"`
		Storage  StorageSettings  `json:"storage"`
		Auth     AuthSettings     `json:"auth"`
	}

	// CreditSettings is the credits section.
	CreditSettings struct {
		MonthlyGrant int `json:"monthlyGrant"`
	}

	// DatabaseSettings is the database section.
	DatabaseSettings struct {
		Driver   string           `json:"driver"`
		SQLite   SQLiteSettings   `json:"sqlite"`
		Postgres PostgresSettings `json:"postgres"`
	}

	// SQLiteSettings is the database.sqlite section.
	SQLiteSettings struct {
		DSN string `json:"dsn"`
	}

	// PostgresSettings is the database.postgres section.
	PostgresSettings struct {
		DSN string `json:"dsn"`
	}

	// StorageSettings is the storage section.
	StorageSettings struct {
		S3 S3Settings `json:"s3"`
	}

	// S3Settings is the storage.s3 section.
	S3Settings struct {
		Enabled         bool   `json:"enabled"`
		Endpoint        string `json:"endpoint"`
		Region          string `json:"region"`
		Bucket          string `json:"bucket"`
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		UsePathStyle    bool   `json:"usePathStyle"`
	}

	// AuthSettings is the auth section.
	AuthSettings struct {
		Google OAuthClientSettings `json:"google"`
		GitHub OAuthClientSettings `json:"github"`
	}

	// OAuthClientSettings is an OAuth app registration.
	OAuthClientSettings struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
)
