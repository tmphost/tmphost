package view

type (
	// RootLoginRequest is the root sign-in body.
	RootLoginRequest struct {
		Token string `json:"token"`
	}

	// Settings are the setup page settings. Secrets are write-only: they are
	// always empty in responses (see SecretsSet), and an empty secret in a
	// request keeps the stored value.
	Settings struct {
		Domain   DomainSettings   `json:"domain"`
		TLS      TLSSettings      `json:"tls"`
		Credits  CreditSettings   `json:"credits"`
		Database DatabaseSettings `json:"database"`
		Storage  StorageSettings  `json:"storage"`
		Auth     AuthSettings     `json:"auth"`
	}

	// DomainSettings is the domain section.
	DomainSettings struct {
		AppURL      string `json:"appUrl"`
		SitesDomain string `json:"sitesDomain"`
	}

	// TLSSettings is the tls section.
	TLSSettings struct {
		Enabled     bool         `json:"enabled"`
		Port        int          `json:"port"`
		Certificate string       `json:"certificate"`
		PrivateKey  string       `json:"privateKey"`
		Lego        LegoSettings `json:"lego"`
	}

	// LegoSettings is the tls.lego section.
	LegoSettings struct {
		Enabled            bool   `json:"enabled"`
		Email              string `json:"email"`
		CloudflareAPIToken string `json:"cloudflareApiToken"`
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
		Google    OAuthClientSettings     `json:"google"`
		GitHub    OAuthClientSettings     `json:"github"`
		Providers []OAuthProviderSettings `json:"providers"`
	}

	// OAuthProviderSettings is a custom OpenID Connect provider.
	OAuthProviderSettings struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		AuthURL      string `json:"authUrl"`
	}

	// OAuthClientSettings is an OAuth app registration.
	OAuthClientSettings struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}

	// SecretsSet reports which secrets have a stored value.
	SecretsSet struct {
		PostgresDSN           bool            `json:"postgresDsn"`
		S3SecretAccessKey     bool            `json:"s3SecretAccessKey"`
		GoogleClientSecret    bool            `json:"googleClientSecret"`
		GitHubClientSecret    bool            `json:"githubClientSecret"`
		TLSPrivateKey         bool            `json:"tlsPrivateKey"`
		CloudflareAPIToken    bool            `json:"cloudflareApiToken"`
		ProviderClientSecrets map[string]bool `json:"providerClientSecrets"`
	}

	// SettingsRequest is the PUT /setup/settings body.
	SettingsRequest struct {
		Settings Settings `json:"settings"`
	}

	// SettingsResponse is the GET and PUT /setup/settings response.
	SettingsResponse struct {
		Settings        Settings   `json:"settings"`
		SecretsSet      SecretsSet `json:"secretsSet"`
		RestartRequired bool       `json:"restartRequired"`
	}
)
