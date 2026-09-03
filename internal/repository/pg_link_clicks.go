package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LinkClick is one recorded click on one tracked link. Machine clicks (a
// security gateway walking every link at delivery time) are kept for the
// record with the reason they were flagged, but never count as engagement.
type LinkClick struct {
	ID            uuid.UUID
	TrackedLinkID *uuid.UUID
	TaskID        uuid.UUID
	CampaignID    uuid.UUID
	ContactID     uuid.UUID
	SequenceID    uuid.UUID
	Destination   string
	Label         string
	UserAgent     string
	IPHash        string
	Machine       bool
	MachineReason string
	ClickedAt     time.Time
}

// Machine-click reasons stored in email_link_clicks.machine_reason.
const (
	LinkClickReasonPrefetch = "prefetch" // no user agent: never a person's browser
	LinkClickReasonInstant  = "instant"  // arrived inside the machine window after dispatch
	LinkClickReasonBurst    = "burst"    // a second link of the same email from the same source within seconds
)

// LinkClickRepository is the per-link click log behind the contact timeline
// and the scanner heuristics. Only the tracking consumer writes it.
type LinkClickRepository interface {
	Insert(ctx context.Context, click *LinkClick) error
	// CountRecentOtherLinks counts clicks from the same source on OTHER links
	// of the same email since the given time: the burst signal.
	CountRecentOtherLinks(ctx context.Context, taskID uuid.UUID, ipHash, destination string, since time.Time) (int, error)
	// MarkBurst flags the source's earlier human-labelled clicks on the email
	// since the given time as machine, once a burst is recognised.
	MarkBurst(ctx context.Context, taskID uuid.UUID, ipHash string, since time.Time) (int64, error)
	// HasHumanClick reports whether any click on the step is still labelled
	// as a person's.
	HasHumanClick(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error)
	// IsMachine reads a logged click's current classification, which a burst
	// recognised after the fact may have changed.
	IsMachine(ctx context.Context, id uuid.UUID) (bool, error)
	// HasHumanClickOn reports whether a person already clicked this exact
	// link of the email, so a repeat is not logged twice. The ticket is the
	// identity when known (two links may share a destination); the
	// destination is the fallback for events from an older tracking build.
	HasHumanClickOn(ctx context.Context, taskID uuid.UUID, linkID *uuid.UUID, destination string) (bool, error)
}

type linkClickRepository struct {
	db *pgxpool.Pool
}

// NewLinkClickRepository creates a new link click repository.
func NewLinkClickRepository(db *pgxpool.Pool) LinkClickRepository {
	return &linkClickRepository{db: db}
}

func (r *linkClickRepository) Insert(ctx context.Context, c *LinkClick) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.ClickedAt.IsZero() {
		c.ClickedAt = time.Now()
	}
	query := `
		INSERT INTO email_link_clicks
			(id, tracked_link_id, task_id, campaign_id, contact_id, sequence_id,
			 destination, label, user_agent, ip_hash, machine, machine_reason, clicked_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := r.db.Exec(ctx, query,
		c.ID, c.TrackedLinkID, c.TaskID, c.CampaignID, c.ContactID, c.SequenceID,
		c.Destination, c.Label, c.UserAgent, c.IPHash, c.Machine, c.MachineReason, c.ClickedAt,
	)
	return err
}

func (r *linkClickRepository) CountRecentOtherLinks(ctx context.Context, taskID uuid.UUID, ipHash, destination string, since time.Time) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM email_link_clicks
		WHERE task_id = $1
		  AND ip_hash = $2
		  AND destination <> $3
		  AND clicked_at >= $4
	`
	var n int
	err := r.db.QueryRow(ctx, query, taskID, ipHash, destination, since).Scan(&n)
	return n, err
}

func (r *linkClickRepository) MarkBurst(ctx context.Context, taskID uuid.UUID, ipHash string, since time.Time) (int64, error) {
	query := `
		UPDATE email_link_clicks
		SET machine = true, machine_reason = $4
		WHERE task_id = $1
		  AND ip_hash = $2
		  AND clicked_at >= $3
		  AND machine = false
	`
	tag, err := r.db.Exec(ctx, query, taskID, ipHash, since, LinkClickReasonBurst)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *linkClickRepository) HasHumanClick(ctx context.Context, campaignID, contactID, sequenceID uuid.UUID) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM email_link_clicks
			WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3 AND machine = false
		)
	`
	var ok bool
	err := r.db.QueryRow(ctx, query, campaignID, contactID, sequenceID).Scan(&ok)
	return ok, err
}

func (r *linkClickRepository) IsMachine(ctx context.Context, id uuid.UUID) (bool, error) {
	var machine bool
	err := r.db.QueryRow(ctx, `SELECT machine FROM email_link_clicks WHERE id = $1`, id).Scan(&machine)
	return machine, err
}

func (r *linkClickRepository) HasHumanClickOn(ctx context.Context, taskID uuid.UUID, linkID *uuid.UUID, destination string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 FROM email_link_clicks
			WHERE task_id = $1 AND destination = $2 AND machine = false
		)
	`
	args := []any{taskID, destination}
	if linkID != nil {
		query = `
			SELECT EXISTS (
				SELECT 1 FROM email_link_clicks
				WHERE task_id = $1 AND tracked_link_id = $2 AND machine = false
			)
		`
		args = []any{taskID, *linkID}
	}
	var ok bool
	err := r.db.QueryRow(ctx, query, args...).Scan(&ok)
	return ok, err
}
