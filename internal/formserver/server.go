// Package formserver is the public face of hosted forms: a standalone HTTP
// service (cmd/forms) that serves the TanStack form app (forms/ at the repo
// root), its per-form page shells, the embed loader, and a same-origin JSON
// API that forwards to the backend over the internal API. It exists so
// nothing customer-facing answers on the API origin and form traffic scales
// on its own box. Like the workers and the tracking service, it holds no
// database: the backend owns validation, contacts and counters; this process
// owns the shell (with its per-form CSP), caching and the per-visitor abuse
// checks only it can see.
package formserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/warmbly/warmbly/internal/formwire"
)

// formMaxSubmitBytes caps a public submit body; the largest legitimate form
// is dozens of short answers.
const formMaxSubmitBytes = 64 << 10

const (
	submitWindow       = 10 * time.Minute
	submitDefaultLimit = 30
	viewDedupeTTL      = 30 * time.Minute
	maxSourceURLLen    = 2048
)

type Config struct {
	// BackendURL and InternalToken reach the backend's /api/v1/internal
	// endpoints, the service's only dependency.
	BackendURL    string
	InternalToken string
	// StaticDir is the built forms app (forms/dist): index.html is the page
	// shell, assets/ the hashed bundles.
	StaticDir string
	// SubmitLimit is submissions per source IP per 10 minutes; 0 means the
	// default of 30.
	SubmitLimit int
}

type Server struct {
	client    *backendClient
	views     *ttlSet
	limits    *ipLimiter
	shell     []byte
	assetsDir string
}

func New(cfg Config) (*Server, error) {
	if cfg.BackendURL == "" {
		return nil, errors.New("formserver: BackendURL is required")
	}
	if cfg.InternalToken == "" {
		return nil, errors.New("formserver: InternalToken is required")
	}
	if cfg.StaticDir == "" {
		return nil, errors.New("formserver: StaticDir is required")
	}
	shell, err := os.ReadFile(filepath.Join(cfg.StaticDir, "index.html"))
	if err != nil {
		return nil, fmt.Errorf("formserver: cannot read %s/index.html (run `pnpm build` in forms/): %w", cfg.StaticDir, err)
	}
	limit := cfg.SubmitLimit
	if limit <= 0 {
		limit = submitDefaultLimit
	}
	return &Server{
		client:    newBackendClient(strings.TrimRight(cfg.BackendURL, "/"), cfg.InternalToken),
		views:     newTTLSet(viewDedupeTTL),
		limits:    newIPLimiter(limit, submitWindow),
		shell:     shell,
		assetsDir: filepath.Join(cfg.StaticDir, "assets"),
	}, nil
}

func (s *Server) Router(trustedProxies []string) (*gin.Engine, error) {
	r := gin.Default()
	// Same posture as the backend: trust no proxy unless the operator names
	// it, so a forged X-Forwarded-For cannot dodge the submit limiter.
	if len(trustedProxies) > 0 {
		if err := r.SetTrustedProxies(trustedProxies); err != nil {
			return nil, err
		}
	} else if err := r.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/forms.js", s.ServeFormsEmbedJS)
	r.GET("/f/:publicID", s.ServeFormShell)

	// Hashed filenames, so the bundles are immutable by construction.
	assets := r.Group("/assets", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	})
	assets.Static("/", s.assetsDir)

	api := r.Group("/api")
	api.GET("/forms/:publicID", s.GetPublicForm)
	api.POST("/forms/:publicID/submit", s.SubmitForm)
	return r, nil
}

// fetchForm resolves a form or reports how the lookup ended.
func (s *Server) fetchForm(c *gin.Context) (*formwire.PublicForm, error) {
	publicID := c.Param("publicID")
	if publicID == "" || len(publicID) > 64 {
		return nil, errFormNotFound
	}
	return s.client.Form(c.Request.Context(), publicID)
}

// ServeFormShell serves the app shell for a published form. Public and
// unauthenticated: the unguessable public id is the capability. The form is
// resolved before serving so unknown ids 404 like any dead link and the
// per-form embed CSP rides on the document the browser frames.
func (s *Server) ServeFormShell(c *gin.Context) {
	f, err := s.fetchForm(c)
	if errors.Is(err, errFormNotFound) {
		formNotFoundPage(c)
		return
	}
	if err != nil {
		formUnavailablePage(c)
		return
	}
	setFormFrameHeaders(c, f.AllowedDomains)
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "strict-origin-when-cross-origin")

	s.countFormView(c, f)

	c.Data(http.StatusOK, "text/html; charset=utf-8", s.shell)
}

// countFormView reports the render once per source per half hour, and skips
// prefetches so link previews do not inflate conversion stats.
func (s *Server) countFormView(c *gin.Context, f *formwire.PublicForm) {
	for _, header := range []string{"Sec-Purpose", "Purpose", "X-Purpose", "X-Moz"} {
		if v := c.GetHeader(header); v != "" && strings.Contains(strings.ToLower(v), "prefetch") {
			return
		}
	}
	sum := sha256.Sum256([]byte(c.ClientIP()))
	if !s.views.Add(f.PublicID + ":" + hex.EncodeToString(sum[:8])) {
		return
	}
	go s.client.CountView(f.PublicID)
}

// GetPublicForm hands the app the form definition: what the visitor may see
// and nothing more (the embed allowlist stays server-side with the CSP).
func (s *Server) GetPublicForm(c *gin.Context) {
	f, err := s.fetchForm(c)
	if errors.Is(err, errFormNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "unavailable"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"public_id":        f.PublicID,
		"name":             f.Name,
		"fields":           f.Fields,
		"design":           f.Design,
		"captcha_site_key": f.CaptchaSiteKey,
	})
}

// submitBody is what the app posts; keep in sync with forms/src/api.ts.
type submitBody struct {
	Answers map[string][]string `json:"answers"`
	// Website is the honeypot value; a human never fills it.
	Website      string `json:"website"`
	WT           int64  `json:"_wt"`
	CaptchaToken string `json:"captcha_token"`
	SourceURL    string `json:"source_url"`
}

// SubmitForm accepts a public submission, runs the checks only this process
// can make (per-IP budget, body cap) and forwards the rest to the backend.
// Same-origin with the app, so there is no CORS surface.
func (s *Server) SubmitForm(c *gin.Context) {
	fail := func(status int, msg string) {
		c.JSON(status, gin.H{"error": "form_submit_failed", "message": msg})
	}

	if ip := c.ClientIP(); ip != "" && !s.limits.Allow(ip) {
		c.Header("Retry-After", fmt.Sprintf("%d", int(submitWindow.Seconds())))
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":   "rate_limit_exceeded",
			"message": "Too many submissions from this address. Try again later.",
			"code":    "rate_limit_exceeded",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, formMaxSubmitBytes)
	var body submitBody
	if err := json.NewDecoder(c.Request.Body).Decode(&body); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			fail(http.StatusRequestEntityTooLarge, "The submission is too large.")
			return
		}
		fail(http.StatusBadRequest, "The submission could not be read.")
		return
	}

	sourceURL := body.SourceURL
	if sourceURL == "" {
		sourceURL = c.GetHeader("Referer")
	}
	if len(sourceURL) > maxSourceURLLen {
		sourceURL = sourceURL[:maxSourceURLLen]
	}

	req := &formwire.SubmitRequest{
		Answers:        body.Answers,
		RemoteIP:       c.ClientIP(),
		SourceURL:      sourceURL,
		CaptchaToken:   body.CaptchaToken,
		HoneypotFilled: strings.TrimSpace(body.Website) != "",
		RenderedAt:     body.WT,
	}

	out, err := s.client.Submit(c.Request.Context(), c.Param("publicID"), req)
	if err != nil {
		fail(http.StatusBadGateway, "Something went wrong. Please try again.")
		return
	}
	switch {
	case out.NotFound:
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	case out.Rejected != "":
		fail(http.StatusBadRequest, out.Rejected)
		return
	}
	c.JSON(http.StatusOK, out.Result)
}

// ServeFormsEmbedJS serves the embed loader. Cached for an hour, mirroring
// the tracking service's tracking.js.
func (s *Server) ServeFormsEmbedJS(c *gin.Context) {
	c.Header("Cache-Control", "public, max-age=3600")
	c.Data(http.StatusOK, "application/javascript; charset=utf-8", formsEmbedJS)
}
