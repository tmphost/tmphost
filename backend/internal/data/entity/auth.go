package entity

type (
	// GetAuthConfigResponse lists the sign-in providers that are configured.
	GetAuthConfigResponse struct {
		Providers []AuthProvider
	}

	// AuthProvider is a sign-in button.
	AuthProvider struct {
		// ID is the provider's route name.
		ID   string
		Name string
		// Icon is SVG markup; empty for the built-in providers.
		Icon string
	}

	// StartLoginRequest begins an OAuth round trip.
	StartLoginRequest struct {
		Provider    string
		RedirectURL string
	}

	// StartLoginResponse carries the consent URL and the state to pin in a
	// cookie for the callback.
	StartLoginResponse struct {
		AuthURL string
		State   string
	}

	// CompleteLoginRequest finishes an OAuth round trip.
	CompleteLoginRequest struct {
		Provider string
		Code     string
		// State is the state echoed back by the provider.
		State string
		// StateCookie is the state pinned in the browser at login start.
		StateCookie string
	}

	// CompleteLoginResponse carries the new session and where to send the
	// browser next.
	CompleteLoginResponse struct {
		SessionToken string
		RedirectURL  string
		User         User
	}

	// AuthenticateRequest carries a session token.
	AuthenticateRequest struct {
		SessionToken string
	}

	// AuthenticateResponse identifies the session's user.
	AuthenticateResponse struct {
		UserID int64
	}
)
