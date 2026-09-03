package tasks

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// GetCampaignSequences returns the sequences for a campaign ordered by position
func (s *tasksService) GetCampaignSequences(ctx context.Context, campaignID uuid.UUID) ([]models.Sequence, error) {
	return s.campaignRepo.GetSequencesByCampaignID(ctx, campaignID)
}

// SendTestEmail renders a campaign email and sends it to a test recipient for preview
func (s *tasksService) SendTestEmail(ctx context.Context, userID string, accountID uuid.UUID, recipient string, campaign *models.Campaign, sequence *models.Sequence) *errx.Error {
	// Load the email account
	account, err := s.emailRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return errx.New(errx.NotFound, "email account not found")
	}

	// Verify account belongs to user
	if account.UserID != userID {
		return errx.ErrForbidden
	}

	// Create a dummy contact for template rendering
	testContact := models.Contact{
		ID:        uuid.New(),
		FirstName: "Test",
		LastName:  "Recipient",
		Email:     recipient,
		Company:   "Test Company",
	}

	// A test send carries the real opt-out footer and header so the sender
	// sees exactly what a recipient will, but its link names no contact
	// (uuid.Nil), so clicking it can never suppress anyone.
	var orgID uuid.UUID
	if account.OrganizationID != nil {
		orgID = *account.OrganizationID
	}
	optOut := s.resolveOptOut(ctx, orgID, campaign)
	var unsubscribeURL string
	if s.unsubLinks != nil && s.unsubLinks.Enabled() {
		unsubscribeURL = s.unsubLinks.URL(orgID, campaign.ID, uuid.Nil, time.Now())
	}
	extra := map[string]string{UnsubscribeLinkVar: unsubscribeURL}

	// Render templates with the test contact
	subject := RenderTemplateWith(sequence.Subject, testContact, extra)
	bodyHTML := RenderTemplateWith(sequence.BodyHTML, testContact, extra)
	bodyPlain := RenderTemplateWith(sequence.BodyPlain, testContact, extra)

	if bodyPlain == "" && bodyHTML != "" {
		bodyPlain = ExtractPlainTextFromHTML(bodyHTML)
	}
	// The test shows what the campaign will send: plain-text campaigns get
	// no HTML part here either.
	if campaign.TextOnly {
		bodyHTML = ""
	}

	// Prepend [TEST] to subject
	subject = "[TEST] " + subject

	// Add signature if enabled
	// Same guard as the campaign send path: a plain-text test must not grow
	// an HTML part back through the signature.
	if account.SignatureSync {
		if bodyHTML != "" {
			bodyHTML = AddSignature(bodyHTML, account.SignatureHTML, true)
		}
		bodyPlain = AddSignature(bodyPlain, account.SignaturePlain, false)
	}
	bodyHTML, bodyPlain = appendOptOut(bodyHTML, bodyPlain, optOut, unsubscribeURL)

	// Generate message ID
	messageID := generateMessageID(account.Email)

	headerURL := ""
	if campaign.UnsubscribeHeader {
		headerURL = unsubscribeURL
	}

	// Build tracking info (disabled for test emails)
	emailMsg := EmailMessage{
		From:           account.Email,
		To:             []string{recipient},
		Subject:        subject,
		BodyHTML:       bodyHTML,
		BodyPlain:      bodyPlain,
		MessageID:      messageID,
		IsWarmup:       false,
		UnsubscribeURL: headerURL,
	}

	taskID := uuid.New()
	if err := s.emailSender.Send(ctx, taskID, emailMsg, *account); err != nil {
		return errx.New(errx.Internal, fmt.Sprintf("failed to send test email: %v", err))
	}

	return nil
}
