package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/mail"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/app/token"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/idtoken"
)

// ssoStateTTL bounds how long an in-flight authorization may take. Short enough
// that a leaked state is useless, long enough for a real person to type a
// password and approve an MFA prompt at their provider.
const ssoStateTTL = 10 * time.Minute

// FederatedProvider is one browser sign-in flow: generic OIDC, Sign in with
// Google or Sign in with Apple. All three hand back the same verified claims,
// so the service runs one login path for them rather than three.
//
// Satisfied by *oidcauth.Service, *socialauth.Google and *socialauth.Apple; an
// interface here so the auth package imports none of them and tests can stub it.
type FederatedProvider interface {
	AuthCodeURL(state, nonce, verifier string) string
	Exchange(ctx context.Context, code, verifier, expectedNonce string) (*idtoken.Claims, error)
	// ProviderName is the label its button carries. It is what makes
	// OIDC_PROVIDER_NAME reach the login screen, so a deployment behind
	// Authentik or Keycloak says so instead of "single sign-on".
	ProviderName() string
}

// SSORedirect is what the client sends the browser to.
type SSORedirect struct {
	URL string `json:"url"`
}

// SSOCallback is one provider callback: everything the flow needs that is not
// already held server-side under the state.
//
// FirstName and LastName exist for Apple, which shares the person's name once,
// with the callback, and never inside the ID token.
type SSOCallback struct {
	Provider  string
	Code      string
	State     string
	FirstName string
	LastName  string
	IPAddress string
	UserAgent string
}

// ssoFlow is the server-side half of one authorization request. Keeping the
// verifier and nonce here, keyed by state, is what makes them single-use: RFC
// 9700 requires PKCE and a one-time state, and an ID token nonce only proves
// anything if the value it is compared against was never reused.
//
// Provider is stored with them so a state minted for one provider cannot be
// presented at another provider's callback.
type ssoFlow struct {
	Provider string `json:"provider"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
}

func ssoStateKey(state string) string { return "oidc_state:" + state }

// ssoHandoffTTL is how long the dashboard has to exchange the handoff code for
// the real session. Seconds, not minutes: the redirect and the exchange happen
// back to back.
const ssoHandoffTTL = 60 * time.Second

func ssoHandoffKey(code string) string { return "oidc_handoff:" + code }

// WireFederatedProvider attaches one browser sign-in provider under its
// identity-provider key ("oidc", "google", "apple"). A nil provider is ignored,
// so an unconfigured one simply stays unavailable.
func (s *authService) WireFederatedProvider(name string, p FederatedProvider) {
	if p == nil || name == "" {
		return
	}
	if s.providers == nil {
		s.providers = map[string]FederatedProvider{}
	}
	s.providers[name] = p
}

// federatedProviderOrder is the order the login screen renders the buttons in.
var federatedProviderOrder = []string{
	models.IdentityProviderGoogle,
	models.IdentityProviderApple,
	models.IdentityProviderOIDC,
}

// FederatedProviders lists the configured providers, so /auth/config can
// advertise exactly the buttons this deployment can actually complete.
func (s *authService) FederatedProviders() []string {
	out := make([]string, 0, len(s.providers))
	for _, name := range federatedProviderOrder {
		if _, ok := s.providers[name]; ok {
			out = append(out, name)
		}
	}
	return out
}

// FederatedProviderLabels is what each button should say.
func (s *authService) FederatedProviderLabels() map[string]string {
	if len(s.providers) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.providers))
	for _, name := range federatedProviderOrder {
		if p, ok := s.providers[name]; ok && p.ProviderName() != "" {
			out[name] = p.ProviderName()
		}
	}
	return out
}

// mintHandoff stores a completed login behind a single-use code.
//
// The provider redirects a browser to the callback, so that response cannot be
// JSON: it has to be a redirect the user can follow. Putting tokens in the URL
// would leak them into history, Referer and any proxy log, so the redirect
// carries an opaque code and the dashboard exchanges it over POST.
func (s *authService) mintHandoff(ctx context.Context, result *models.LoginResult) (string, *errx.Error) {
	code, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	payload, err := json.Marshal(result)
	if err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	if err := s.cache.SetEx(ctx, ssoHandoffKey(code), payload, ssoHandoffTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	return code, nil
}

// SSOExchange swaps a handoff code for the session it stands for. Single use:
// the code is deleted as it is read.
func (s *authService) SSOExchange(ctx context.Context, code string) (*models.LoginResult, *errx.Error) {
	if code == "" {
		return nil, errx.ErrToken
	}
	raw, err := s.cache.GetDel(ctx, ssoHandoffKey(code)).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, errx.ErrToken
	}
	var result models.LoginResult
	if err := json.Unmarshal(raw, &result); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	return &result, nil
}

// SSOBegin starts an authorization request against one provider.
func (s *authService) SSOBegin(ctx context.Context, provider string) (*SSORedirect, *errx.Error) {
	p, ok := s.providers[provider]
	if !ok || p == nil {
		return nil, errx.ErrExternalProvider
	}

	state, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	nonce, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	verifier, err := randomHex(32)
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	payload, err := json.Marshal(ssoFlow{Provider: provider, Verifier: verifier, Nonce: nonce})
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}
	if err := s.cache.SetEx(ctx, ssoStateKey(state), payload, ssoStateTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return &SSORedirect{URL: p.AuthCodeURL(state, nonce, verifier)}, nil
}

// SSOCallbackComplete finishes an authorization and returns a single-use
// handoff code the dashboard exchanges for the session.
func (s *authService) SSOCallbackComplete(ctx context.Context, in SSOCallback) (string, *errx.Error) {
	p, ok := s.providers[in.Provider]
	if !ok || p == nil {
		return "", errx.ErrExternalProvider
	}
	if in.Code == "" || in.State == "" {
		return "", errx.ErrExternalCode
	}

	// Consume the state before doing anything with it. A replayed callback
	// finds nothing and is rejected, which is the whole point of one-time
	// state.
	raw, cerr := s.cache.GetDel(ctx, ssoStateKey(in.State)).Bytes()
	if cerr != nil || len(raw) == 0 {
		return "", errx.ErrExternalCode
	}

	var flow ssoFlow
	if err := json.Unmarshal(raw, &flow); err != nil {
		sentry.CaptureException(err)
		return "", errx.InternalError()
	}
	// A state is minted for one provider. Presenting it at another provider's
	// callback is a mix-up attack (RFC 9700 4.4), not a real sign-in.
	if flow.Provider != "" && flow.Provider != in.Provider {
		return "", errx.ErrExternalCode
	}

	claims, err := p.Exchange(ctx, in.Code, flow.Verifier, flow.Nonce)
	if err != nil {
		// Provider-side failures are user-visible configuration problems more
		// often than attacks, so they are worth reporting rather than burying.
		sentry.CaptureException(err)
		return "", errx.ErrExternalCode
	}

	email, perr := mail.ParseAddress(claims.Email)
	if perr != nil {
		return "", errx.ErrExternalEmail
	}

	firstName, lastName := claims.GivenName, claims.FamilyName
	if firstName == "" {
		firstName = in.FirstName
	}
	if lastName == "" {
		lastName = in.LastName
	}

	userID, rerr := s.resolveFederatedUser(
		ctx,
		in.Provider,
		claims.Issuer,
		claims.Subject,
		email,
		firstName,
		lastName,
	)
	if rerr != nil {
		return "", rerr
	}

	result, lerr := s.finishLoginAs(ctx, userID, in.IPAddress, in.UserAgent, sessionProvider(in.Provider))
	if lerr != nil {
		return "", lerr
	}
	return s.mintHandoff(ctx, result)
}

// sessionProvider is how the session records what the person signed in with,
// shown on the account security page.
func sessionProvider(provider string) string {
	switch provider {
	case models.IdentityProviderGoogle:
		return token.AuthProviderGoogle
	case models.IdentityProviderApple:
		return token.AuthProviderApple
	default:
		return token.AuthProviderOIDC
	}
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
