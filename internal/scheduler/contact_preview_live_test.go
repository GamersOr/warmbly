package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Live checks for the contact drawer's next-action preview (issue #255). The
// preview must say what the send path would do, so every case here is proven
// against the real routing and placement queries rather than a stub.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/scheduler/ -run LivePreview -v

func livePreviewer(t *testing.T, s SchedulerService) ContactSendPreviewer {
	t.Helper()
	p, ok := s.(ContactSendPreviewer)
	if !ok {
		t.Fatal("scheduler service does not preview contact sends")
	}
	return p
}

func liveLead(t *testing.T, pool *pgxpool.Pool, campaign uuid.UUID) (step1, contact uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx,
		`SELECT id FROM sequences WHERE campaign_id = $1 AND position = 0`, campaign).Scan(&step1); err != nil {
		t.Fatalf("load step 1: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT contact_id FROM campaign_leads WHERE campaign_id = $1`, campaign).Scan(&contact); err != nil {
		t.Fatalf("load contact: %v", err)
	}
	return step1, contact
}

// A new lead in an always-open campaign is due now: the preview names the
// entry step and carries the slot the scheduler would give it.
func TestLivePreviewDueLeadGetsASlot(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	step1, contact := liveLead(t, pool, f.campaign)

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(context.Background(), f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Route.Target == nil || *pv.Route.Target != step1 || !pv.Route.IsNewLead {
		t.Fatalf("want the entry step for a new lead, got route %+v", pv.Route)
	}
	if pv.State != models.NextActionDue || pv.ScheduledAt == nil {
		t.Fatalf("want a due step with a slot, got state=%q scheduled_at=%v constraint=%q", pv.State, pv.ScheduledAt, pv.Constraint)
	}
	if pv.ScheduledAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("slot %s is in the past", pv.ScheduledAt)
	}
}

// A follow-up inside its wait must not be promised a time: the preview reports
// the step, the wait as the constraint, and an honest not-before.
func TestLivePreviewFollowUpWaitIsReported(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)

	step2 := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after, position, kind)
		VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', 3, 1, 'email')`,
		step2, f.campaign, f.org); err != nil {
		t.Fatalf("insert step 2: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE sequences SET conditions = jsonb_build_object('branches', jsonb_build_array(
			jsonb_build_object('branch_id', 'live-else', 'target_step_id', $1::text)))
		WHERE campaign_id = $2 AND position = 0`, step2.String(), f.campaign); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at)
		VALUES ($1, $2, $3, NOW())`, f.campaign, contact, step1); err != nil {
		t.Fatalf("stamp step 1 sent: %v", err)
	}

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(ctx, f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Route.Target == nil || *pv.Route.Target != step2 {
		t.Fatalf("want step 2 routed next, got %+v", pv.Route)
	}
	if pv.State != models.NextActionWaiting || pv.Constraint != ConstraintStepWait {
		t.Fatalf("want waiting on the step wait, got state=%q constraint=%q", pv.State, pv.Constraint)
	}
	if pv.ScheduledAt != nil {
		t.Fatalf("a waiting step must not promise a slot, got %s", pv.ScheduledAt)
	}
	if pv.NotBefore == nil || time.Until(*pv.NotBefore) < 47*time.Hour {
		t.Fatalf("not-before %v is too soon for a 3-day wait", pv.NotBefore)
	}
}

// A lead whose campaign is outside its sending window right now: the preview
// says so, and its not-before lands inside the next window instead of
// inventing a time in the closed one.
func TestLivePreviewSendingWindowBlocksTheStep(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	_, contact := liveLead(t, pool, f.campaign)

	// A one-hour window that opens three hours from now. Near midnight the
	// window would wrap, so it is pushed to 01:00-02:00 tomorrow instead.
	now := time.Now().UTC()
	startHour := (now.Hour() + 3) % 24
	if startHour >= 23 || startHour < now.Hour() {
		startHour = 1
	}
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET start_time = $2, end_time = $3 WHERE id = $1`,
		f.campaign, fmt.Sprintf("%02d:00", startHour), fmt.Sprintf("%02d:00", startHour+1)); err != nil {
		t.Fatalf("narrow window: %v", err)
	}

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(ctx, f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.State != models.NextActionWaiting || pv.Constraint != ConstraintSendingWindow {
		t.Fatalf("want waiting on the sending window, got state=%q constraint=%q scheduled_at=%v", pv.State, pv.Constraint, pv.ScheduledAt)
	}
	if pv.ScheduledAt != nil {
		t.Fatalf("a window-blocked step must not promise a slot, got %s", pv.ScheduledAt)
	}
	if pv.NotBefore == nil {
		t.Fatal("want a not-before inside the next window")
	}
	until := time.Until(*pv.NotBefore)
	if until < 90*time.Minute || until > 27*time.Hour {
		t.Fatalf("not-before %s away does not sit in the next window", until.Round(time.Minute))
	}
	t.Logf("window opens in ~%s; preview says not before %s", until.Round(time.Minute), pv.NotBefore.UTC().Format(time.Kitchen))
}

// A paused campaign schedules nothing, whatever the step says.
func TestLivePreviewPausedCampaignHasNoSlot(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	ctx := context.Background()
	step1, contact := liveLead(t, pool, f.campaign)
	if _, err := pool.Exec(ctx, `UPDATE campaigns SET status = 'paused' WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("pause: %v", err)
	}

	pv, err := livePreviewer(t, liveScheduler(t, handle, pool)).PreviewContactSend(ctx, f.campaign, contact)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if pv.Route.Target == nil || *pv.Route.Target != step1 {
		t.Fatalf("routing must still name the step, got %+v", pv.Route)
	}
	if pv.State != models.NextActionPaused || pv.Constraint != ConstraintCampaignInactive || pv.ScheduledAt != nil {
		t.Fatalf("want paused with no slot, got state=%q constraint=%q scheduled_at=%v", pv.State, pv.Constraint, pv.ScheduledAt)
	}
}
