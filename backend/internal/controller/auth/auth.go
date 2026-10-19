// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package auth implements sign-in with third-party identity providers and
// session authentication.
package auth

//go:generate mockgen -source=auth.go -destination=mocks/auth.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
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
	// ErrProviderUnavailable is returned when the provider cannot start a
	// sign-in, e.g. its OpenID Connect discovery fails.
	ErrProviderUnavailable = errors.New("provider unavailable")
	// ErrUnauthorized is returned for a missing or invalid session.
	ErrUnauthorized = errors.New("unauthorized")
)

const (
	// AgentWeb is the agent of requests from a browser: the web app.
	AgentWeb = "Web"
	// AgentUnnamed is the agent of an OAuth client that registered
	// without a name.
	AgentUnnamed = "Unnamed app"
)

type (
	// Controller is the sign-in and session business logic.
	Controller interface {
		GetAuthConfig(ctx context.Context) *entity.GetAuthConfigResponse
		StartLogin(ctx context.Context, rq *entity.StartLoginRequest) (*entity.StartLoginResponse, error)
		CompleteLogin(ctx context.Context, rq *entity.CompleteLoginRequest) (*entity.CompleteLoginResponse, error)
		Authenticate(ctx context.Context, rq *entity.AuthenticateRequest) (*entity.AuthenticateResponse, error)
		// AuthenticateAnonymous identifies a visitor without a session by
		// IP address, only when the request comes from the app's web origin.
		AuthenticateAnonymous(ctx context.Context, rq *entity.AuthenticateAnonymousRequest) (*entity.AuthenticateResponse, error)
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
		// SignupGrant returns the current credit grant of new accounts.
		SignupGrant func() int
		// BasePath is the path prefix the app is served under ("" or "/x").
		BasePath string
		// BaseURL is the app's public URL; anonymous requests are accepted
		// from its origin.
		BaseURL string
		// AllowedOrigins returns the other origins anonymous requests are
		// accepted from. It may be nil.
		AllowedOrigins func() []string
		// MCPURL is the MCP endpoint's URL that clients are shown.
		MCPURL string
		Now    func() time.Time
	}

	controller struct {
		providers map[string]oauth.Provider
		order     []string
		token     token.Service
		users     repository.UserRepository
		idgen     idgen.Service
		grant     func() int
		signup    func() int
		basePath  string
		webOrigin string
		allowed   func() []string
		mcpURL    string
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
		signup:    credit.SignupGrantOrDefault(p.SignupGrant),
		basePath:  strings.TrimRight(p.BasePath, "/"),
		webOrigin: origin(p.BaseURL),
		allowed:   p.AllowedOrigins,
		mcpURL:    p.MCPURL,
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
	providers := make([]entity.AuthProvider, len(c.order))
	for i, name := range c.order {
		p := c.providers[name]
		providers[i] = entity.AuthProvider{ID: name, Name: p.Label(), Icon: p.Icon()}
	}
	return &entity.GetAuthConfigResponse{Providers: providers, MCPURL: c.mcpURL}
}

func (c *controller) StartLogin(ctx context.Context, rq *entity.StartLoginRequest) (*entity.StartLoginResponse, error) {
	provider, ok := c.providers[rq.Provider]
	if !ok {
		return nil, ErrUnknownProvider
	}
	state, err := c.token.CreateStateToken(provider.Name(), sanitizeRedirect(rq.RedirectURL))
	if err != nil {
		return nil, fmt.Errorf("create state: %w", err)
	}
	authURL, err := provider.AuthURL(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
	}
	return &entity.StartLoginResponse{AuthURL: authURL, State: state}, nil
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

// Authenticate accepts an OAuth access token or, without one, a browser
// session.
func (c *controller) Authenticate(_ context.Context, rq *entity.AuthenticateRequest) (*entity.AuthenticateResponse, error) {
	var (
		subject string
		agent   = AgentWeb
		err     error
	)
	switch {
	case rq.AccessToken != "":
		var grant token.OAuthGrant
		grant, err = c.token.ValidateAccessToken(rq.AccessToken)
		subject, agent = grant.UserID, grant.ClientName
		if agent == "" {
			agent = AgentUnnamed
		}
	case rq.SessionToken != "":
		subject, err = c.token.ValidateSessionToken(rq.SessionToken)
	default:
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, ErrUnauthorized
	}
	id := monoflake.IDFromBase62(subject).Int64()
	if id <= 0 {
		return nil, ErrUnauthorized
	}
	return &entity.AuthenticateResponse{UserID: id, Agent: agent}, nil
}

// AuthenticateAnonymous accepts a request from the app's web origin, by
// its Origin header or, without one, its Referer, as the anonymous user of
// its IP address. Non-browser clients could forge either header; this
// keeps anonymous use to the web app, while the per-IP limits bound it.
func (c *controller) AuthenticateAnonymous(_ context.Context, rq *entity.AuthenticateAnonymousRequest) (*entity.AuthenticateResponse, error) {
	from := rq.Origin
	if from == "" {
		from = origin(rq.Referer)
	}
	if !c.trustedOrigin(from) {
		return nil, ErrUnauthorized
	}
	id := idgen.AnonymousUserID(rq.IP)
	if id == 0 {
		return nil, ErrUnauthorized
	}
	return &entity.AuthenticateResponse{UserID: id, Anonymous: true, Agent: AgentWeb}, nil
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
		// New accounts start with the sign-up grant; the daily job resets
		// it to the monthly grant.
		user, err = c.users.CreateUser(ctx, model.User{
			ID:             c.idgen.NextID(),
			Email:          email,
			Name:           id.Name,
			Picture:        id.Picture,
			Provider:       provider,
			Credits:        c.signup(),
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

// origin is the scheme://host[:port] of an absolute URL, or "".
// trustedOrigin reports whether from is the app's own origin or an allowed
// one.
func (c *controller) trustedOrigin(from string) bool {
	if from == "" {
		return false
	}
	if strings.EqualFold(from, c.webOrigin) {
		return true
	}
	if c.allowed == nil {
		return false
	}
	return slices.ContainsFunc(c.allowed(), func(o string) bool { return strings.EqualFold(from, o) })
}

func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
