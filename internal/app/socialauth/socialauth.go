// Package socialauth is browser Sign in with Google and Sign in with Apple.
//
// The native apps authenticate with the provider on device and hand the
// backend a signed ID token, which internal/pkg/idtoken verifies and the auth
// service turns into a session. A browser cannot do that: it has to be sent to
// the provider and come back with an authorization code, and until now nothing
// in the backend owned that half. GOOGLE_CLIENT_ID was read at boot, the login
// screen rendered a Google button because of it, and the button led to a URL no
// route served.
//
// Both providers here end where generic OIDC ends, at a verified
// idtoken.Claims, so the auth service runs one federated login for all three:
// the same (issuer, subject) identity keying, the same just-in-time
// provisioning, the same ban and 2FA gates.
package socialauth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	apple "github.com/meszmate/apple-go"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
	"golang.org/x/oauth2"
	googleendpoint "golang.org/x/oauth2/google"
)

// Google is the browser authorization-code flow for Sign in with Google.
type Google struct {
	oauth    *oauth2.Config
	verifier *idtoken.Verifier
}

// NewGoogle builds the flow. Both halves of the client credential and a
// redirect URL are required: Google refuses the exchange without the secret and
// rejects the authorization request outright without a registered redirect, and
// either mistake would otherwise only surface as a failed sign-in.
func NewGoogle(clientID, clientSecret, redirectURL string) (*Google, error) {
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("socialauth: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are both required")
	}
	if redirectURL == "" {
		return nil, errors.New("socialauth: GOOGLE_REDIRECT_URI is required (or set API_PUBLIC_URL and let it derive)")
	}
	if !strings.HasPrefix(redirectURL, "http://") && !strings.HasPrefix(redirectURL, "https://") {
		return nil, fmt.Errorf("socialauth: GOOGLE_REDIRECT_URI %q is not an absolute URL", redirectURL)
	}

	return &Google{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			// The ID token carries everything a login needs, so no userinfo
			// call and no token to store: nothing here grants access to
			// anything in the person's Google account.
			Scopes:   []string{"openid", "email", "profile"},
			Endpoint: googleendpoint.Endpoint,
		},
		verifier: idtoken.GoogleVerifier(clientID),
	}, nil
}

func (g *Google) ProviderName() string { return "Google" }

// RedirectURL is the URI that has to be registered at the provider, surfaced so
// boot can log it: an operator who configured the client and got nothing has no
// other way to see what Warmbly is asking the provider to call back.
func (g *Google) RedirectURL() string { return g.oauth.RedirectURL }

// AuthCodeURL builds the authorization request. PKCE is mandatory for every
// client type under RFC 9700, including confidential ones, and the nonce is
// what binds the returned ID token to this attempt.
//
// prompt=select_account is deliberate: without it Google silently reuses
// whichever account the browser is already signed into, which on a shared
// machine signs the wrong person in with no way to notice.
func (g *Google) AuthCodeURL(state, nonce, verifier string) string {
	return g.oauth.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
}

// Exchange trades the authorization code for a verified identity.
func (g *Google) Exchange(ctx context.Context, code, verifier, expectedNonce string) (*idtoken.Claims, error) {
	tok, err := g.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, fmt.Errorf("socialauth: google code exchange: %w", err)
	}

	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, errors.New("socialauth: google token response carried no id_token")
	}

	claims, err := g.verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("socialauth: verifying google id_token: %w", err)
	}
	if err := checkIdentity(claims, expectedNonce); err != nil {
		return nil, err
	}
	// The verifier already collapses Google's two issuer spellings onto the
	// https form, which is what the identity is keyed on.
	return claims, nil
}

// Apple is the browser authorization-code flow for Sign in with Apple.
//
// Apple only puts the email claim in the ID token when the email scope is
// requested, and requiring a scope forces response_mode=form_post, so its
// callback arrives as a cross-site POST rather than a redirect with a query
// string. That is the one structural difference from Google here.
type Apple struct {
	client      apple.AppleAuth
	servicesID  string
	redirectURL string
	verifier    *idtoken.Verifier
}

// NewApple builds the flow from the same credentials the native path uses. The
// client id is the Services ID (the web identifier), not the app's bundle ID.
func NewApple(client apple.AppleAuth, servicesID, redirectURL string) (*Apple, error) {
	if client == nil {
		return nil, errors.New("socialauth: apple client is not configured")
	}
	if servicesID == "" {
		return nil, errors.New("socialauth: APPLE_APP_ID is required")
	}
	if redirectURL == "" {
		return nil, errors.New("socialauth: APPLE_REDIRECT_URI is required (or set API_PUBLIC_URL and let it derive)")
	}
	// Apple rejects a plain-http redirect URI outright, so a deployment that
	// would never work is better refused at boot than at the first sign-in.
	if !strings.HasPrefix(redirectURL, "https://") {
		return nil, fmt.Errorf("socialauth: Apple requires an https redirect URI, got %q", redirectURL)
	}

	return &Apple{
		client:      client,
		servicesID:  servicesID,
		redirectURL: redirectURL,
		verifier:    idtoken.AppleVerifier(servicesID),
	}, nil
}

func (a *Apple) ProviderName() string { return "Apple" }
func (a *Apple) RedirectURL() string  { return a.redirectURL }

// AuthCodeURL builds the authorization request. Apple does not support PKCE on
// the web flow, so the verifier is ignored; one-time state and the nonce inside
// the ID token are what bind the response to this attempt.
func (a *Apple) AuthCodeURL(state, nonce, _ string) string {
	return apple.AuthorizeURL(apple.AuthorizeURLConfig{
		ClientID:     a.servicesID,
		RedirectURI:  a.redirectURL,
		State:        state,
		Nonce:        nonce,
		Scope:        []string{"name", "email"},
		ResponseType: apple.ResponseTypeCode,
		ResponseMode: apple.ResponseModeFormPost,
	})
}

// Exchange trades the authorization code for a verified identity. The ID token
// comes straight off Apple's token endpoint, but it is still verified against
// Apple's published keys: a token nobody checked is a token nobody can trust,
// and the audience check is what stops one issued for a different client.
func (a *Apple) Exchange(ctx context.Context, code, _, expectedNonce string) (*idtoken.Claims, error) {
	resp, err := a.client.ValidateCodeWithRedirectURI(code, a.redirectURL)
	if err != nil {
		if errors.Is(err, apple.ErrorResponseInvalidGrant) {
			return nil, fmt.Errorf("socialauth: apple rejected the authorization code: %w", err)
		}
		return nil, fmt.Errorf("socialauth: apple code exchange: %w", err)
	}
	if resp == nil || resp.IDToken == "" {
		return nil, errors.New("socialauth: apple token response carried no id_token")
	}

	claims, err := a.verifier.Verify(ctx, resp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("socialauth: verifying apple id_token: %w", err)
	}
	if err := checkIdentity(claims, expectedNonce); err != nil {
		return nil, err
	}
	return claims, nil
}

// checkIdentity is the part neither provider does for us: the nonce proves the
// token was minted for this authorization request rather than replayed from
// another one, and an unverified address is how federated login turns into
// account takeover at any issuer where addresses are self-asserted.
func checkIdentity(claims *idtoken.Claims, expectedNonce string) error {
	if expectedNonce == "" || claims.Nonce != expectedNonce {
		return errors.New("socialauth: id_token nonce does not match this authorization request")
	}
	if claims.Subject == "" {
		return errors.New("socialauth: id_token carried no subject")
	}
	if claims.Email == "" {
		return errors.New("socialauth: id_token carried no email claim")
	}
	if !claims.EmailVerified {
		return errors.New("socialauth: the provider did not report this email address as verified")
	}
	return nil
}
