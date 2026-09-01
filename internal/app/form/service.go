// Package form owns hosted lead-capture forms: the builder CRUD the
// dashboard edits, and the public render/submit pipeline that turns a
// visitor's answers into a contact, category links and a campaign lead.
package form

import (
	"context"
	"crypto/rand"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/utils"
	"github.com/warmbly/warmbly/internal/utils/validate"
)

type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.Form, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Form, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, name string) (*models.Form, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, in *models.FormWrite) (*models.Form, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	ListSubmissions(ctx context.Context, orgID, formID uuid.UUID, limit int, before *time.Time) ([]models.FormSubmission, bool, *errx.Error)
	DeleteSubmission(ctx context.Context, orgID, formID, subID uuid.UUID) *errx.Error

	// PublicForm resolves a published form for the hosted page.
	PublicForm(ctx context.Context, publicID string) (*models.Form, *errx.Error)
	// RecordView bumps the view counter, deduped per source by the caller.
	RecordView(ctx context.Context, formID uuid.UUID)
	// Submit runs the public pipeline: validate, contact upsert, store.
	Submit(ctx context.Context, publicID string, answers map[string][]string, meta SubmitMeta) (*SubmitResult, *errx.Error)

	// SetCaptcha wires the Turnstile verifier; nil leaves captcha off.
	SetCaptcha(v CaptchaVerifier)
	SetRealtime(p RealtimePublisher)
	SetWebhooks(d WebhookDispatcher)
	SetContacts(a ContactAdder)
}

// ContactAdder is the slice of the contact service the submit pipeline needs.
type ContactAdder interface {
	Add(ctx context.Context, userID string, orgID uuid.UUID, contacts []models.AddContact) ([]models.Contact, *errx.Error)
}

// CaptchaVerifier validates a Turnstile token for a source IP.
type CaptchaVerifier interface {
	Verify(ctx context.Context, token, remoteIP string) *errx.Error
}

// RealtimePublisher fans the submission signal out to the org channel.
type RealtimePublisher interface {
	PublishFormSubmission(ctx context.Context, orgID, formID uuid.UUID, submissionID, contactID string)
}

// WebhookDispatcher delivers form.submitted to subscribed endpoints.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// SubmitMeta is everything the handler knows about the request that is not
// an answer: abuse signals and attribution.
type SubmitMeta struct {
	RemoteIP     string
	SourceURL    string
	CaptchaToken string
	// HoneypotFilled means the hidden trap field carried a value.
	HoneypotFilled bool
	// RenderedAt is when the page was served; bots submit near-instantly.
	RenderedAt time.Time
}

// SubmitResult is what the visitor sees after a successful submit.
type SubmitResult struct {
	Message     string `json:"message"`
	RedirectURL string `json:"redirect_url,omitempty"`
}

type service struct {
	repo     repository.FormRepository
	contacts ContactAdder
	captcha  CaptchaVerifier
	realtime RealtimePublisher
	webhooks WebhookDispatcher
}

func NewService(repo repository.FormRepository) Service {
	return &service{repo: repo}
}

func (s *service) SetCaptcha(v CaptchaVerifier)    { s.captcha = v }
func (s *service) SetRealtime(p RealtimePublisher) { s.realtime = p }
func (s *service) SetWebhooks(d WebhookDispatcher) { s.webhooks = d }
func (s *service) SetContacts(a ContactAdder)      { s.contacts = a }

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]models.Form, *errx.Error) {
	return s.repo.List(ctx, orgID)
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Form, *errx.Error) {
	return s.repo.Get(ctx, orgID, id)
}

func (s *service) Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, name string) (*models.Form, *errx.Error) {
	name, xerr := models.ValidateFormName(name)
	if xerr != nil {
		return nil, xerr
	}
	f := &models.Form{
		PublicID:       newPublicID(),
		Name:           name,
		Status:         models.FormStatusDraft,
		Fields:         defaultFields(),
		SuccessMessage: models.FormDefaultSuccessMsg,
	}
	return s.repo.Create(ctx, orgID, createdBy, f)
}

// defaultFields seeds a new form so the builder never opens empty.
func defaultFields() []models.FormField {
	return []models.FormField{
		{ID: "first_name", Type: models.FormFieldText, Label: "First name", MapTo: "first_name", Width: "half"},
		{ID: "last_name", Type: models.FormFieldText, Label: "Last name", MapTo: "last_name", Width: "half"},
		{ID: "email", Type: models.FormFieldEmail, Label: "Email", MapTo: "email", Required: true},
	}
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, in *models.FormWrite) (*models.Form, *errx.Error) {
	f, xerr := s.repo.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if in.Name != nil {
		name, xerr := models.ValidateFormName(*in.Name)
		if xerr != nil {
			return nil, xerr
		}
		f.Name = name
	}
	if in.Fields != nil {
		fields := *in.Fields
		if xerr := models.ValidateFormFields(fields); xerr != nil {
			return nil, xerr
		}
		f.Fields = fields
	}
	if in.Design != nil {
		d := *in.Design
		if xerr := models.ValidateFormDesign(&d); xerr != nil {
			return nil, xerr
		}
		models.NormalizeFormDesign(&d)
		f.Design = d
	}
	if in.SuccessMessage != nil {
		msg := strings.TrimSpace(*in.SuccessMessage)
		if len(msg) > models.FormMaxTextLen {
			return nil, errx.New(errx.BadRequest, "success message is too long")
		}
		if msg == "" {
			msg = models.FormDefaultSuccessMsg
		}
		f.SuccessMessage = msg
	}
	if in.RedirectURL != nil {
		u, xerr := models.ValidateFormRedirectURL(*in.RedirectURL)
		if xerr != nil {
			return nil, xerr
		}
		f.RedirectURL = u
	}
	if in.CampaignID.Set {
		f.CampaignID = in.CampaignID.Value
	}
	if in.CategoryIDs != nil {
		f.CategoryIDs = *in.CategoryIDs
	}
	if in.AllowedDomains != nil {
		domains, xerr := models.ValidateFormDomains(*in.AllowedDomains)
		if xerr != nil {
			return nil, xerr
		}
		f.AllowedDomains = domains
	}
	if in.CaptchaEnabled != nil {
		f.CaptchaEnabled = *in.CaptchaEnabled
	}
	if in.Status != nil {
		if !in.Status.Valid() {
			return nil, errx.New(errx.BadRequest, "status must be draft, published or archived")
		}
		if *in.Status == models.FormStatusPublished && f.Status != models.FormStatusPublished {
			if xerr := publishable(f.Fields); xerr != nil {
				return nil, xerr
			}
			now := time.Now()
			f.PublishedAt = &now
		}
		f.Status = *in.Status
	}
	return s.repo.Update(ctx, orgID, f)
}

// publishable refuses to put a form online that cannot collect anything.
func publishable(fields []models.FormField) *errx.Error {
	for _, f := range fields {
		if f.Type.IsInput() && f.Type != models.FormFieldHidden {
			return nil
		}
	}
	return errx.New(errx.BadRequest, "add at least one input field before publishing")
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	return s.repo.Delete(ctx, orgID, id)
}

func (s *service) ListSubmissions(ctx context.Context, orgID, formID uuid.UUID, limit int, before *time.Time) ([]models.FormSubmission, bool, *errx.Error) {
	return s.repo.ListSubmissions(ctx, orgID, formID, limit, before)
}

func (s *service) DeleteSubmission(ctx context.Context, orgID, formID, subID uuid.UUID) *errx.Error {
	return s.repo.DeleteSubmission(ctx, orgID, formID, subID)
}

func (s *service) PublicForm(ctx context.Context, publicID string) (*models.Form, *errx.Error) {
	f, xerr := s.repo.GetByPublicID(ctx, publicID)
	if xerr != nil {
		return nil, xerr
	}
	if f.Status != models.FormStatusPublished {
		return nil, errx.New(errx.NotFound, "form not found")
	}
	return f, nil
}

func (s *service) RecordView(ctx context.Context, formID uuid.UUID) {
	// Best-effort: a lost view must never fail a page render.
	_ = s.repo.RecordView(ctx, formID)
}

// minFillSeconds is the human floor: a submit faster than this after render
// is treated as a bot and quietly discarded.
const minFillSeconds = 2

func (s *service) Submit(ctx context.Context, publicID string, answers map[string][]string, meta SubmitMeta) (*SubmitResult, *errx.Error) {
	f, xerr := s.PublicForm(ctx, publicID)
	if xerr != nil {
		return nil, xerr
	}
	ok := &SubmitResult{Message: f.SuccessMessage, RedirectURL: f.RedirectURL}

	// Traps: pretend success so a bot learns nothing and stops probing.
	if meta.HoneypotFilled {
		return ok, nil
	}
	if !meta.RenderedAt.IsZero() && time.Since(meta.RenderedAt) < minFillSeconds*time.Second {
		return ok, nil
	}
	if f.CaptchaEnabled && s.captcha != nil {
		if xerr := s.captcha.Verify(ctx, meta.CaptchaToken, meta.RemoteIP); xerr != nil {
			return nil, errx.New(errx.BadRequest, "captcha verification failed")
		}
	}

	data, lead, xerr := buildSubmission(f.Fields, answers)
	if xerr != nil {
		return nil, xerr
	}

	sub := &models.FormSubmission{
		FormID:         f.ID,
		OrganizationID: f.OrganizationID,
		Data:           data,
		SourceURL:      truncate(meta.SourceURL, 2048),
	}

	// The contact write is best-effort: a plan cap or a bad address must not
	// lose the submission, and never the visitor's success page.
	var contactID string
	if lead != nil && s.contacts != nil {
		if owner := f.CreatedBy; owner != nil {
			lead.Campaigns = campaignList(f.CampaignID)
			lead.Categories = categoryList(f.CategoryIDs)
			lead.Source = models.ContactSourceForm
			lead.SourceDetail = f.Name
			created, cerr := s.contacts.Add(ctx, owner.String(), f.OrganizationID, []models.AddContact{*lead})
			if cerr == nil && len(created) > 0 {
				id := created[0].ID
				sub.ContactID = &id
				contactID = id.String()
			}
		}
	}

	stored, xerr := s.repo.CreateSubmission(ctx, sub)
	if xerr != nil {
		return nil, xerr
	}
	if sub.ContactID != nil {
		_ = s.repo.LogSubmissionActivity(ctx, f.OrganizationID, *sub.ContactID, f.ID, f.Name, stored.ID)
	}
	if s.realtime != nil {
		s.realtime.PublishFormSubmission(ctx, f.OrganizationID, f.ID, stored.ID.String(), contactID)
	}
	if s.webhooks != nil {
		payload := map[string]any{
			"form_id":       f.ID.String(),
			"form_name":     f.Name,
			"submission_id": stored.ID.String(),
			"data":          data,
			"source_url":    sub.SourceURL,
		}
		if contactID != "" {
			payload["contact_id"] = contactID
		}
		_, _ = s.webhooks.Dispatch(ctx, f.OrganizationID, models.WebhookEventFormSubmitted, payload)
	}
	return ok, nil
}

// buildSubmission validates the answers against the field list and splits
// them into the stored payload and the contact to upsert. A nil lead means
// the form collected no usable email.
func buildSubmission(fields []models.FormField, answers map[string][]string) (map[string]any, *models.AddContact, *errx.Error) {
	data := make(map[string]any, len(fields))
	cols := map[string]string{}
	custom := map[string]string{}

	for _, f := range fields {
		if !f.Type.IsInput() {
			continue
		}
		vals := answers[f.ID]
		label := f.Label
		if label == "" {
			label = f.ID
		}

		if f.Type == models.FormFieldCheckboxes {
			picked := make([]string, 0, len(vals))
			allowed := optionSet(f.Options)
			for _, v := range vals {
				v = strings.TrimSpace(v)
				if v == "" {
					continue
				}
				if !allowed[v] {
					return nil, nil, errx.New(errx.BadRequest, label+": unknown option")
				}
				picked = append(picked, v)
			}
			if f.Required && len(picked) == 0 {
				return nil, nil, errx.New(errx.BadRequest, label+" is required")
			}
			if len(picked) > 0 {
				data[f.ID] = picked
				custom[customKey(f)] = strings.Join(picked, ", ")
			}
			continue
		}

		v := ""
		if len(vals) > 0 {
			v = strings.TrimSpace(vals[0])
		}
		if f.Type == models.FormFieldHidden && v == "" {
			v = f.Value
		}
		if len(v) > models.FormMaxAnswerLen {
			return nil, nil, errx.New(errx.BadRequest, label+" is too long")
		}

		switch f.Type {
		case models.FormFieldCheckbox:
			checked := v == "on" || v == "true" || v == "1" || v == "yes"
			if f.Required && !checked {
				return nil, nil, errx.New(errx.BadRequest, label+" must be checked")
			}
			if checked {
				data[f.ID] = "yes"
				custom[customKey(f)] = "yes"
			}
			continue
		case models.FormFieldEmail:
			if v != "" && !validate.Email(v) {
				return nil, nil, errx.New(errx.BadRequest, label+" must be a valid email address")
			}
			v = strings.ToLower(v)
		case models.FormFieldNumber:
			if v != "" {
				if _, err := strconv.ParseFloat(v, 64); err != nil {
					return nil, nil, errx.New(errx.BadRequest, label+" must be a number")
				}
			}
		case models.FormFieldDate:
			if v != "" {
				if _, err := time.Parse("2006-01-02", v); err != nil {
					return nil, nil, errx.New(errx.BadRequest, label+" must be a date")
				}
			}
		case models.FormFieldSelect, models.FormFieldRadio:
			if v != "" && !optionSet(f.Options)[v] {
				return nil, nil, errx.New(errx.BadRequest, label+": unknown option")
			}
		}

		if f.Required && v == "" {
			return nil, nil, errx.New(errx.BadRequest, label+" is required")
		}
		if v == "" {
			continue
		}
		data[f.ID] = v
		if f.MapTo != "" {
			cols[f.MapTo] = v
		} else {
			custom[customKey(f)] = v
		}
	}

	if cols["email"] == "" {
		return data, nil, nil
	}
	lead := &models.AddContact{
		Email:        cols["email"],
		FirstName:    cols["first_name"],
		LastName:     cols["last_name"],
		Company:      cols["company"],
		Phone:        cols["phone"],
		CustomFields: custom,
	}
	return data, lead, nil
}

var jsonKeyStrip = regexp.MustCompile(`[^A-Za-z0-9_ -]+`)

// customKey turns the field label into an addressable contact custom-field
// key ("Company size" stays as-is; punctuation is stripped so "Role?" still
// yields a key the template engine can resolve). The field id is the
// fallback when nothing usable remains.
func customKey(f models.FormField) string {
	label := f.Label
	if label == "" {
		label = f.ID
	}
	key := utils.NormalizeJSONKey(label)
	if !utils.IsValidJSONKey(key) {
		key = utils.NormalizeJSONKey(jsonKeyStrip.ReplaceAllString(key, " "))
	}
	if utils.IsValidJSONKey(key) {
		return key
	}
	return f.ID
}

func optionSet(opts []string) map[string]bool {
	set := make(map[string]bool, len(opts))
	for _, o := range opts {
		set[o] = true
	}
	return set
}

func campaignList(id *uuid.UUID) []string {
	if id == nil {
		return nil
	}
	return []string{id.String()}
}

func categoryList(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// publicIDAlphabet keeps the token URL-safe and unambiguous.
const publicIDAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// newPublicID mints the unguessable page token: 21 chars of 32-symbol
// alphabet is over 100 bits of entropy.
func newPublicID() string {
	buf := make([]byte, 21)
	if _, err := rand.Read(buf); err != nil {
		return uuid.NewString()
	}
	for i, b := range buf {
		buf[i] = publicIDAlphabet[int(b)%len(publicIDAlphabet)]
	}
	return string(buf)
}
