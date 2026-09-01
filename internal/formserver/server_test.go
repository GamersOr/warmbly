package formserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/api/handler"
	"github.com/warmbly/warmbly/internal/app/form"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// stubFormService backs the REAL internal handlers, so this file tests the
// whole wire: forms service -> HTTP -> backend handler -> service call.
type stubFormService struct {
	form.Service // unimplemented methods panic; the internal endpoints use only these three

	mu      sync.Mutex
	form    *models.Form
	answers map[string][]string
	meta    form.SubmitMeta
	views   int
	reject  *errx.Error
}

func (s *stubFormService) PublicForm(_ context.Context, publicID string) (*models.Form, *errx.Error) {
	if s.form == nil || publicID != s.form.PublicID {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	return s.form, nil
}

func (s *stubFormService) RecordView(_ context.Context, _ uuid.UUID) {
	s.mu.Lock()
	s.views++
	s.mu.Unlock()
}

func (s *stubFormService) Submit(_ context.Context, publicID string, answers map[string][]string, meta form.SubmitMeta) (*form.SubmitResult, *errx.Error) {
	if s.form == nil || publicID != s.form.PublicID {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	if s.reject != nil {
		return nil, s.reject
	}
	s.mu.Lock()
	s.answers, s.meta = answers, meta
	s.mu.Unlock()
	return &form.SubmitResult{Message: "Thanks!"}, nil
}

func (s *stubFormService) viewCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.views
}

const shellMarker = `<div id="root"></div>`

func writeStaticDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"+shellMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "index-abc123.js"), []byte("console.log(1)"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newFixture(t *testing.T, submitLimit int) (*stubFormService, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	stub := &stubFormService{form: &models.Form{
		ID:       uuid.New(),
		PublicID: "pubtest123",
		Name:     "Demo request",
		Fields: []models.FormField{
			{ID: "email", Type: models.FormFieldEmail, Label: "Work email", Required: true, MapTo: "email"},
		},
		AllowedDomains: []string{"example.com"},
	}}
	h := &handler.Handler{FormService: stub}
	backend := gin.New()
	backend.GET("/api/v1/internal/forms/:publicID", h.InternalGetPublicForm)
	backend.POST("/api/v1/internal/forms/:publicID/views", h.InternalCountFormView)
	backend.POST("/api/v1/internal/forms/:publicID/submissions", h.InternalSubmitForm)
	ts := httptest.NewServer(backend)
	t.Cleanup(ts.Close)

	srv, err := New(Config{BackendURL: ts.URL, InternalToken: "test-token", StaticDir: writeStaticDir(t), SubmitLimit: submitLimit})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	r, err := srv.Router(nil)
	if err != nil {
		t.Fatalf("router: %v", err)
	}
	return stub, r
}

func get(r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func submit(r *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestFormServerServesShellAndCountsViewOnce(t *testing.T) {
	stub, r := newFixture(t, 0)

	w := get(r, "/f/pubtest123")
	if w.Code != http.StatusOK {
		t.Fatalf("shell status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), shellMarker) {
		t.Fatal("shell did not serve the built app")
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "example.com") {
		t.Fatalf("embed allowlist not enforced: %q", csp)
	}

	// Second render from the same source must not count again; the report
	// itself is async, so poll for the first one.
	get(r, "/f/pubtest123")
	deadline := time.Now().Add(2 * time.Second)
	for stub.viewCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	if got := stub.viewCount(); got != 1 {
		t.Fatalf("view count = %d, want 1", got)
	}

	if w := get(r, "/f/unknownid"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id status %d", w.Code)
	}
}

func TestFormServerServesFormJSONAndAssets(t *testing.T) {
	_, r := newFixture(t, 0)

	w := get(r, "/api/forms/pubtest123")
	if w.Code != http.StatusOK {
		t.Fatalf("form json status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		PublicID string             `json:"public_id"`
		Name     string             `json:"name"`
		Fields   []models.FormField `json:"fields"`
		Allowed  []string           `json:"allowed_domains"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Name != "Demo request" || len(res.Fields) != 1 || res.Fields[0].Label != "Work email" {
		t.Fatalf("form json: %s", w.Body.String())
	}
	if res.Allowed != nil {
		t.Fatal("embed allowlist must not reach the client")
	}
	if w := get(r, "/api/forms/unknownid"); w.Code != http.StatusNotFound {
		t.Fatalf("unknown id status %d", w.Code)
	}

	a := get(r, "/assets/index-abc123.js")
	if a.Code != http.StatusOK || !strings.Contains(a.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset serving: %d %q", a.Code, a.Header().Get("Cache-Control"))
	}
}

func TestFormServerForwardsSubmission(t *testing.T) {
	stub, r := newFixture(t, 0)

	w := submit(r, "/api/forms/pubtest123/submit", map[string]any{
		"answers": map[string][]string{"email": {"visitor@example.com"}},
		"website": "bot-filled", // honeypot: forwarded as a signal, not an answer
		"_wt":     1700000000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("submit status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Message != "Thanks!" {
		t.Fatalf("submit response: %s", w.Body.String())
	}
	if got := stub.answers["email"]; len(got) != 1 || got[0] != "visitor@example.com" {
		t.Fatalf("answers did not cross the wire: %+v", stub.answers)
	}
	if !stub.meta.HoneypotFilled {
		t.Fatal("honeypot signal lost on the wire")
	}
	if stub.meta.RenderedAt.Unix() != 1700000000 {
		t.Fatalf("rendered-at lost on the wire: %v", stub.meta.RenderedAt)
	}
}

func TestFormServerRelaysRejectionMessage(t *testing.T) {
	stub, r := newFixture(t, 0)
	stub.reject = errx.New(errx.BadRequest, "Email is required.")

	w := submit(r, "/api/forms/pubtest123/submit", map[string]any{
		"answers": map[string][]string{"email": {""}},
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status %d", w.Code)
	}
	var res struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.Message != "Email is required." {
		t.Fatalf("rejection not relayed verbatim: %s", w.Body.String())
	}
}

func TestFormServerSubmitRateLimit(t *testing.T) {
	_, r := newFixture(t, 2)
	payload := map[string]any{"answers": map[string][]string{"email": {"a@b.co"}}}
	for i := 0; i < 2; i++ {
		if w := submit(r, "/api/forms/pubtest123/submit", payload); w.Code != http.StatusOK {
			t.Fatalf("submit %d status %d", i, w.Code)
		}
	}
	w := submit(r, "/api/forms/pubtest123/submit", payload)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("third submit status %d, want 429", w.Code)
	}
}
