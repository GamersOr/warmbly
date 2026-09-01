// Package formwire is the wire contract between the backend's internal form
// endpoints and the forms service (cmd/forms). Both processes marshal these
// structs, so the two sides of the internal API cannot drift apart silently.
package formwire

import "github.com/warmbly/warmbly/internal/models"

// PublicForm is everything the forms service needs to render one published
// form: the definition plus the resolved captcha site key, and nothing the
// visitor must not see (no org id, no counters, no owner).
type PublicForm struct {
	PublicID       string             `json:"public_id"`
	Name           string             `json:"name"`
	Fields         []models.FormField `json:"fields"`
	Design         models.FormDesign  `json:"design"`
	AllowedDomains []string           `json:"allowed_domains,omitempty"`
	CaptchaSiteKey string             `json:"captcha_site_key,omitempty"`
}

// SubmitRequest carries a visitor's answers plus the abuse signals only the
// public-facing process can observe.
type SubmitRequest struct {
	Answers        map[string][]string `json:"answers"`
	RemoteIP       string              `json:"remote_ip"`
	SourceURL      string              `json:"source_url,omitempty"`
	CaptchaToken   string              `json:"captcha_token,omitempty"`
	HoneypotFilled bool                `json:"honeypot_filled,omitempty"`
	// RenderedAt is the unix second the page was served; bots submit near-instantly.
	RenderedAt int64 `json:"rendered_at,omitempty"`
}

// SubmitResult is what the visitor sees after a successful submit.
type SubmitResult struct {
	Message     string `json:"message"`
	RedirectURL string `json:"redirect_url,omitempty"`
}

// SubmitError is the 400 body for a rejected submission; Message is
// visitor-facing and shown inline on the form.
type SubmitError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
