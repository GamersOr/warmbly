package orgrisk

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func sig(weight int, detail string) map[string]any {
	return map[string]any{"weight": float64(weight), "detail": detail}
}

func TestBandFor(t *testing.T) {
	for _, tt := range []struct {
		score int
		want  models.OrgRiskState
	}{
		{0, models.OrgRiskTrusted},
		{WatchScore - 1, models.OrgRiskTrusted},
		{WatchScore, models.OrgRiskWatch},
		{RestrictedScore - 1, models.OrgRiskWatch},
		{RestrictedScore, models.OrgRiskRestricted},
		{SuspendedScore - 1, models.OrgRiskRestricted},
		{SuspendedScore, models.OrgRiskSuspended},
		{100, models.OrgRiskSuspended},
	} {
		if got := BandFor(tt.score); got != tt.want {
			t.Errorf("BandFor(%d) = %q, want %q", tt.score, got, tt.want)
		}
	}
}

func TestScoreFusesSignalsAndCaps(t *testing.T) {
	if got := Score(nil); got != 0 {
		t.Errorf("Score(nil) = %d, want 0", got)
	}
	// The whole point: several individually-tolerable signals add up to a band
	// none of them would reach alone.
	signals := map[string]any{
		"signup_email_risk": sig(20, "disposable signup domain"),
		"list_quality":      sig(20, "imported list is 30% invalid"),
		"placement":         sig(20, "mailboxes landing in spam"),
	}
	if got := Score(signals); got != 60 {
		t.Errorf("Score() = %d, want 60", got)
	}
	if got := BandFor(Score(signals)); got != models.OrgRiskRestricted {
		t.Errorf("three tolerable signals should restrict, got %q", got)
	}

	signals["more"] = sig(80, "and another")
	if got := Score(signals); got != 100 {
		t.Errorf("Score() = %d, want it capped at 100", got)
	}
}

func TestScoreIgnoresMalformedEvidence(t *testing.T) {
	// The blob is operator-visible and hand-editable; a bad entry must not
	// panic or silently score as something arbitrary.
	signals := map[string]any{
		"good":       sig(30, "real"),
		"a string":   "not an object",
		"no weight":  map[string]any{"detail": "missing weight"},
		"bad weight": map[string]any{"weight": "heavy", "detail": "text weight"},
		"null":       nil,
	}
	if got := Score(signals); got != 30 {
		t.Errorf("Score() = %d, want only the well-formed 30", got)
	}
}

func TestReasonIsStableAndHeaviestFirst(t *testing.T) {
	signals := map[string]any{
		"a": sig(10, "light thing"),
		"b": sig(40, "heavy thing"),
		"c": sig(25, "middling thing"),
		"d": sig(5, "trivial thing"),
	}
	want := "heavy thing; middling thing; light thing"
	// Map iteration order is random, so run it enough that an unsorted
	// implementation would be caught rather than passing by luck.
	for i := 0; i < 50; i++ {
		if got := Reason(signals); got != want {
			t.Fatalf("Reason() = %q, want %q", got, want)
		}
	}
	if got := Reason(nil); got != "" {
		t.Errorf("Reason(nil) = %q, want empty", got)
	}
}

func TestBandEffects(t *testing.T) {
	if models.OrgRiskTrusted.CapMultiplier() != 1 || models.OrgRiskWatch.CapMultiplier() != 1 {
		t.Error("watch must not change what a customer can feel")
	}
	if models.OrgRiskWatch.ForcesFreeWarmupPool() || models.OrgRiskWatch.BlocksSending() {
		t.Error("watch must not restrict anything")
	}
	if !models.OrgRiskRestricted.ForcesFreeWarmupPool() || models.OrgRiskRestricted.BlocksSending() {
		t.Error("restricted lowers volume and leaves the paid pool, but still sends")
	}
	if !models.OrgRiskSuspended.BlocksSending() || models.OrgRiskSuspended.CapMultiplier() != 0 {
		t.Error("suspended must stop sending")
	}
}

type recordingAudit struct {
	entries []string
}

func (r *recordingAudit) LogAction(_ context.Context, orgID, _ uuid.UUID, _ models.AuditAction,
	entityType models.AuditEntityType, _ *uuid.UUID, _, _ string, changes, _ map[string]string) {
	r.entries = append(r.entries, string(entityType)+":"+changes["risk_state"])
	_ = orgID
}

type stubRiskRepo struct {
	risk *models.OrgRisk
}

func (s *stubRiskRepo) GetOrgRisk(context.Context, uuid.UUID) (*models.OrgRisk, error) {
	snapshot := *s.risk
	return &snapshot, nil
}
func (s *stubRiskRepo) GetOrgRiskStates(context.Context, []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	return nil, nil
}

// write models what the UPDATE does: the derived score always lands, but the
// band only does when no operator pin outranks it.
func (s *stubRiskRepo) write(derive repository.OrgRiskDerive) *models.OrgRisk {
	signals, state, score, reason := derive(s.risk.Signals)
	s.risk.Signals, s.risk.Score = signals, score
	if s.risk.Override != nil {
		s.risk.State = s.risk.Override.State
		if s.risk.Override.Reason != "" {
			reason = s.risk.Override.Reason
		}
	} else {
		s.risk.State = state
	}
	s.risk.Reason = reason
	snapshot := *s.risk
	return &snapshot
}

func (s *stubRiskRepo) UpdateOrgRiskSignals(_ context.Context, _ uuid.UUID, derive repository.OrgRiskDerive) (*models.OrgRisk, error) {
	return s.write(derive), nil
}
func (s *stubRiskRepo) ClearOrgRiskOverride(_ context.Context, _ uuid.UUID, derive repository.OrgRiskDerive) (*models.OrgRisk, error) {
	s.risk.Override = nil
	return s.write(derive), nil
}
func (s *stubRiskRepo) OrgsWithSignal(context.Context, string) ([]uuid.UUID, error) {
	return nil, nil
}
func (s *stubRiskRepo) OrgsWithExpiringSignals(context.Context) ([]uuid.UUID, error) {
	return []uuid.UUID{s.risk.OrganizationID}, nil
}
func (s *stubRiskRepo) SetOrgRiskOverride(_ context.Context, _ uuid.UUID, state models.OrgRiskState, reason string, by uuid.UUID) (*models.OrgRisk, error) {
	actor := by
	s.risk.State, s.risk.Reason = state, reason
	s.risk.Override = &models.OrgRiskOverride{State: state, Reason: reason, By: &actor}
	snapshot := *s.risk
	return &snapshot, nil
}

// A transition has to reach the audit spine, or the banner never updates for a
// teammate and there is no trail of who was restricted when.
func TestTransitionsAreAudited(t *testing.T) {
	repo := &stubRiskRepo{risk: &models.OrgRisk{State: models.OrgRiskTrusted, Signals: map[string]any{}}}
	rec := &recordingAudit{}
	svc := NewService(repo)
	svc.(AuditAware).WireAudit(rec)

	org := uuid.New()
	if _, err := svc.RecordSignal(context.Background(), org, Signal{Key: "a", Weight: 30, Detail: "something"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if len(rec.entries) != 1 || rec.entries[0] != "org_risk:trusted -> watch" {
		t.Fatalf("entries = %v, want one trusted -> watch", rec.entries)
	}

	// A detector re-recording the same finding is not a transition and must not
	// fill the feed with no-ops.
	if _, err := svc.RecordSignal(context.Background(), org, Signal{Key: "a", Weight: 30, Detail: "something"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if len(rec.entries) != 1 {
		t.Errorf("a no-op re-record logged again: %v", rec.entries)
	}

	if _, err := svc.SetOverride(context.Background(), org, models.OrgRiskSuspended, "manual", uuid.New()); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if len(rec.entries) != 2 || rec.entries[1] != "org_risk:watch -> suspended" {
		t.Errorf("entries = %v, want the operator transition logged", rec.entries)
	}
}

// Issue #241: a workspace that reached suspended could never come back. Every
// route out of it is proven here.
func newRiskFixture() (*stubRiskRepo, Service, uuid.UUID) {
	org := uuid.New()
	repo := &stubRiskRepo{risk: &models.OrgRisk{
		OrganizationID: org, State: models.OrgRiskTrusted, Signals: map[string]any{},
	}}
	return repo, NewService(repo), org
}

// The operator's pin outranks the score, in both directions: it holds a
// suspension the detectors would release, and it holds a release the
// detectors would re-suspend.
func TestOverrideOutranksTheScore(t *testing.T) {
	repo, svc, org := newRiskFixture()
	ctx := context.Background()
	actor := uuid.New()

	if _, err := svc.RecordSignal(ctx, org, Signal{Key: "heavy", Weight: 90, Detail: "confirmed abuse"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if repo.risk.State != models.OrgRiskSuspended {
		t.Fatalf("state = %q, want the score to suspend", repo.risk.State)
	}

	// Review clears the workspace while the evidence is still on file.
	risk, err := svc.SetOverride(ctx, org, models.OrgRiskTrusted, "reviewed, legitimate agency", actor)
	if err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if risk.State != models.OrgRiskTrusted || risk.Override == nil {
		t.Fatalf("state = %q, override = %+v, want a pinned trusted", risk.State, risk.Override)
	}
	if risk.Override.By == nil || *risk.Override.By != actor {
		t.Errorf("override records %v, want the operator who set it", risk.Override.By)
	}

	// The next detector write must not undo the operator's decision.
	risk, err = svc.RecordSignal(ctx, org, Signal{Key: "another", Weight: 90, Detail: "and another"})
	if err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if risk.State != models.OrgRiskTrusted {
		t.Errorf("state = %q, want the pin to survive a detector write", risk.State)
	}
	if risk.Score != 100 {
		t.Errorf("score = %d, want the evidence still scored under the pin", risk.Score)
	}
	if risk.Reason != "reviewed, legitimate agency" {
		t.Errorf("reason = %q, want the operator's reason while pinned", risk.Reason)
	}
}

// Lifting the pin hands the posture back to the evidence, which is the whole
// point: it is a decision to stop deciding, not a permanent release.
func TestClearOverrideReDerivesFromEvidence(t *testing.T) {
	repo, svc, org := newRiskFixture()
	ctx := context.Background()

	if _, err := svc.RecordSignal(ctx, org, Signal{Key: "heavy", Weight: 90, Detail: "confirmed abuse"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if _, err := svc.SetOverride(ctx, org, models.OrgRiskTrusted, "under review", uuid.New()); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}

	risk, err := svc.ClearOverride(ctx, org)
	if err != nil {
		t.Fatalf("ClearOverride: %v", err)
	}
	if risk.Override != nil {
		t.Error("the pin must be gone")
	}
	if risk.State != models.OrgRiskSuspended {
		t.Errorf("state = %q, want the untouched evidence to decide again", risk.State)
	}

	// Retracting the evidence is the other way out, and it lasts.
	if _, err := svc.ClearSignal(ctx, org, "heavy"); err != nil {
		t.Fatalf("ClearSignal: %v", err)
	}
	if repo.risk.State != models.OrgRiskTrusted || repo.risk.Score != 0 {
		t.Errorf("state/score = %q/%d, want trusted/0 once the evidence is retracted",
			repo.risk.State, repo.risk.Score)
	}
}

// A one-shot detector cannot retract its own finding, so the finding has to
// age out. Without this the three one-shot signals were permanent.
func TestExpiredEvidenceIsRetiredAndTheBandFalls(t *testing.T) {
	repo, svc, org := newRiskFixture()
	ctx := context.Background()

	if _, err := svc.RecordSignal(ctx, org, Signal{
		Key: "signup", Weight: 35, Detail: "disposable signup domain", TTL: time.Hour,
	}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if repo.risk.State != models.OrgRiskWatch {
		t.Fatalf("state = %q, want watch", repo.risk.State)
	}
	entry, ok := repo.risk.Signals["signup"].(map[string]any)
	if !ok || entry["expires_at"] == nil {
		t.Fatalf("signals = %+v, want a dated finding", repo.risk.Signals)
	}

	// A finding with no TTL is a sweep's to retract, never a clock's.
	if _, err := svc.RecordSignal(ctx, org, Signal{Key: "cluster", Weight: 15, Detail: "shared address"}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}

	// Wind the expiry back rather than sleeping through it.
	entry["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)

	if swept := svc.SweepExpired(ctx); swept != 1 {
		t.Errorf("SweepExpired() = %d, want the one organization holding stale evidence", swept)
	}
	if _, still := repo.risk.Signals["signup"]; still {
		t.Error("the expired finding is still on file")
	}
	if _, gone := repo.risk.Signals["cluster"]; !gone {
		t.Error("a finding with no expiry was swept; only dated evidence ages out")
	}
	if repo.risk.State != models.OrgRiskTrusted || repo.risk.Score != 15 {
		t.Errorf("state/score = %q/%d, want trusted/15 after the expiry",
			repo.risk.State, repo.risk.Score)
	}
}

// Evidence whose expiry cannot be read is still evidence. Discarding it would
// let one malformed write clear a workspace's record.
func TestUnreadableExpiryIsKept(t *testing.T) {
	signals := map[string]any{
		"bad":   map[string]any{"weight": float64(30), "detail": "x", "expires_at": "not a time"},
		"typed": map[string]any{"weight": float64(10), "detail": "y", "expires_at": 12345},
		"empty": map[string]any{"weight": float64(5), "detail": "z", "expires_at": ""},
	}
	if got := PruneExpired(signals, time.Now()); len(got) != 3 {
		t.Errorf("PruneExpired kept %d of 3 findings with unreadable expiries", len(got))
	}
}

// The organization's own audit feed resolves an actor to a name and an email
// for its members, so an operator's review must be recorded there as the
// platform. Who decided lives on the record and in the admin trail.
func TestOperatorIdentityStaysOutOfTheTenantFeed(t *testing.T) {
	repo, _, org := newRiskFixture()
	rec := &recordingActor{}
	svc := NewService(repo)
	svc.(AuditAware).WireAudit(rec)
	operator := uuid.New()

	if _, err := svc.SetOverride(context.Background(), org, models.OrgRiskSuspended, "review", operator); err != nil {
		t.Fatalf("SetOverride: %v", err)
	}
	if len(rec.actors) != 1 {
		t.Fatalf("actors = %v, want the one transition", rec.actors)
	}
	if rec.actors[0] != uuid.Nil {
		t.Errorf("the tenant feed names %v, want the platform", rec.actors[0])
	}
	if repo.risk.Override == nil || repo.risk.Override.By == nil || *repo.risk.Override.By != operator {
		t.Error("the record must still say which operator decided")
	}
}

type recordingActor struct {
	actors []uuid.UUID
}

func (r *recordingActor) LogAction(_ context.Context, _, actorID uuid.UUID, _ models.AuditAction,
	_ models.AuditEntityType, _ *uuid.UUID, _, _ string, _, _ map[string]string) {
	r.actors = append(r.actors, actorID)
}
