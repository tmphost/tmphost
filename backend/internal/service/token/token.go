// Package token issues and validates the signed JWTs used for browser
// sessions and for OAuth state.
package token

//go:generate mockgen -source=token.go -destination=mocks/token.go -package=mocks

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// SessionTTL is how long a browser session lasts.
	SessionTTL = 7 * 24 * time.Hour
	// StateTTL is how long a user has to finish an OAuth round trip.
	StateTTL = 5 * time.Minute
	// RootTTL is how long a root (setup) session lasts.
	RootTTL = 12 * time.Hour

	_sessionAudience = "session:human"
	_rootAudience    = "session:root"
	_rootSubject     = "root"
	_stateSubject    = "oauth_state"
)

// ErrInvalidToken is returned for any token that fails validation.
var ErrInvalidToken = errors.New("invalid token")

type (
	// Service creates and validates tokens.
	Service interface {
		// CreateSessionToken signs a session for the given user ID.
		CreateSessionToken(userID string) (string, error)
		// ValidateSessionToken returns the user ID a session belongs to.
		ValidateSessionToken(token string) (string, error)
		// CreateStateToken signs an OAuth state bound to a provider and the
		// in-app path to return to.
		CreateStateToken(provider, redirectURL string) (string, error)
		// ValidateStateToken returns the redirect path carried by a state
		// issued for provider.
		ValidateStateToken(token, provider string) (string, error)
		// CreateRootToken signs a root (setup) session.
		CreateRootToken() (string, error)
		// ValidateRootToken checks a root session.
		ValidateRootToken(token string) error
	}

	// Config holds the token signing settings.
	Config struct {
		Secret string
		Now    func() time.Time
	}

	service struct {
		secret []byte
		now    func() time.Time
	}

	stateClaims struct {
		jwt.RegisteredClaims
		RedirectURL string `json:"rurl,omitempty"`
	}
)

// New returns a token service. The secret must not be empty.
func New(cfg Config) (Service, error) {
	if cfg.Secret == "" {
		return nil, errors.New("token: secret is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{secret: []byte(cfg.Secret), now: now}, nil
}

func (s *service) CreateSessionToken(userID string) (string, error) {
	now := s.now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		Audience:  jwt.ClaimStrings{_sessionAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(SessionTTL)),
	}
	return s.sign(claims)
}

func (s *service) ValidateSessionToken(token string) (string, error) {
	var claims jwt.RegisteredClaims
	if err := s.parse(token, &claims, _sessionAudience); err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}

func (s *service) CreateStateToken(provider, redirectURL string) (string, error) {
	now := s.now()
	claims := stateClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   _stateSubject,
			Audience:  jwt.ClaimStrings{provider},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(StateTTL)),
		},
		RedirectURL: redirectURL,
	}
	return s.sign(claims)
}

func (s *service) ValidateStateToken(token, provider string) (string, error) {
	var claims stateClaims
	if err := s.parse(token, &claims, provider); err != nil {
		return "", err
	}
	if claims.Subject != _stateSubject {
		return "", ErrInvalidToken
	}
	return claims.RedirectURL, nil
}

func (s *service) CreateRootToken() (string, error) {
	now := s.now()
	return s.sign(jwt.RegisteredClaims{
		Subject:   _rootSubject,
		Audience:  jwt.ClaimStrings{_rootAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(RootTTL)),
	})
}

func (s *service) ValidateRootToken(token string) error {
	var claims jwt.RegisteredClaims
	if err := s.parse(token, &claims, _rootAudience); err != nil {
		return err
	}
	if claims.Subject != _rootSubject {
		return ErrInvalidToken
	}
	return nil
}

func (s *service) sign(claims jwt.Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func (s *service) parse(token string, claims jwt.Claims, audience string) error {
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return s.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return nil
}
