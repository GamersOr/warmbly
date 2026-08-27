package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Issue #209: six admin queries named schema that does not exist, so each one
// failed on every call. The prepare sweep (TestLiveEveryQueryPrepares) proves a
// statement CAN run; these tests prove the fixed statements do the right thing.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAdmin -v

// adminFixture is one user with one organization, one campaign and one mailbox
// that has never synced.
type adminFixture struct {
	org      uuid.UUID
	user     uuid.UUID
	campaign uuid.UUID
	mailbox  uuid.UUID
	tag      string
}

func newAdminFixture(t *testing.T, pool *pgxpool.Pool) *adminFixture {
	t.Helper()
	ctx := context.Background()
	f := &adminFixture{
		org:      uuid.New(),
		user:     uuid.New(),
		campaign: uuid.New(),
		mailbox:  uuid.New(),
	}
	f.tag = "i209-" + f.org.String()[:8]

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Nadia', 'Live', $2, 'x')`, f.user, f.tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Issue 209', $2, $3)`, f.org, f.tag, f.user)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
	      VALUES ($1, $2, 'owner', NOW())`, f.org, f.user)
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Issue 209 outreach', '', 62, 'active', NOW(), NOW())`,
		f.campaign, f.user, f.org)
	// last_synced_at stays NULL: a mailbox that has been connected but never
	// synced is the common case right after setup, and it used to break the
	// scan for the whole list.
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Nadia', '', '', 'smtp_imap')`,
		f.mailbox, f.user, f.org, f.tag+"-mb@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_logs WHERE campaign_id IN (SELECT id FROM campaigns WHERE organization_id = $1)`, f.org},
			{`DELETE FROM campaigns WHERE organization_id = $1`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organization_members WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM user_rate_limits WHERE user_id = $1`, f.user},
			{`DELETE FROM admin_audit_logs WHERE admin_user_id = $1`, f.user},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// ensureDuration returns the id of the durations row with this title, creating
// it for the duration of the test if the instance does not have one.
func ensureDuration(t *testing.T, pool *pgxpool.Pool, title string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var id uuid.UUID
	err := pool.QueryRow(ctx, `SELECT id FROM durations WHERE title = $1`, title).Scan(&id)
	if err == nil {
		return id
	}
	id = uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO durations (id, title) VALUES ($1, $2)`, id, title); err != nil {
		t.Fatalf("create durations row %q: %v", title, err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM durations WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup durations row %q: %v", title, err)
		}
	})
	return id
}

// The email-account section of the user preview compared a uuid column against
// a text parameter, so the query errored and the caller swallowed it: every
// user looked like they had no mailboxes.
func TestLiveAdminUserPreviewListsMailboxes(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)

	preview, err := repo.GetUserPreview(context.Background(), f.user)
	if err != nil {
		t.Fatalf("GetUserPreview: %v", err)
	}
	if preview == nil {
		t.Fatal("GetUserPreview returned no preview for an existing user")
	}
	if len(preview.EmailAccounts) != 1 || preview.EmailAccounts[0].ID != f.mailbox {
		t.Fatalf("preview lists %d mailbox(es), want the one that was connected", len(preview.EmailAccounts))
	}
	if preview.EmailAccounts[0].LastSyncedAt != nil {
		t.Fatalf("a mailbox that never synced reads last_synced_at = %v, want nil", preview.EmailAccounts[0].LastSyncedAt)
	}
	if len(preview.Organizations) != 1 || preview.Organizations[0].ID != f.org {
		t.Fatalf("preview lists %d organization(s), want 1", len(preview.Organizations))
	}

	// The paginated mailbox list behind the user detail page compared the same
	// uuid column against a text parameter, and that one surfaced as a 500.
	emails, pagination, err := repo.GetUserEmails(context.Background(), f.user, nil, 50)
	if err != nil {
		t.Fatalf("GetUserEmails: %v", err)
	}
	if len(emails) != 1 || emails[0].ID != f.mailbox {
		t.Fatalf("GetUserEmails returned %d mailbox(es), want the one that was connected", len(emails))
	}
	if pagination == nil || pagination.HasMore {
		t.Fatalf("pagination = %+v, want a single complete page", pagination)
	}
}

// A force-stop wrote status = 'stopped', which campaign_status has never had,
// and stopped_at, which campaigns has never had.
func TestLiveAdminForceStopPausesCampaign(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	stopped, err := repo.StopCampaign(ctx, f.campaign)
	if err != nil {
		t.Fatalf("StopCampaign: %v", err)
	}
	if !stopped {
		t.Fatal("StopCampaign refused an active campaign")
	}

	var status string
	var changedAt *time.Time
	if err = pool.QueryRow(ctx,
		`SELECT status::text, last_status_change_at FROM campaigns WHERE id = $1`, f.campaign,
	).Scan(&status, &changedAt); err != nil {
		t.Fatalf("read campaign back: %v", err)
	}
	if status != "paused" {
		t.Fatalf("campaign status = %q after a force-stop, want %q", status, "paused")
	}
	if changedAt == nil {
		t.Fatal("force-stop left last_status_change_at unset, so the owner's stop cooldown never starts")
	}

	// The scheduler only runs campaigns that are still 'active', which is what
	// makes the status flip enough to halt the send loop.
	detail, err := repo.GetCampaignDetail(ctx, f.campaign)
	if err != nil {
		t.Fatalf("GetCampaignDetail: %v", err)
	}
	if detail == nil || detail.Status != "paused" {
		t.Fatalf("campaign detail reports %v, want a paused campaign", detail)
	}
}

// A campaign that reaches a terminal state while the operator is deciding must
// not be dragged back out of it, so the eligibility test lives in the UPDATE.
func TestLiveAdminForceStopLeavesTerminalCampaignsAlone(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	for _, status := range []string{"completed", "draft"} {
		if _, err := pool.Exec(ctx,
			`UPDATE campaigns SET status = $2::campaign_status WHERE id = $1`, f.campaign, status,
		); err != nil {
			t.Fatalf("park campaign at %s: %v", status, err)
		}

		stopped, err := repo.StopCampaign(ctx, f.campaign)
		if err != nil {
			t.Fatalf("StopCampaign on a %s campaign: %v", status, err)
		}
		if stopped {
			t.Fatalf("StopCampaign reported that it stopped a %s campaign", status)
		}

		var got string
		if err := pool.QueryRow(ctx, `SELECT status::text FROM campaigns WHERE id = $1`, f.campaign).Scan(&got); err != nil {
			t.Fatalf("read campaign back: %v", err)
		}
		if got != status {
			t.Fatalf("a %s campaign is now %q; the stop overwrote a state it had no business touching", status, got)
		}
	}
}

// plans stores the billing period as duration_id (FK to durations); the admin
// writes named a `duration` column that does not exist.
func TestLiveAdminPlanRoundTripsDuration(t *testing.T) {
	_, pool := liveContactDB(t)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	// A bare install ships only the monthly duration (migration 000080); the
	// yearly one arrives with the seed. Add whatever is missing and take it
	// back out again, so this runs on either.
	monthID := ensureDuration(t, pool, "month")
	yearID := ensureDuration(t, pool, "year")

	resolved, err := repo.DurationIDByTitle(ctx, "month")
	if err != nil {
		t.Fatalf("DurationIDByTitle(month): %v", err)
	}
	if resolved == nil || *resolved != monthID {
		t.Fatalf("DurationIDByTitle(month) = %v, want %v", resolved, monthID)
	}
	if unknown, err := repo.DurationIDByTitle(ctx, "fortnight"); err != nil || unknown != nil {
		t.Fatalf("DurationIDByTitle(fortnight) = %v, %v; want nil, nil so the API can answer 400", unknown, err)
	}

	name := "Issue 209 plan"
	plan := &models.Plan{
		ID:             uuid.New(),
		Name:           &name,
		MaxContacts:    1000,
		DailyEmails:    50,
		AccountLimit:   3,
		Price:          49,
		Duration:       models.DurationMonth,
		MonthlyCredits: 25,
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM plans WHERE id = $1`, plan.ID); err != nil {
			t.Errorf("cleanup plan: %v", err)
		}
	})

	if err := repo.CreatePlan(ctx, plan, monthID); err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	got, err := repo.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}
	if got == nil {
		t.Fatal("GetPlan found nothing for a plan that was just created")
	}
	if got.Duration != models.DurationMonth {
		t.Fatalf("plan reads back duration %q, want %q", got.Duration, models.DurationMonth)
	}
	if got.MonthlyCredits != 25 {
		t.Fatalf("plan reads back %d monthly credits, want 25", got.MonthlyCredits)
	}

	got.Duration = models.DurationYear
	got.Price = 490
	if err := repo.UpdatePlan(ctx, got, yearID); err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	after, err := repo.GetPlan(ctx, plan.ID)
	if err != nil {
		t.Fatalf("GetPlan after update: %v", err)
	}
	if after.Duration != models.DurationYear {
		t.Fatalf("plan reads back duration %q after switching to yearly, want %q", after.Duration, models.DurationYear)
	}
	if after.Price != 490 {
		t.Fatalf("plan reads back price %v, want 490", after.Price)
	}
}

// user_rate_limits has limit_api_calls_daily / limit_bulk_ops_daily and no
// daily_email_limit, so both the read and the write failed every time.
func TestLiveAdminUserRateLimitsRoundTrip(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	existing, err := repo.GetUserRateLimits(ctx, f.user)
	if err != nil {
		t.Fatalf("GetUserRateLimits before any override: %v", err)
	}
	if existing != nil {
		t.Fatalf("a fresh user already has an override row: %+v", existing)
	}

	// A partial patch is the normal case, and it has to create the row without
	// tripping the NOT NULL constraint on every column it does not mention.
	writes := 42
	if err := repo.UpdateUserRateLimits(ctx, f.user, f.user, &models.UpdateUserRateLimitsRequest{
		LimitWritePM: &writes,
	}); err != nil {
		t.Fatalf("UpdateUserRateLimits (first, partial): %v", err)
	}

	limits, err := repo.GetUserRateLimits(ctx, f.user)
	if err != nil {
		t.Fatalf("GetUserRateLimits: %v", err)
	}
	if limits == nil {
		t.Fatal("no override row after a successful update")
	}
	if limits.LimitWritePM != writes {
		t.Fatalf("limit_write_pm = %d, want %d", limits.LimitWritePM, writes)
	}
	if limits.LimitReadPM == 0 || limits.LimitAPICallsDaily == 0 || limits.MaxConnections == 0 {
		t.Fatalf("a partial patch zeroed the untouched columns: %+v", limits)
	}
	if limits.UpdatedBy == nil || *limits.UpdatedBy != f.user {
		t.Fatalf("updated_by = %v, want the acting admin", limits.UpdatedBy)
	}

	// A second patch must leave the first one alone.
	daily := 7
	if err := repo.UpdateUserRateLimits(ctx, f.user, f.user, &models.UpdateUserRateLimitsRequest{
		LimitBulkOpsDaily: &daily,
	}); err != nil {
		t.Fatalf("UpdateUserRateLimits (second, partial): %v", err)
	}
	after, err := repo.GetUserRateLimits(ctx, f.user)
	if err != nil {
		t.Fatalf("GetUserRateLimits after second patch: %v", err)
	}
	if after.LimitBulkOpsDaily != daily {
		t.Fatalf("limit_bulk_ops_daily = %d, want %d", after.LimitBulkOpsDaily, daily)
	}
	if after.LimitWritePM != writes {
		t.Fatalf("the second patch reset limit_write_pm to %d, want %d", after.LimitWritePM, writes)
	}
}
