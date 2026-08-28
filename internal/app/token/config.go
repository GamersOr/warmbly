package token

import "time"

const (
	SessionTTL           = 12 * time.Hour
	RefreshTokenTTL      = 12 * time.Hour
	AccessTokenLifeTime  = 12 * time.Hour
	RefreshTokenLifeTime = 180 * 24 * time.Hour

	AuthProviderEmail    = "email"
	AuthProviderApple    = "apple"
	AuthProviderGoogle   = "google"
	AuthProviderWebAuthn = "webauthn"
	// AuthProviderOIDC covers generic single sign-on. Google and Apple keep
	// their own values, so the security page can name what was actually used.
	AuthProviderOIDC = "oidc"
)
