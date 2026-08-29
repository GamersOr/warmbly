package scheduler

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// ContactSendConstraint names the hard constraint holding a contact's next
// step back. The caller turns it into copy; the scheduler only knows which
// gate applied.
type ContactSendConstraint string

const (
	ConstraintNone             ContactSendConstraint = ""
	ConstraintStepWait         ContactSendConstraint = "step_wait"
	ConstraintConditionWindow  ContactSendConstraint = "condition_window"
	ConstraintStartDate        ContactSendConstraint = "start_date"
	ConstraintSendingWindow    ContactSendConstraint = "sending_window"
	ConstraintNewLeadCap       ContactSendConstraint = "new_lead_cap"
	ConstraintCapacity         ContactSendConstraint = "capacity"
	ConstraintCampaignInactive ContactSendConstraint = "campaign_inactive"
	ConstraintNoMailbox        ContactSendConstraint = "no_mailbox"
	ConstraintDomainAuth       ContactSendConstraint = "domain_auth"
	ConstraintCampaignEnded    ContactSendConstraint = "campaign_ended"
)

// ContactSendPreview is a read-only answer to "what happens next for this
// contact in this campaign". Route says which step; State, ScheduledAt and
// NotBefore say when, through the same constraints the send path applies.
type ContactSendPreview struct {
	Route *repository.ContactRoute
	State models.ContactNextActionState
	// ScheduledAt is set only when the step is due now: the slot the scheduler
	// would give it this tick.
	ScheduledAt *time.Time
	// NotBefore is the earliest the hard constraints allow the step.
	NotBefore  *time.Time
	Constraint ContactSendConstraint
}

// ContactSendPreviewer is the capability the contact drawer reads; the
// scheduler service satisfies it.
type ContactSendPreviewer interface {
	PreviewContactSend(ctx context.Context, campaignID, contactID uuid.UUID) (*ContactSendPreview, error)
}

// PreviewContactSend derives the contact's next step and its timing without
// writing anything: it routes the contact the way FindNextRoutedPair does and
// places the send the way CalculateNextCampaignTime does. A campaign is one
// self-perpetuating task, so this is the only honest source of "scheduled
// for"; nothing per contact is stored.
func (s *schedulerService) PreviewContactSend(ctx context.Context, campaignID, contactID uuid.UUID) (*ContactSendPreview, error) {
	campaign, err := s.campaignRepo.GetByID(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	route, err := s.campaignProgressRepo.RouteContact(ctx, campaignID, contactID)
	if err != nil {
		return nil, err
	}
	pv := &ContactSendPreview{Route: route}

	if route.WaitUntil != nil {
		pv.State = models.NextActionWaiting
		pv.NotBefore = route.WaitUntil
		pv.Constraint = ConstraintConditionWindow
		return pv, nil
	}
	if route.Target == nil || route.Excluded != "" {
		return pv, nil
	}
	if campaign.Status != "active" {
		pv.State = models.NextActionPaused
		pv.NotBefore = route.DueAt
		pv.Constraint = ConstraintCampaignInactive
		return pv, nil
	}

	accounts, meta, err := s.campaignSenders(ctx, campaign)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintNoMailbox
		return pv, nil
	}

	pair := &repository.ContactSequencePair{ContactID: contactID, SequenceID: *route.Target, IsNewLead: route.IsNewLead}
	at, sendable, _, perr := s.placeCampaignSend(ctx, campaign, accounts, meta, pair, true)
	switch {
	case perr == nil && sendable != nil:
		pv.State = models.NextActionDue
		slot := at
		pv.ScheduledAt = &slot
		return pv, nil
	case errors.Is(perr, ErrCampaignDeferred):
		pv.State = models.NextActionWaiting
		slot := at
		if route.DueAt != nil && route.DueAt.After(slot) {
			slot = *route.DueAt
		}
		pv.NotBefore = &slot
		pv.Constraint = s.deferralConstraint(ctx, campaign, route)
		return pv, nil
	case errors.Is(perr, ErrCampaignEnded):
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintCampaignEnded
		return pv, nil
	case errors.Is(perr, ErrDomainAuthFailing):
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintDomainAuth
		return pv, nil
	case errors.Is(perr, ErrNoEmailAccounts):
		pv.State = models.NextActionBlocked
		pv.Constraint = ConstraintCapacity
		return pv, nil
	}
	return nil, perr
}

// deferralConstraint picks the gate that explains a deferred placement, in
// the order the send path applies them: the step's own wait, the campaign
// start date, the sending window, today's new-lead cap, and otherwise the
// mailbox pool (daily caps, spacing, health, rest).
func (s *schedulerService) deferralConstraint(ctx context.Context, campaign *models.Campaign, route *repository.ContactRoute) ContactSendConstraint {
	grace := time.Now().Add(config.CampaignNotDueGraceSeconds * time.Second)
	if route.DueAt != nil && route.DueAt.After(grace) {
		return ConstraintStepWait
	}
	if campaign.StartDate != nil && campaign.StartDate.After(grace) {
		return ConstraintStartDate
	}
	if nextScheduleSlot(time.Now(), effectiveWindows(campaign), loadLocation(campaign.Timezone)).After(grace) {
		return ConstraintSendingWindow
	}
	if route.IsNewLead && campaign.MaxNewLeadsPerDay > 0 {
		if n, err := s.campaignRepo.CountNewLeadsStartedToday(ctx, campaign.ID); err == nil && n >= campaign.MaxNewLeadsPerDay {
			return ConstraintNewLeadCap
		}
	}
	return ConstraintCapacity
}
