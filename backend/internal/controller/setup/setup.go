// Package setup implements the root-only setup page: root sign-in with the
// root access token, and reading and changing the server settings.
package setup

//go:generate mockgen -source=setup.go -destination=mocks/setup.go -package=mocks

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

const (
	// DriverSQLite selects SQLite.
	DriverSQLite = "sqlite"
	// DriverPostgres selects PostgreSQL.
	DriverPostgres = "postgres"

	_maxMonthlyGrant = 1_000_000
)

var (
	// ErrInvalidRootToken is returned for a wrong root access token.
	ErrInvalidRootToken = errors.New("invalid root access token")
	// ErrUnauthorized is returned for a missing or invalid root session.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrInvalidSettings wraps a settings validation failure.
	ErrInvalidSettings = errors.New("invalid settings")
)

type (
	// Controller is the setup page business logic.
	Controller interface {
		Login(ctx context.Context, rq *entity.RootLoginRequest) (*entity.RootLoginResponse, error)
		Authenticate(ctx context.Context, rq *entity.AuthenticateRootRequest) error
		GetSettings(ctx context.Context) *entity.GetSettingsResponse
		UpdateSettings(ctx context.Context, rq *entity.UpdateSettingsRequest) (*entity.UpdateSettingsResponse, error)
		// Current returns the effective settings, secrets included. It is for
		// the server itself and never reaches the API.
		Current() entity.Settings
		// MonthlyGrant returns the current monthly credit grant.
		MonthlyGrant() int
	}

	// Params are the controller's dependencies.
	Params struct {
		Repository repository.SettingsRepository
		Token      token.Service
		// RootAccessToken is the token that unlocks the setup page.
		RootAccessToken string
		// Defaults are the settings from the config file and environment,
		// used until settings are first saved.
		Defaults entity.Settings
	}

	controller struct {
		repo      repository.SettingsRepository
		token     token.Service
		rootHash  [sha256.Size]byte
		mu        sync.RWMutex
		current   entity.Settings
		startedAt entity.Settings
	}
)

// New loads the saved settings, falling back to the defaults.
func New(p Params) (Controller, error) {
	if p.RootAccessToken == "" {
		return nil, errors.New("setup: root access token is required")
	}
	current := p.Defaults
	saved, err := p.Repository.LoadSettings()
	switch {
	case err == nil:
		current = mappermodel.FromSettingsModelToSettingsEntity(saved)
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}
	return &controller{
		repo:      p.Repository,
		token:     p.Token,
		rootHash:  sha256.Sum256([]byte(p.RootAccessToken)),
		current:   current,
		startedAt: current,
	}, nil
}

func (c *controller) Login(_ context.Context, rq *entity.RootLoginRequest) (*entity.RootLoginResponse, error) {
	// Compare fixed-size digests in constant time so neither the token nor
	// its length leaks through timing.
	got := sha256.Sum256([]byte(rq.Token))
	if subtle.ConstantTimeCompare(got[:], c.rootHash[:]) != 1 {
		return nil, ErrInvalidRootToken
	}
	session, err := c.token.CreateRootToken()
	if err != nil {
		return nil, fmt.Errorf("create root session: %w", err)
	}
	return &entity.RootLoginResponse{SessionToken: session}, nil
}

func (c *controller) Authenticate(_ context.Context, rq *entity.AuthenticateRootRequest) error {
	if rq.SessionToken == "" || c.token.ValidateRootToken(rq.SessionToken) != nil {
		return ErrUnauthorized
	}
	return nil
}

func (c *controller) GetSettings(_ context.Context) *entity.GetSettingsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.response()
}

func (c *controller) UpdateSettings(_ context.Context, rq *entity.UpdateSettingsRequest) (*entity.UpdateSettingsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	next := normalize(rq.Settings)
	keepSecrets(&next, c.current)
	if err := validate(next); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
	}
	if err := c.repo.SaveSettings(mappermodel.FromSettingsEntityToSettingsModel(next)); err != nil {
		return nil, fmt.Errorf("save settings: %w", err)
	}
	c.current = next
	return c.response(), nil
}

func (c *controller) Current() entity.Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

func (c *controller) MonthlyGrant() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Credits.MonthlyGrant
}

// response must be called with c.mu held.
func (c *controller) response() *entity.GetSettingsResponse {
	public := c.current
	public.Database.Postgres.DSN = ""
	public.Storage.S3.SecretAccessKey = ""
	public.Auth.Google.ClientSecret = ""
	public.Auth.GitHub.ClientSecret = ""
	return &entity.GetSettingsResponse{
		Settings: public,
		SecretsSet: entity.SecretsSet{
			PostgresDSN:        c.current.Database.Postgres.DSN != "",
			S3SecretAccessKey:  c.current.Storage.S3.SecretAccessKey != "",
			GoogleClientSecret: c.current.Auth.Google.ClientSecret != "",
			GitHubClientSecret: c.current.Auth.GitHub.ClientSecret != "",
		},
		RestartRequired: restartRequired(c.startedAt, c.current),
	}
}

// restartRequired reports whether anything but the live-applied credit
// settings changed since the server started.
func restartRequired(started, current entity.Settings) bool {
	started.Credits, current.Credits = entity.CreditSettings{}, entity.CreditSettings{}
	return started != current
}

// keepSecrets keeps the current value of every secret left blank. Clearing
// a provider's client ID disables it, so its secret goes too.
func keepSecrets(next *entity.Settings, current entity.Settings) {
	keep := func(dst *string, cur string) {
		if *dst == "" {
			*dst = cur
		}
	}
	keep(&next.Database.Postgres.DSN, current.Database.Postgres.DSN)
	keep(&next.Storage.S3.SecretAccessKey, current.Storage.S3.SecretAccessKey)
	for _, p := range []struct {
		next *entity.OAuthClientSettings
		cur  entity.OAuthClientSettings
	}{{&next.Auth.Google, current.Auth.Google}, {&next.Auth.GitHub, current.Auth.GitHub}} {
		if p.next.ClientID == "" {
			p.next.ClientSecret = ""
			continue
		}
		keep(&p.next.ClientSecret, p.cur.ClientSecret)
	}
}

func normalize(s entity.Settings) entity.Settings {
	t := strings.TrimSpace
	s.Database.Driver = strings.ToLower(t(s.Database.Driver))
	s.Database.SQLite.DSN = t(s.Database.SQLite.DSN)
	s.Database.Postgres.DSN = t(s.Database.Postgres.DSN)
	s3 := &s.Storage.S3
	s3.Endpoint, s3.Region, s3.Bucket, s3.AccessKeyID = t(s3.Endpoint), t(s3.Region), t(s3.Bucket), t(s3.AccessKeyID)
	s.Auth.Google.ClientID = t(s.Auth.Google.ClientID)
	s.Auth.GitHub.ClientID = t(s.Auth.GitHub.ClientID)
	return s
}

func validate(s entity.Settings) error {
	if s.Credits.MonthlyGrant < 0 || s.Credits.MonthlyGrant > _maxMonthlyGrant {
		return fmt.Errorf("monthly credits must be between 0 and %d", _maxMonthlyGrant)
	}
	switch s.Database.Driver {
	case DriverSQLite:
		if s.Database.SQLite.DSN == "" {
			return errors.New("sqlite DSN is required")
		}
	case DriverPostgres:
		if s.Database.Postgres.DSN == "" {
			return errors.New("postgres DSN is required")
		}
		// Parsing only; it never connects. The error is not echoed because
		// it can quote the DSN, password included.
		if _, err := pgconn.ParseConfig(s.Database.Postgres.DSN); err != nil {
			return errors.New("postgres DSN is not a valid postgres:// URL or key=value connection string")
		}
	default:
		return fmt.Errorf("database driver must be %q or %q", DriverSQLite, DriverPostgres)
	}
	if s3 := s.Storage.S3; s3.Enabled {
		if s3.Bucket == "" || s3.Region == "" {
			return errors.New("s3 bucket and region are required")
		}
		if s3.Endpoint != "" {
			u, err := url.Parse(s3.Endpoint)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("s3 endpoint must be an http(s) URL")
			}
		}
	}
	if (s.Auth.Google.ClientID == "") != (s.Auth.Google.ClientSecret == "") {
		return errors.New("google needs both client ID and secret, or neither")
	}
	if (s.Auth.GitHub.ClientID == "") != (s.Auth.GitHub.ClientSecret == "") {
		return errors.New("github needs both client ID and secret, or neither")
	}
	return nil
}
