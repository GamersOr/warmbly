package models

import (
	"time"

	"github.com/google/uuid"
)

// ContactCampaignState is one campaign a contact belongs to, read the way the
// contact drawer shows it: the flow with the steps this contact has been
// through, the lead status the Leads list derives, and what happens next.
type ContactCampaignState struct {
	CampaignID     uuid.UUID `json:"campaign_id"`
	CampaignName   string    `json:"campaign_name"`
	CampaignStatus string    `json:"campaign_status"`
	// LeadStatus follows ContactCampaignProgress.Status exactly.
	LeadStatus string `json:"lead_status"`
	// FailureReason is the worker's reason for the last failed send.
	FailureReason string `json:"failure_reason,omitempty"`

	Steps          []ContactCampaignStep `json:"steps"`
	CompletedSteps int                   `json:"completed_steps"`
	TotalSteps     int                   `json:"total_steps"`

	// CurrentStep is the step the contact is on now: the latest one sent.
	CurrentStep *ContactCampaignStep `json:"current_step,omitempty"`
	// LastAction is the most recent thing that happened on this campaign for
	// the contact ("Email sent", "Opened", ...) and when.
	LastAction   string     `json:"last_action,omitempty"`
	LastActionAt *time.Time `json:"last_action_at,omitempty"`

	// Next is what the scheduler will do next for this contact. Nil when the
	// flow has ended for them (finished, replied, bounced, stopped).
	Next *ContactNextAction `json:"next,omitempty"`
	// EndedReason says why Next is nil, in the words the timeline uses.
	EndedReason string `json:"ended_reason,omitempty"`
}

// ContactCampaignStep is one node of the campaign flow with this contact's
// progress on it.
type ContactCampaignStep struct {
	ID       uuid.UUID `json:"id"`
	Label    string    `json:"label"`
	Kind     string    `json:"kind"`
	Position int       `json:"position"`
	Subject  string    `json:"subject,omitempty"`

	SentAt    *time.Time `json:"sent_at,omitempty"`
	OpenedAt  *time.Time `json:"opened_at,omitempty"`
	ClickedAt *time.Time `json:"clicked_at,omitempty"`
	RepliedAt *time.Time `json:"replied_at,omitempty"`
	BouncedAt *time.Time `json:"bounced_at,omitempty"`
	FailedAt  *time.Time `json:"failed_at,omitempty"`
	// Attempts is how many sends of this step have failed so far.
	Attempts int `json:"attempts,omitempty"`
	// InFlight is a step reserved for a worker whose result has not come back.
	InFlight bool `json:"in_flight,omitempty"`
}

// ContactNextActionState says how firm the timing of the next action is.
type ContactNextActionState string

const (
	// NextActionDue: the step is due and the scheduler produced a slot for it.
	NextActionDue ContactNextActionState = "due"
	// NextActionWaiting: a hard constraint holds the step back (a step wait, a
	// sending window, a start date, a condition window, mailbox capacity).
	// NotBefore is the earliest it can go; there is no promised slot.
	NextActionWaiting ContactNextActionState = "waiting"
	// NextActionPaused: the campaign is not active, so nothing is scheduled.
	NextActionPaused ContactNextActionState = "paused"
	// NextActionBlocked: the campaign cannot send at all right now (no
	// mailbox, authentication failing, past its end date).
	NextActionBlocked ContactNextActionState = "blocked"
)

// ContactNextAction is a scheduler-backed preview of the contact's next step.
// A campaign is one self-perpetuating task, so this is derived on read through
// the scheduler's own constraints, never stored.
type ContactNextAction struct {
	// StepID is nil while a condition window is still open: which step comes
	// next depends on how the contact responds.
	StepID    *uuid.UUID `json:"step_id,omitempty"`
	StepLabel string     `json:"step_label"`
	Kind      string     `json:"kind,omitempty"`
	Subject   string     `json:"subject,omitempty"`

	State ContactNextActionState `json:"state"`
	// ScheduledAt is the slot the scheduler computed for this step. Only set
	// when the step is due now; other contacts ahead in the queue can still
	// push it later.
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`
	// NotBefore is the earliest the hard constraints let the step go.
	NotBefore *time.Time `json:"not_before,omitempty"`
	// Constraint is the human-readable reason the step is not going out now.
	Constraint string `json:"constraint,omitempty"`
}

type ContactCampaignStatesResult struct {
	Data []ContactCampaignState `json:"data"`
}
