// Package auth implements sign-in with third-party identity providers and
// session authentication.
package auth

//go:generate mockgen -source=auth.go -destination=mocks/auth.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/oauth"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

var (
	// ErrUnknownProvider is returned for a provider that is not configured.
	ErrUnknownProvider = errors.New("unknown provider")
	// ErrInvalidState is returned when the OAuth state is missing, forged,
	// expired or not the one pinned in the browser.
	ErrInvalidState = errors.New("invalid oauth state")
	// ErrEmailNotVerified is returned when the provider has no verified
	// email for the user.
	ErrEmailNotVerified = errors.New("email not verified")
	// ErrProviderExchange is returned when the provider rejects the code.
	ErrProviderExchange = errors.New("provider exchange failed")
	// ErrUnauthorized is returned for a missing or invalid session.
	ErrUnauthorized = errors.New("unauthorized")
)

type (
	// Controller is the sign-in and session business logic.
	Controller interface {
		GetAuthConfig(ctx context.Context) *entity.GetAuthConfigResponse
		StartLogin(ctx context.Context, rq *entity.StartLoginRequest) (*entity.StartLoginResponse, error)
		CompleteLogin(ctx context.Context, rq *entity.CompleteLoginRequest) (*entity.CompleteLoginResponse, error)
		Authenticate(ctx context.Context, rq *entity.AuthenticateRequest) (*entity.AuthenticateResponse, error)
		GetCurrentUser(ctx context.Context, rq *entity.GetCurrentUserRequest) (*entity.GetCurrentUserResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Providers []oauth.Provider
		Token     token.Service
		Users     repository.UserRepository
		IDGen     idgen.Service
		// Grant returns the current monthly credit grant.
		Grant func() int
		// BasePath is the path prefix the app is served under ("" or "/x").
		BasePath string
		Now      func() time.Time
	}

	controller struct {
		providers map[string]oauth.Provider
		order     []string
		token     token.Service
		users     repository.UserRepository
		idgen     idgen.Service
		grant     func() int
		basePath  string
		now       func() time.Time
	}
)

// New returns the auth controller.
func New(p Params) Controller {
	c := &controller{
		providers: make(map[string]oauth.Provider, len(p.Providers)),
		token:     p.Token,
		users:     p.Users,
		idgen:     p.IDGen,
		grant:     credit.GrantOrDefault(p.Grant),
		basePath:  strings.TrimRight(p.BasePath, "/"),
		now:       p.Now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	for _, provider := range p.Providers {
		c.providers[provider.Name()] = provider
		c.order = append(c.order, provider.Name())
	}
	return c
}

func (c *controller) GetAuthConfig(_ context.Context) *entity.GetAuthConfigResponse {
	return &entity.GetAuthConfigResponse{Providers: append([]string{}, c.order...)}
}

func (c *controller) StartLogin(_ context.Context, rq *entity.StartLoginRequest) (*entity.StartLoginResponse, error) {
	provider, ok := c.providers[rq.Provider]
	if !ok {
		return nil, ErrUnknownProvider
	}
	state, err := c.token.CreateStateToken(provider.Name(), sanitizeRedirect(rq.RedirectURL))
	if err != nil {
		return nil, fmt.Errorf("create state: %w", err)
	}
	return &entity.StartLoginResponse{AuthURL: provider.AuthURL(state), State: state}, nil
}

func (c *controller) CompleteLogin(ctx context.Context, rq *entity.CompleteLoginRequest) (*entity.CompleteLoginResponse, error) {
	provider, ok := c.providers[rq.Provider]
	if !ok {
		return nil, ErrUnknownProvider
	}
	// The state must be signed for this provider AND match the copy pinned
	// in the browser at login start; otherwise an attacker could log a
	// victim into the attacker's account (login CSRF).
	if rq.State == "" || rq.State != rq.StateCookie {
		return nil, ErrInvalidState
	}
	redirect, err := c.token.ValidateStateToken(rq.State, provider.Name())
	if err != nil {
		return nil, ErrInvalidState
	}

	identity, err := provider.Exchange(ctx, rq.Code)
	if errors.Is(err, oauth.ErrNoVerifiedEmail) {
		return nil, ErrEmailNotVerified
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderExchange, err)
	}

	user, err := c.findOrCreateUser(ctx, provider.Name(), identity)
	if err != nil {
		return nil, err
	}
	session, err := c.token.CreateSessionToken(monoflake.ID(user.ID).String())
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	return &entity.CompleteLoginResponse{
		SessionToken: session,
		RedirectURL:  c.basePath + sanitizeRedirect(redirect),
		User:         mappermodel.FromUserModelToUserEntity(user, c.grant()),
	}, nil
}

func (c *controller) Authenticate(_ context.Context, rq *entity.AuthenticateRequest) (*entity.AuthenticateResponse, error) {
	if rq.SessionToken == "" {
		return nil, ErrUnauthorized
	}
	subject, err := c.token.ValidateSessionToken(rq.SessionToken)
	if err != nil {
		return nil, ErrUnauthorized
	}
	id := monoflake.IDFromBase62(subject).Int64()
	if id <= 0 {
		return nil, ErrUnauthorized
	}
	return &entity.AuthenticateResponse{UserID: id}, nil
}

func (c *controller) GetCurrentUser(ctx context.Context, rq *entity.GetCurrentUserRequest) (*entity.GetCurrentUserResponse, error) {
	user, err := c.users.FindUserByID(ctx, rq.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &entity.GetCurrentUserResponse{User: mappermodel.FromUserModelToUserEntity(user, c.grant())}, nil
}

func (c *controller) findOrCreateUser(ctx context.Context, provider string, id *oauth.Identity) (model.User, error) {
	email := strings.ToLower(strings.TrimSpace(id.Email))
	user, err := c.users.FindUserByEmail(ctx, email)
	if errors.Is(err, repository.ErrNotFound) {
		// New accounts start with the full grant; the daily job renews it.
		user, err = c.users.CreateUser(ctx, model.User{
			ID:             c.idgen.NextID(),
			Email:          email,
			Name:           id.Name,
			Picture:        id.Picture,
			Provider:       provider,
			Credits:        c.grant(),
			CreditsRenewAt: credit.FirstRenewal(c.now()),
		})
		if err != nil {
			return model.User{}, fmt.Errorf("create user: %w", err)
		}
		return user, nil
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}

	changed := false
	if user.Name == "" && id.Name != "" {
		user.Name, changed = id.Name, true
	}
	if user.Picture == "" && id.Picture != "" {
		user.Picture, changed = id.Picture, true
	}
	if !changed {
		return user, nil
	}
	if user, err = c.users.UpdateUser(ctx, user); err != nil {
		return model.User{}, fmt.Errorf("update user: %w", err)
	}
	return user, nil
}

// sanitizeRedirect only allows in-app absolute paths, so the login flow
// can never be turned into an open redirect.
func sanitizeRedirect(path string) string {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") || strings.HasPrefix(path, "/\\") {
		return "/"
	}
	return path
}
