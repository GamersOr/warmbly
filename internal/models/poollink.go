package models

import (
	"time"

	"github.com/google/uuid"
)

// Pool link is the bridge between a self-hosted Warmbly instance and the
// hosted warmup pool. The cloud holds a mailbox's credential for warmup only
// and runs the sends, replies and inbox engagement on its own workers; the
// self-hosted instance keeps everything else (campaigns, contacts, inbox,
// storage) and mirrors the warmup state for its dashboard.

// PoolLinkCodeStatus is the lifecycle of one device-code handshake.
type PoolLinkCodeStatus string

const (
	PoolLinkCodePending  PoolLinkCodeStatus = "pending"
	PoolLinkCodeApproved PoolLinkCodeStatus = "approved"
	// Claimed: the instance has fetched its token, the code is spent.
	PoolLinkCodeClaimed PoolLinkCodeStatus = "claimed"
	PoolLinkCodeDenied  PoolLinkCodeStatus = "denied"
)

// PoolLinkCode is one handshake row on the cloud side.
type PoolLinkCode struct {
	ID              uuid.UUID          `json:"id"`
	UserCode        string             `json:"user_code"`
	InstanceName    string             `json:"instance_name"`
	InstanceURL     string             `json:"instance_url"`
	InstanceVersion string             `json:"instance_version"`
	Status          PoolLinkCodeStatus `json:"status"`
	OrganizationID  *uuid.UUID         `json:"organization_id,omitempty"`
	InstanceID      *uuid.UUID         `json:"instance_id,omitempty"`
	ExpiresAt       time.Time          `json:"expires_at"`
	CreatedAt       time.Time          `json:"created_at"`
}

// PoolLinkInstance is a linked self-hosted instance as the cloud sees it.
type PoolLinkInstance struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	Name           string     `json:"name"`
	URL            string     `json:"url"`
	Version        string     `json:"version"`
	CreatedBy      *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	LastSeenAt     *time.Time `json:"last_seen_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	// MailboxCount is filled by list endpoints only.
	MailboxCount int `json:"mailbox_count"`
}

// PoolLinkMailbox ties an enrolled cloud mailbox back to the remote one.
type PoolLinkMailbox struct {
	InstanceID     uuid.UUID `json:"instance_id"`
	RemoteID       uuid.UUID `json:"remote_id"`
	EmailAccountID uuid.UUID `json:"email_account_id"`
	EnrolledAt     time.Time `json:"enrolled_at"`
}

// PoolLinkStartRequest is what a self-hosted instance sends to begin linking.
type PoolLinkStartRequest struct {
	InstanceName    string `json:"instance_name"`
	InstanceURL     string `json:"instance_url"`
	InstanceVersion string `json:"instance_version"`
}

// PoolLinkStartResponse is the device-code grant.
type PoolLinkStartResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// PoolLinkPollResponse is the poll answer; Token and the rest are present
// only once the code has been approved.
type PoolLinkPollResponse struct {
	Status        PoolLinkCodeStatus `json:"status"`
	InstanceID    *uuid.UUID         `json:"instance_id,omitempty"`
	InstanceToken string             `json:"instance_token,omitempty"`
	Organization  *PoolLinkOrgInfo   `json:"organization,omitempty"`
}

// PoolLinkOrgInfo is the cloud workspace a link belongs to.
type PoolLinkOrgInfo struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// PoolLinkPlan is the linked instance's allowance as the cloud computes it.
type PoolLinkPlan struct {
	// Tier is "free" or "paid".
	Tier string `json:"tier"`
	// MailboxLimit is nil when unlimited.
	MailboxLimit *int `json:"mailbox_limit"`
	Enrolled     int  `json:"enrolled"`
	// PriceUSD is the monthly price of the paid tier, for the upgrade card.
	PriceUSD int `json:"price_usd"`
	// UpgradeURL is where the paid tier is bought; empty when billing is off
	// or the plan has no price attached.
	UpgradeURL string `json:"upgrade_url,omitempty"`
	// WarmupEntitled is false when the cloud workspace itself cannot warm
	// (billing off is always entitled).
	WarmupEntitled bool `json:"warmup_entitled"`
}

// PoolLinkInstanceInfo is the instance-facing status document.
type PoolLinkInstanceInfo struct {
	Instance     PoolLinkInstance `json:"instance"`
	Organization PoolLinkOrgInfo  `json:"organization"`
	Plan         PoolLinkPlan     `json:"plan"`
}

// PoolLinkWarmupSettings is the ramp the self-hosted instance asks the cloud
// to run; zero values fall back to the cloud defaults.
type PoolLinkWarmupSettings struct {
	Base      int    `json:"base"`
	Max       int    `json:"max"`
	Increase  int    `json:"increase"`
	ReplyRate int    `json:"reply_rate"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Days      int    `json:"days"`
	Timezone  string `json:"timezone"`
}

// PoolLinkOAuthCredential carries a Gmail or Microsoft refresh grant.
type PoolLinkOAuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// PoolLinkEnrollRequest enrolls one remote mailbox. Exactly one credential
// block is set, matching Provider.
type PoolLinkEnrollRequest struct {
	RemoteID uuid.UUID                `json:"remote_id"`
	Email    string                   `json:"email"`
	Name     string                   `json:"name"`
	Provider InboxProvider            `json:"provider"`
	OAuth    *PoolLinkOAuthCredential `json:"oauth,omitempty"`
	SMTPIMAP *SmtpImap                `json:"smtp_imap,omitempty"`
	Warmup   PoolLinkWarmupSettings   `json:"warmup"`
}

// PoolLinkMailboxState is the per-mailbox view returned to the instance and
// shown in both dashboards.
type PoolLinkMailboxState struct {
	RemoteID       uuid.UUID              `json:"remote_id"`
	EmailAccountID uuid.UUID              `json:"email_account_id"`
	Email          string                 `json:"email"`
	Provider       string                 `json:"provider"`
	Status         string                 `json:"status"`
	EnrolledAt     time.Time              `json:"enrolled_at"`
	Warmup         *WarmupStatusInfo      `json:"warmup,omitempty"`
	Health         *WarmupHealthInfo      `json:"health,omitempty"`
	SentToday      int                    `json:"sent_today"`
	Sent7d         int                    `json:"sent_7d"`
	Replied7d      int                    `json:"replied_7d"`
	SpamPlaced7d   int                    `json:"spam_placed_7d"`
	Errors         []AccountError         `json:"errors,omitempty"`
	AuthState      string                 `json:"auth_state"`
	Settings       PoolLinkWarmupSettings `json:"settings"`
}

// PoolLinkMailboxPatch updates a mailbox's ramp or lifecycle on the cloud.
type PoolLinkMailboxPatch struct {
	// Lifecycle is "pause", "resume" or empty.
	Lifecycle string                   `json:"lifecycle,omitempty"`
	Warmup    *PoolLinkWarmupSettings  `json:"warmup,omitempty"`
	OAuth     *PoolLinkOAuthCredential `json:"oauth,omitempty"`
	SMTPIMAP  *SmtpImap                `json:"smtp_imap,omitempty"`
}

// CloudLink is the self-hosted instance's single link row. Token is never
// serialized.
type CloudLink struct {
	CloudURL         string     `json:"cloud_url"`
	InstanceID       uuid.UUID  `json:"instance_id"`
	Token            string     `json:"-"`
	OrganizationName string     `json:"organization_name"`
	ConnectedBy      *uuid.UUID `json:"connected_by,omitempty"`
	ConnectedAt      time.Time  `json:"connected_at"`
	LastSyncedAt     *time.Time `json:"last_synced_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
}

// CloudLinkMailbox marks a local mailbox as warmed by the cloud.
type CloudLinkMailbox struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	RemoteID       uuid.UUID `json:"remote_id"`
	EnrolledAt     time.Time `json:"enrolled_at"`
}

// CloudLinkStatus is the self-hosted dashboard's view of the link.
type CloudLinkStatus struct {
	Connected bool                  `json:"connected"`
	Link      *CloudLink            `json:"link,omitempty"`
	Info      *PoolLinkInstanceInfo `json:"info,omitempty"`
	// Reachable is false when the cloud could not be contacted on this read;
	// Link is still returned from the local row.
	Reachable bool   `json:"reachable"`
	Error     string `json:"error,omitempty"`
	// DefaultCloudURL is what the connect flow proposes.
	DefaultCloudURL string `json:"default_cloud_url"`
}

// CloudLinkMailboxRow merges a local mailbox with its cloud warmup state.
type CloudLinkMailboxRow struct {
	ID         uuid.UUID             `json:"id"`
	Email      string                `json:"email"`
	Name       string                `json:"name"`
	Provider   string                `json:"provider"`
	Status     string                `json:"status"`
	Enrolled   bool                  `json:"enrolled"`
	EnrolledAt *time.Time            `json:"enrolled_at,omitempty"`
	Cloud      *PoolLinkMailboxState `json:"cloud,omitempty"`
}
