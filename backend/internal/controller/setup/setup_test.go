package setup

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	mocktoken "github.com/tmphost/tmphost/backend/internal/service/token/mocks"
)

var _errBoom = errors.New("boom")

func defaults() entity.Settings {
	return entity.Settings{
		Credits: entity.CreditSettings{MonthlyGrant: 250},
		Database: entity.DatabaseSettings{
			Driver:   DriverSQLite,
			SQLite:   entity.SQLiteSettings{DSN: "./_storage/tmphost.db"},
			Postgres: entity.PostgresSettings{Host: "localhost", Port: 5432, User: "postgres", Password: "pgpass", DBName: "tmphost", SSLMode: "disable"},
		},
		Storage: entity.StorageSettings{S3: entity.S3Settings{SecretAccessKey: "s3secret"}},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: "g", ClientSecret: "gsecret"},
		},
	}
}

type env struct {
	ctrl  Controller
	repo  *mockrepo.MockSettingsRepository
	token *mocktoken.MockService
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{repo: mockrepo.NewMockSettingsRepository(mc), token: mocktoken.NewMockService(mc)}
	e.repo.EXPECT().LoadSettings().Return(model.Settings{}, repository.ErrNotFound)
	ctrl, err := New(Params{Repository: e.repo, Token: e.token, RootAccessToken: "root-token", Defaults: defaults()})
	require.NoError(t, err)
	e.ctrl = ctrl
	return e
}

func TestNew(t *testing.T) {
	mc := gomock.NewController(t)

	t.Run("requires a root token", func(t *testing.T) {
		_, err := New(Params{})
		assert.Error(t, err)
	})

	t.Run("saved settings win over defaults", func(t *testing.T) {
		repo := mockrepo.NewMockSettingsRepository(mc)
		saved := defaults()
		saved.Credits.MonthlyGrant = 500
		repo.EXPECT().LoadSettings().Return(mappermodel.FromSettingsEntityToSettingsModel(saved), nil)
		ctrl, err := New(Params{Repository: repo, RootAccessToken: "x", Defaults: defaults()})
		require.NoError(t, err)
		assert.Equal(t, 500, ctrl.MonthlyGrant())
		assert.Equal(t, saved, ctrl.Current())
	})

	t.Run("load error", func(t *testing.T) {
		repo := mockrepo.NewMockSettingsRepository(mc)
		repo.EXPECT().LoadSettings().Return(model.Settings{}, _errBoom)
		_, err := New(Params{Repository: repo, RootAccessToken: "x"})
		assert.ErrorIs(t, err, _errBoom)
	})
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("wrong token", func(t *testing.T) {
		e := newEnv(t)
		for _, tok := range []string{"", "root", "root-token-but-longer"} {
			_, err := e.ctrl.Login(ctx, &entity.RootLoginRequest{Token: tok})
			assert.ErrorIs(t, err, ErrInvalidRootToken, tok)
		}
	})

	t.Run("ok", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateRootToken().Return("root-session", nil)
		rs, err := e.ctrl.Login(ctx, &entity.RootLoginRequest{Token: "root-token"})
		require.NoError(t, err)
		assert.Equal(t, "root-session", rs.SessionToken)
	})

	t.Run("session error", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateRootToken().Return("", _errBoom)
		_, err := e.ctrl.Login(ctx, &entity.RootLoginRequest{Token: "root-token"})
		assert.ErrorIs(t, err, _errBoom)
	})
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	assert.ErrorIs(t, e.ctrl.Authenticate(ctx, &entity.AuthenticateRootRequest{}), ErrUnauthorized)

	e.token.EXPECT().ValidateRootToken("bad").Return(_errBoom)
	assert.ErrorIs(t, e.ctrl.Authenticate(ctx, &entity.AuthenticateRootRequest{SessionToken: "bad"}), ErrUnauthorized)

	e.token.EXPECT().ValidateRootToken("good").Return(nil)
	assert.NoError(t, e.ctrl.Authenticate(ctx, &entity.AuthenticateRootRequest{SessionToken: "good"}))
}

func TestGetSettings_HidesSecrets(t *testing.T) {
	e := newEnv(t)
	rs := e.ctrl.GetSettings(context.Background())
	assert.Empty(t, rs.Settings.Database.Postgres.Password)
	assert.Empty(t, rs.Settings.Storage.S3.SecretAccessKey)
	assert.Empty(t, rs.Settings.Auth.Google.ClientSecret)
	assert.Equal(t, entity.SecretsSet{PostgresPassword: true, S3SecretAccessKey: true, GoogleClientSecret: true}, rs.SecretsSet)
	assert.Equal(t, "g", rs.Settings.Auth.Google.ClientID)
	assert.False(t, rs.RestartRequired)
}

func TestUpdateSettings_CreditsApplyLive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	next := e.ctrl.GetSettings(ctx).Settings // secrets blank: kept
	next.Credits.MonthlyGrant = 400

	e.repo.EXPECT().SaveSettings(gomock.Any()).DoAndReturn(func(m model.Settings) error {
		assert.Equal(t, 400, m.Credits.MonthlyGrant)
		assert.Equal(t, "pgpass", m.Database.Postgres.Password)
		assert.Equal(t, "s3secret", m.Storage.S3.SecretAccessKey)
		assert.Equal(t, "gsecret", m.Auth.Google.ClientSecret)
		return nil
	})
	rs, err := e.ctrl.UpdateSettings(ctx, &entity.UpdateSettingsRequest{Settings: next})
	require.NoError(t, err)
	assert.False(t, rs.RestartRequired, "credits apply without a restart")
	assert.Equal(t, 400, e.ctrl.MonthlyGrant())
	assert.Empty(t, rs.Settings.Auth.Google.ClientSecret)
}

func TestUpdateSettings_RestartRequired(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	next := e.ctrl.GetSettings(ctx).Settings
	next.Database.Driver = " Postgres "
	next.Database.Postgres.Password = "newpass"
	next.Auth.Google.ClientID = "" // disables Google, dropping its secret
	next.Auth.GitHub = entity.OAuthClientSettings{ClientID: " h ", ClientSecret: "hsecret"}

	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
	rs, err := e.ctrl.UpdateSettings(ctx, &entity.UpdateSettingsRequest{Settings: next})
	require.NoError(t, err)
	assert.True(t, rs.RestartRequired)

	cur := e.ctrl.Current()
	assert.Equal(t, DriverPostgres, cur.Database.Driver)
	assert.Equal(t, "newpass", cur.Database.Postgres.Password)
	assert.Equal(t, entity.OAuthClientSettings{}, cur.Auth.Google)
	assert.Equal(t, entity.OAuthClientSettings{ClientID: "h", ClientSecret: "hsecret"}, cur.Auth.GitHub)
}

func TestUpdateSettings_SaveFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	next := e.ctrl.GetSettings(ctx).Settings
	next.Credits.MonthlyGrant = 1
	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(_errBoom)
	_, err := e.ctrl.UpdateSettings(ctx, &entity.UpdateSettingsRequest{Settings: next})
	assert.ErrorIs(t, err, _errBoom)
	assert.Equal(t, 250, e.ctrl.MonthlyGrant(), "unchanged on failure")
}

func TestUpdateSettings_Validation(t *testing.T) {
	cases := map[string]func(*entity.Settings){
		"negative credits":   func(s *entity.Settings) { s.Credits.MonthlyGrant = -1 },
		"huge credits":       func(s *entity.Settings) { s.Credits.MonthlyGrant = _maxMonthlyGrant + 1 },
		"unknown driver":     func(s *entity.Settings) { s.Database.Driver = "mysql" },
		"sqlite without dsn": func(s *entity.Settings) { s.Database.SQLite.DSN = " " },
		"postgres no host": func(s *entity.Settings) {
			s.Database.Driver = DriverPostgres
			s.Database.Postgres.Host = ""
		},
		"postgres bad port": func(s *entity.Settings) {
			s.Database.Driver = DriverPostgres
			s.Database.Postgres.Port = 70000
		},
		"postgres bad sslmode": func(s *entity.Settings) {
			s.Database.Driver = DriverPostgres
			s.Database.Postgres.SSLMode = "sometimes"
		},
		"s3 without bucket": func(s *entity.Settings) {
			s.Storage.S3 = entity.S3Settings{Enabled: true, Region: "r"}
		},
		"s3 bad endpoint": func(s *entity.Settings) {
			s.Storage.S3 = entity.S3Settings{Enabled: true, Region: "r", Bucket: "b", Endpoint: "ftp://x"}
		},
		"s3 unparsable endpoint": func(s *entity.Settings) {
			s.Storage.S3 = entity.S3Settings{Enabled: true, Region: "r", Bucket: "b", Endpoint: "http://[::1"}
		},
		"github id without secret": func(s *entity.Settings) { s.Auth.GitHub = entity.OAuthClientSettings{ClientID: "h"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			next := defaults()
			mutate(&next)
			_, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
			assert.ErrorIs(t, err, ErrInvalidSettings)
		})
	}
}

func TestUpdateSettings_ProviderIDWithoutAnySecret(t *testing.T) {
	// Nothing stored to keep, and no new secret: rejected.
	e := newEnvWith(t, func(s *entity.Settings) { s.Auth.Google = entity.OAuthClientSettings{} })
	next := defaults()
	next.Auth.Google = entity.OAuthClientSettings{ClientID: "g2"}
	_, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
	assert.ErrorIs(t, err, ErrInvalidSettings)
}

func TestUpdateSettings_ValidS3(t *testing.T) {
	e := newEnv(t)
	next := defaults()
	next.Storage.S3 = entity.S3Settings{Enabled: true, Region: "auto", Bucket: "b", Endpoint: "https://r2.example.com"}
	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
	rs, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
	require.NoError(t, err)
	assert.True(t, rs.SecretsSet.S3SecretAccessKey, "kept from current")
}

func newEnvWith(t *testing.T, mutate func(*entity.Settings)) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{repo: mockrepo.NewMockSettingsRepository(mc), token: mocktoken.NewMockService(mc)}
	d := defaults()
	mutate(&d)
	e.repo.EXPECT().LoadSettings().Return(model.Settings{}, repository.ErrNotFound)
	ctrl, err := New(Params{Repository: e.repo, Token: e.token, RootAccessToken: "root-token", Defaults: d})
	require.NoError(t, err)
	e.ctrl = ctrl
	return e
}
