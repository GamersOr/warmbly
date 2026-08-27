package warmup

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks of warmup health evaluation against a real Postgres. Skipped
// unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/warmup/ -run Live -v
//
// Issue #195: the invalid-token band never fired. GetParticipantHealth compared
// the driver's not-found error with `err == sql.ErrNoRows`, but the pool is pgx,
// whose ErrNoRows is a proxy error that wraps sql.ErrNoRows rather than being
// it. The comparison was therefore always false and "this account is not in
// this pool" surfaced as a hard error. evaluateAndPersistAnyPool probes
// "premium" first, so for every free-pool account the very first probe failed
// and the account's own pool was never reached: no evaluation, ever.
//
// The pool probe is pure SQL and the whole failure lives in the driver
// boundary, so it only reproduces against a real database.

func liveWarmupRepo(t *testing.T) (repository.WarmupRepository, *db.DB) {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	handle, err := db.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })
	return repository.NewWarmupRepository(handle.Pool), handle
}

// freePoolAccount is a mailbox that participates in the free warmup pool and
// has no premium row, which is the shape every account on a self-hosted install
// has.
type freePoolAccount struct {
	user, org, account uuid.UUID
}

func newFreePoolAccount(t *testing.T, handle *db.DB) *freePoolAccount {
	t.Helper()
	ctx := context.Background()
	pool := handle.Pool

	f := &freePoolAccount{user: uuid.New(), org: uuid.New(), account: uuid.New()}

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Health', 'Live')`,
		f.user, "wh-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Warmup Health Live', $2, $3)`,
		f.org, "wh-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Health', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		f.account, f.user, f.org, "wh-"+f.account.String()[:8]+"@test.local")

	// Free pool only. No premium row, which is the whole point.
	exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id)
	      SELECT id, $1 FROM warmup_pools WHERE pool_type = 'free'`, f.account)

	t.Cleanup(func() {
		c := context.Background()
		for _, s := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_invalid_token_attempts WHERE email_account_id = $1`, f.account},
			{`DELETE FROM warmup_pool_participants WHERE email_account_id = $1`, f.account},
			{`DELETE FROM email_accounts WHERE id = $1`, f.account},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, s.sql, s.arg); err != nil {
				t.Errorf("cleanup %q: %v", s.sql, err)
			}
		}
	})

	return f
}

// The driver-boundary bug itself: "not in this pool" is not an error.
func TestLiveGetParticipantHealthReportsAbsenceNotFailure(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)

	health, err := repo.GetParticipantHealth(context.Background(), f.account, "premium")
	if err != nil {
		t.Fatalf("premium probe returned an error for an account that is simply not in that pool: %v", err)
	}
	if health != nil {
		t.Fatal("expected no premium participant")
	}

	health, err = repo.GetParticipantHealth(context.Background(), f.account, "free")
	if err != nil {
		t.Fatalf("free probe: %v", err)
	}
	if health == nil {
		t.Fatal("expected the free participant row")
	}
}

// What the absence-as-error bug cost: the band that is supposed to act on
// invalid tokens never ran for any free-pool account.
func TestLiveInvalidTokenBandFiresForAFreePoolAccount(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	svc := NewService(repo)
	ctx := context.Background()

	for i := 0; i < invalidTokenBlockThreshold; i++ {
		if _, err := svc.ApplyInvalidTokenAttempt(ctx, f.account, uuid.NewString(), 0); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}

	attempts, err := repo.CountRecentInvalidAttempts(ctx, f.account, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("count attempts: %v", err)
	}
	if attempts != invalidTokenBlockThreshold {
		t.Fatalf("recorded %d attempts, want %d (a second recording means the caller's degraded path ran)",
			attempts, invalidTokenBlockThreshold)
	}

	health, err := repo.GetParticipantHealth(ctx, f.account, "free")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if health.LastHealthEvaluatedAt == nil {
		t.Fatal("last_health_evaluated_at is still null: the evaluation never ran")
	}
	if health.HealthState != models.WarmupHealthBlocked {
		t.Fatalf("health_state is %q after %d invalid attempts, want %q",
			health.HealthState, attempts, models.WarmupHealthBlocked)
	}
}

// EvaluateAllParticipants is the hourly sweep, and the same absence-as-error
// made it skip every free-pool account without a word.
func TestLiveSweepEvaluatesAFreePoolAccount(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	svc := NewService(repo)
	ctx := context.Background()

	evaluated, _, xerr := svc.EvaluateAllParticipants(ctx)
	if xerr != nil {
		t.Fatalf("sweep: %v", xerr)
	}
	if evaluated == 0 {
		t.Fatal("the sweep evaluated nobody")
	}

	health, err := repo.GetParticipantHealth(ctx, f.account, "free")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if health.LastHealthEvaluatedAt == nil {
		t.Fatal("the sweep skipped this account: last_health_evaluated_at is still null")
	}
}

// The cutover floor (migration 000096): a mailbox is not judged on signals from
// before it was being judged. Without it, the first evaluation after this fix
// would reach back over everything collected while nothing was watching,
// including the spurious Junk placements Graph produced before #199 and #201.
func TestLiveHealthSignalsBeforeTheFloorAreNotCounted(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newFreePoolAccount(t, handle)
	svc := NewService(repo)
	ctx := context.Background()

	// Well over the block threshold, but all of it predates the floor.
	for i := 0; i < invalidTokenBlockThreshold*4; i++ {
		if _, err := handle.Pool.Exec(ctx,
			`INSERT INTO warmup_invalid_token_attempts (email_account_id, attempted_token, created_at)
			 VALUES ($1, $2, NOW() - INTERVAL '2 hours')`,
			f.account, uuid.NewString()); err != nil {
			t.Fatalf("backdated attempt: %v", err)
		}
	}
	if _, err := handle.Pool.Exec(ctx,
		`UPDATE warmup_pool_participants SET health_signals_from = NOW() - INTERVAL '1 hour'
		 WHERE email_account_id = $1`, f.account); err != nil {
		t.Fatalf("set floor: %v", err)
	}

	health, err := svc.(*service).evaluateAndPersistAnyPool(ctx, f.account)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if health.HealthState != models.WarmupHealthHealthy {
		t.Fatalf("health_state is %q; attempts from before the floor were counted against the mailbox",
			health.HealthState)
	}

	// The same volume of attempts after the floor does block it.
	for i := 0; i < invalidTokenBlockThreshold; i++ {
		if _, err := svc.ApplyInvalidTokenAttempt(ctx, f.account, uuid.NewString(), 0); err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
	}
	health, err = svc.(*service).evaluateAndPersistAnyPool(ctx, f.account)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if health.HealthState != models.WarmupHealthBlocked {
		t.Fatalf("health_state is %q after %d attempts past the floor, want %q",
			health.HealthState, invalidTokenBlockThreshold, models.WarmupHealthBlocked)
	}
}
