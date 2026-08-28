package handler

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/app/auth"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// Browser sign-in: generic OIDC, Sign in with Google, Sign in with Apple.
//
// All three run the same three steps. Begin returns the provider's
// authorization URL rather than issuing a 302, because the dashboard is a
// single-page app on a different origin from the API and navigates itself. The
// provider then sends the browser to the callback, which answers with a
// redirect (a person is looking at that response) carrying a single-use handoff
// code, and the dashboard exchanges the code for the session over POST. No
// token ever appears in a URL, browser history, Referer header or proxy log.

// OIDCBegin starts a generic OpenID Connect authorization.
func (h *Handler) OIDCBegin(c *gin.Context) {
	h.ssoBegin(c, models.IdentityProviderOIDC)
}

// GoogleBegin starts a Sign in with Google authorization.
func (h *Handler) GoogleBegin(c *gin.Context) {
	h.ssoBegin(c, models.IdentityProviderGoogle)
}

// AppleBegin starts a Sign in with Apple authorization.
func (h *Handler) AppleBegin(c *gin.Context) {
	h.ssoBegin(c, models.IdentityProviderApple)
}

func (h *Handler) ssoBegin(c *gin.Context, provider string) {
	redirect, err := h.AuthService.SSOBegin(c.Request.Context(), provider)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, redirect)
}

// OIDCCallback is where an OpenID Connect provider sends the browser back.
func (h *Handler) OIDCCallback(c *gin.Context) {
	h.ssoCallback(c, auth.SSOCallback{
		Provider: models.IdentityProviderOIDC,
		Code:     c.Query("code"),
		State:    c.Query("state"),
	})
}

// GoogleCallback is where Google sends the browser back.
func (h *Handler) GoogleCallback(c *gin.Context) {
	h.ssoCallback(c, auth.SSOCallback{
		Provider: models.IdentityProviderGoogle,
		Code:     c.Query("code"),
		State:    c.Query("state"),
	})
}

// appleCallbackUser is the one-time name payload Apple posts alongside the
// code. Apple shares a person's name exactly once, on their first
// authorization, and never inside the ID token, so a first sign-in that drops
// it leaves an account named after its email local part forever.
type appleCallbackUser struct {
	Name struct {
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	} `json:"name"`
}

// AppleCallback is where Apple sends the browser back.
//
// Requesting any scope forces response_mode=form_post, so this arrives as a
// cross-site POST with a form body rather than a redirect with a query string.
// Nothing here reads a cookie, which is what lets that work: the state is held
// server-side and the form is the only thing that has to survive the hop.
func (h *Handler) AppleCallback(c *gin.Context) {
	in := auth.SSOCallback{
		Provider: models.IdentityProviderApple,
		Code:     formOrQuery(c, "code"),
		State:    formOrQuery(c, "state"),
	}
	if raw := formOrQuery(c, "user"); raw != "" {
		var u appleCallbackUser
		if err := json.Unmarshal([]byte(raw), &u); err == nil {
			in.FirstName, in.LastName = u.Name.FirstName, u.Name.LastName
		}
	}
	h.ssoCallback(c, in)
}

func (h *Handler) ssoCallback(c *gin.Context, in auth.SSOCallback) {
	base := config.AppBaseURL()

	// A provider-side refusal arrives as error=, not as a failed exchange.
	// Someone who closed the consent screen is not an error worth shouting
	// about, so it goes back to the login form quietly.
	if e := formOrQuery(c, "error"); e != "" {
		if e == "access_denied" || e == "user_cancelled_authorize" {
			c.Redirect(http.StatusFound, base+"/auth/login")
			return
		}
		c.Redirect(http.StatusFound, base+"/auth/login?sso_error="+url.QueryEscape(e))
		return
	}

	in.IPAddress = c.ClientIP()
	in.UserAgent = c.Request.UserAgent()

	handoff, err := h.AuthService.SSOCallbackComplete(c.Request.Context(), in)
	if err != nil {
		c.Redirect(http.StatusFound, base+"/auth/login?sso_error="+url.QueryEscape(err.Message))
		return
	}

	c.Redirect(http.StatusFound, base+"/auth/sso?code="+url.QueryEscape(handoff))
}

// formOrQuery reads a parameter from either the posted form or the query
// string, because the same callback serves both response modes.
func formOrQuery(c *gin.Context, key string) string {
	if v := c.PostForm(key); v != "" {
		return v
	}
	return c.Query(key)
}

type ssoExchangeRequest struct {
	Code string `json:"code"`
}

// SSOExchange swaps the handoff code for the session. Single use.
func (h *Handler) SSOExchange(c *gin.Context) {
	var req ssoExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		errx.Handle(c, errx.ErrInvalid)
		return
	}

	result, err := h.AuthService.SSOExchange(c.Request.Context(), req.Code)
	if err != nil {
		errx.Handle(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
