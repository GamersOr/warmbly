package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// PoolLinkRepository is the cloud side of pool link: handshake codes, linked
// instances and the mailboxes they enrolled.
type PoolLinkRepository interface {
	CreateCode(ctx context.Context, deviceCodeHash, userCode string, req models.PoolLinkStartRequest, expiresAt time.Time) (*models.PoolLinkCode, error)
	GetCodeByUserCode(ctx context.Context, userCode string) (*models.PoolLinkCode, error)
	// ApproveCode binds a pending code to an organization and stores the token
	// the instance will collect on its next poll. Returns false when the code
	// is not pending anymore.
	ApproveCode(ctx context.Context, userCode string, orgID, approvedBy, instanceID uuid.UUID, instanceToken string) (bool, error)
	DenyCode(ctx context.Context, userCode string) (bool, error)
	// ClaimCode returns the approved code for a device code and clears the
	// stored token in the same statement, so a token is handed out once.
	ClaimCode(ctx context.Context, deviceCodeHash string) (*models.PoolLinkCode, string, error)
	DeleteExpiredCodes(ctx context.Context) error

	CreateInstance(ctx context.Context, inst *models.PoolLinkInstance, tokenHash string) error
	GetInstanceByTokenHash(ctx context.Context, tokenHash string) (*models.PoolLinkInstance, error)
	GetInstance(ctx context.Context, id uuid.UUID) (*models.PoolLinkInstance, error)
	ListInstances(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkInstance, error)
	TouchInstance(ctx context.Context, id uuid.UUID, version string) error
	RevokeInstance(ctx context.Context, id uuid.UUID) error
	// HasActiveInstance reports whether the organization has any live link,
	// which is what entitles it to warm enrolled mailboxes for free.
	HasActiveInstance(ctx context.Context, orgID uuid.UUID) (bool, error)

	EnrollMailbox(ctx context.Context, m *models.PoolLinkMailbox) error
	GetMailboxByRemote(ctx context.Context, instanceID, remoteID uuid.UUID) (*models.PoolLinkMailbox, error)
	GetMailboxByAccount(ctx context.Context, accountID uuid.UUID) (*models.PoolLinkMailbox, error)
	ListMailboxes(ctx context.Context, instanceID uuid.UUID) ([]models.PoolLinkMailbox, error)
	CountMailboxesForOrganization(ctx context.Context, orgID uuid.UUID) (int, error)
	DeleteMailbox(ctx context.Context, instanceID, remoteID uuid.UUID) error
}

type poolLinkRepository struct {
	db *pgxpool.Pool
}

func NewPoolLinkRepository(db *pgxpool.Pool) PoolLinkRepository {
	return &poolLinkRepository{db: db}
}

const poolLinkCodeColumns = `id, user_code, instance_name, instance_url, instance_version, status, organization_id, instance_id, expires_at, created_at`

func scanPoolLinkCode(row pgx.Row) (*models.PoolLinkCode, error) {
	var c models.PoolLinkCode
	if err := row.Scan(&c.ID, &c.UserCode, &c.InstanceName, &c.InstanceURL, &c.InstanceVersion, &c.Status, &c.OrganizationID, &c.InstanceID, &c.ExpiresAt, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (r *poolLinkRepository) CreateCode(ctx context.Context, deviceCodeHash, userCode string, req models.PoolLinkStartRequest, expiresAt time.Time) (*models.PoolLinkCode, error) {
	query := `
		INSERT INTO pool_link_codes (device_code_hash, user_code, instance_name, instance_url, instance_version, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + poolLinkCodeColumns
	c, err := scanPoolLinkCode(r.db.QueryRow(ctx, query, deviceCodeHash, userCode, req.InstanceName, req.InstanceURL, req.InstanceVersion, expiresAt))
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return c, nil
}

func (r *poolLinkRepository) GetCodeByUserCode(ctx context.Context, userCode string) (*models.PoolLinkCode, error) {
	query := `SELECT ` + poolLinkCodeColumns + ` FROM pool_link_codes WHERE user_code = $1 AND expires_at > NOW()`
	c, err := scanPoolLinkCode(r.db.QueryRow(ctx, query, userCode))
	if err != nil {
		db.CaptureError(err, query, []any{userCode}, "queryrow")
		return nil, err
	}
	return c, nil
}

func (r *poolLinkRepository) ApproveCode(ctx context.Context, userCode string, orgID, approvedBy, instanceID uuid.UUID, instanceToken string) (bool, error) {
	query := `
		UPDATE pool_link_codes
		SET status = 'approved', organization_id = $2, approved_by = $3, instance_id = $4, instance_token = $5
		WHERE user_code = $1 AND status = 'pending' AND expires_at > NOW()
	`
	tag, err := r.db.Exec(ctx, query, userCode, orgID, approvedBy, instanceID, instanceToken)
	if err != nil {
		db.CaptureError(err, query, nil, "exec")
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *poolLinkRepository) DenyCode(ctx context.Context, userCode string) (bool, error) {
	query := `UPDATE pool_link_codes SET status = 'denied' WHERE user_code = $1 AND status = 'pending'`
	tag, err := r.db.Exec(ctx, query, userCode)
	if err != nil {
		db.CaptureError(err, query, []any{userCode}, "exec")
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *poolLinkRepository) ClaimCode(ctx context.Context, deviceCodeHash string) (*models.PoolLinkCode, string, error) {
	// A pending code is returned as-is; an approved one flips to claimed and
	// gives up its token in the same statement. The token is read from the
	// locked pre-update row, because RETURNING would only see the cleared value.
	query := `
		WITH picked AS (
			SELECT id, instance_token
			FROM pool_link_codes
			WHERE device_code_hash = $1 AND status = 'approved' AND expires_at > NOW()
			FOR UPDATE
		), claimed AS (
			UPDATE pool_link_codes p
			SET status = 'claimed', instance_token = NULL
			FROM picked
			WHERE p.id = picked.id
			RETURNING p.id, p.user_code, p.instance_name, p.instance_url, p.instance_version, p.status, p.organization_id, p.instance_id, p.expires_at, p.created_at, picked.instance_token AS token
		)
		SELECT id, user_code, instance_name, instance_url, instance_version, status, organization_id, instance_id, expires_at, created_at, COALESCE(token, '') FROM claimed
		UNION ALL
		SELECT ` + poolLinkCodeColumns + `, '' FROM pool_link_codes
		WHERE device_code_hash = $1 AND expires_at > NOW() AND NOT EXISTS (SELECT 1 FROM claimed)
		LIMIT 1
	`
	var c models.PoolLinkCode
	var token string
	err := r.db.QueryRow(ctx, query, deviceCodeHash).Scan(&c.ID, &c.UserCode, &c.InstanceName, &c.InstanceURL, &c.InstanceVersion, &c.Status, &c.OrganizationID, &c.InstanceID, &c.ExpiresAt, &c.CreatedAt, &token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		db.CaptureError(err, query, nil, "queryrow")
		return nil, "", err
	}
	// The claimed row reports its new status; the caller wants "approved".
	if token != "" {
		c.Status = models.PoolLinkCodeApproved
	}
	return &c, token, nil
}

func (r *poolLinkRepository) DeleteExpiredCodes(ctx context.Context) error {
	_, err := r.db.Exec(ctx, `DELETE FROM pool_link_codes WHERE expires_at < NOW() - INTERVAL '1 day'`)
	return err
}

const poolLinkInstanceColumns = `id, organization_id, name, url, version, created_by, created_at, last_seen_at, revoked_at`

func scanPoolLinkInstance(row pgx.Row) (*models.PoolLinkInstance, error) {
	var i models.PoolLinkInstance
	if err := row.Scan(&i.ID, &i.OrganizationID, &i.Name, &i.URL, &i.Version, &i.CreatedBy, &i.CreatedAt, &i.LastSeenAt, &i.RevokedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &i, nil
}

func (r *poolLinkRepository) CreateInstance(ctx context.Context, inst *models.PoolLinkInstance, tokenHash string) error {
	query := `
		INSERT INTO pool_link_instances (id, organization_id, name, url, version, token_hash, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`
	if err := r.db.QueryRow(ctx, query, inst.ID, inst.OrganizationID, inst.Name, inst.URL, inst.Version, tokenHash, inst.CreatedBy).Scan(&inst.CreatedAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	return nil
}

func (r *poolLinkRepository) GetInstanceByTokenHash(ctx context.Context, tokenHash string) (*models.PoolLinkInstance, error) {
	query := `SELECT ` + poolLinkInstanceColumns + ` FROM pool_link_instances WHERE token_hash = $1 AND revoked_at IS NULL`
	i, err := scanPoolLinkInstance(r.db.QueryRow(ctx, query, tokenHash))
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return i, nil
}

func (r *poolLinkRepository) GetInstance(ctx context.Context, id uuid.UUID) (*models.PoolLinkInstance, error) {
	query := `SELECT ` + poolLinkInstanceColumns + ` FROM pool_link_instances WHERE id = $1`
	i, err := scanPoolLinkInstance(r.db.QueryRow(ctx, query, id))
	if err != nil {
		db.CaptureError(err, query, []any{id}, "queryrow")
		return nil, err
	}
	return i, nil
}

func (r *poolLinkRepository) ListInstances(ctx context.Context, orgID uuid.UUID) ([]models.PoolLinkInstance, error) {
	query := `
		SELECT i.id, i.organization_id, i.name, i.url, i.version, i.created_by, i.created_at, i.last_seen_at, i.revoked_at,
		       (SELECT COUNT(*) FROM pool_link_mailboxes m WHERE m.instance_id = i.id)
		FROM pool_link_instances i
		WHERE i.organization_id = $1 AND i.revoked_at IS NULL
		ORDER BY i.created_at
	`
	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.PoolLinkInstance{}
	for rows.Next() {
		var i models.PoolLinkInstance
		if err := rows.Scan(&i.ID, &i.OrganizationID, &i.Name, &i.URL, &i.Version, &i.CreatedBy, &i.CreatedAt, &i.LastSeenAt, &i.RevokedAt, &i.MailboxCount); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (r *poolLinkRepository) TouchInstance(ctx context.Context, id uuid.UUID, version string) error {
	query := `UPDATE pool_link_instances SET last_seen_at = NOW(), version = CASE WHEN $2 = '' THEN version ELSE $2 END WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id, version)
	return err
}

func (r *poolLinkRepository) RevokeInstance(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE pool_link_instances SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`
	if _, err := r.db.Exec(ctx, query, id); err != nil {
		db.CaptureError(err, query, []any{id}, "exec")
		return err
	}
	return nil
}

func (r *poolLinkRepository) HasActiveInstance(ctx context.Context, orgID uuid.UUID) (bool, error) {
	var ok bool
	query := `SELECT EXISTS (SELECT 1 FROM pool_link_instances WHERE organization_id = $1 AND revoked_at IS NULL)`
	if err := r.db.QueryRow(ctx, query, orgID).Scan(&ok); err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return false, err
	}
	return ok, nil
}

func (r *poolLinkRepository) EnrollMailbox(ctx context.Context, m *models.PoolLinkMailbox) error {
	query := `
		INSERT INTO pool_link_mailboxes (instance_id, remote_id, email_account_id)
		VALUES ($1, $2, $3)
		RETURNING enrolled_at
	`
	if err := r.db.QueryRow(ctx, query, m.InstanceID, m.RemoteID, m.EmailAccountID).Scan(&m.EnrolledAt); err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return err
	}
	return nil
}

func scanPoolLinkMailbox(row pgx.Row) (*models.PoolLinkMailbox, error) {
	var m models.PoolLinkMailbox
	if err := row.Scan(&m.InstanceID, &m.RemoteID, &m.EmailAccountID, &m.EnrolledAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *poolLinkRepository) GetMailboxByRemote(ctx context.Context, instanceID, remoteID uuid.UUID) (*models.PoolLinkMailbox, error) {
	query := `SELECT instance_id, remote_id, email_account_id, enrolled_at FROM pool_link_mailboxes WHERE instance_id = $1 AND remote_id = $2`
	m, err := scanPoolLinkMailbox(r.db.QueryRow(ctx, query, instanceID, remoteID))
	if err != nil {
		db.CaptureError(err, query, nil, "queryrow")
		return nil, err
	}
	return m, nil
}

func (r *poolLinkRepository) GetMailboxByAccount(ctx context.Context, accountID uuid.UUID) (*models.PoolLinkMailbox, error) {
	query := `SELECT instance_id, remote_id, email_account_id, enrolled_at FROM pool_link_mailboxes WHERE email_account_id = $1`
	m, err := scanPoolLinkMailbox(r.db.QueryRow(ctx, query, accountID))
	if err != nil {
		db.CaptureError(err, query, []any{accountID}, "queryrow")
		return nil, err
	}
	return m, nil
}

func (r *poolLinkRepository) ListMailboxes(ctx context.Context, instanceID uuid.UUID) ([]models.PoolLinkMailbox, error) {
	query := `SELECT instance_id, remote_id, email_account_id, enrolled_at FROM pool_link_mailboxes WHERE instance_id = $1 ORDER BY enrolled_at`
	rows, err := r.db.Query(ctx, query, instanceID)
	if err != nil {
		db.CaptureError(err, query, []any{instanceID}, "query")
		return nil, err
	}
	defer rows.Close()
	out := []models.PoolLinkMailbox{}
	for rows.Next() {
		var m models.PoolLinkMailbox
		if err := rows.Scan(&m.InstanceID, &m.RemoteID, &m.EmailAccountID, &m.EnrolledAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *poolLinkRepository) CountMailboxesForOrganization(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	query := `
		SELECT COUNT(*) FROM pool_link_mailboxes m
		JOIN pool_link_instances i ON i.id = m.instance_id
		WHERE i.organization_id = $1 AND i.revoked_at IS NULL
	`
	if err := r.db.QueryRow(ctx, query, orgID).Scan(&n); err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return 0, err
	}
	return n, nil
}

func (r *poolLinkRepository) DeleteMailbox(ctx context.Context, instanceID, remoteID uuid.UUID) error {
	query := `DELETE FROM pool_link_mailboxes WHERE instance_id = $1 AND remote_id = $2`
	if _, err := r.db.Exec(ctx, query, instanceID, remoteID); err != nil {
		db.CaptureError(err, query, nil, "exec")
		return err
	}
	return nil
}
