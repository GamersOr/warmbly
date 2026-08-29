package orgrisk

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func sig(weight int, detail string) map[string]any {
	return map[string]any{"weight": float64(weight), "detail": detail}
}

// classed is how RecordSignal writes a finding: the class travels with it.
func classed(weight int, class SignalClass, detail string) map[string]any {
	return map[string]any{"weight": float64(weight), "detail": detail, "class": string(class)}
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
		"signup":         classed(20, ClassSubstantive, "disposable signup domain"),
		"list_quality":   classed(20, ClassSubstantive, "imported list is 30% invalid"),
		"recipient_harm": classed(20, ClassSubstantive, "mailboxes landing in spam"),
	}
	if got := Score(signals); got != 60 {
		t.Errorf("Score() = %d, want 60", got)
	}
	if state, _ := Evaluate(signals); state != models.OrgRiskRestricted {
		t.Errorf("three tolerable substantive signals should restrict, got %q", state)
	}

	signals["more"] = classed(80, ClassSubstantive, "and another")
	if got := Score(signals); got != 100 {
		t.Errorf("Score() = %d, want it capped at 100", got)
	}
}

// Issue #245: an agency opening client workspaces from one office, under one
// operator identity, connecting their mailboxes in an afternoon, trips every
// cross-account detector at once. None of that is conduct, so none of it may
// cost the customer volume.
func TestAgencyOnboardingIsNotRestricted(t *testing.T) {
	agency := map[string]any{
		"cluster_signup_ip":       classed(15, ClassCircumstantial, "opened alongside 2 other workspaces from one address"),
		"cluster_signup_identity": classed(25, ClassCircumstantial, "opened alongside 2 other workspaces by the same email identity"),
		"mailbox_burst":           classed(15, ClassCircumstantial, "connected 15 or more mailboxes within 24 hours"),
	}
	state, score := Evaluate(agency)
	if state != models.OrgRiskWatch {
		t.Errorf("state = %q, want watch: shape alone must not restrict", state)
	}
	if score != CircumstantialCap {
		t.Errorf("score = %d, want the shape total capped at %d", score, CircumstantialCap)
	}

	// The second half of the report: one weak first import used to carry the
	// same workspace past the suspension threshold.
	agency["list_quality"] = classed(12, ClassSubstantive, "26% of this list is unusable")
	state, score = Evaluate(agency)
	if state != models.OrgRiskWatch {
		t.Errorf("state = %q, want watch: %d points of conduct is under the floor", state, 12)
	}
	if score >= SuspendedScore {
		t.Errorf("score = %d, want it nowhere near suspension", score)
	}

	// A list that is mostly unusable IS conduct, and then the shape counts.
	agency["list_quality"] = classed(30, ClassSubstantive, "62% of this list is unusable")
	if state, _ = Evaluate(agency); state != models.OrgRiskRestricted {
		t.Errorf("state = %q, want restricted once real conduct joins the shape", state)
	}
}

// The address cluster and the identity cluster are the same fact found twice.
// Charging an agency for both is what pushed it over the line.
func TestClusterFamilyIsCountedOnce(t *testing.T) {
	both := map[string]any{
		"cluster_signup_ip":       classed(15, ClassCircumstantial, "one address"),
		"cluster_signup_identity": classed(25, ClassCircumstantial, "one identity"),
	}
	if got := Score(both); got != 25 {
		t.Errorf("Score() = %d, want the heaviest family member alone (25)", got)
	}
	only := map[string]any{"cluster_signup_identity": classed(25, ClassCircumstantial, "one identity")}
	if Score(both) != Score(only) {
		t.Error("finding the same cluster by address as well as identity must not add points")
	}
}

// A family exists to stop one fact being counted twice, not to let a heavy
// shape finding swallow the conduct finding beside it.
func TestFamilyFusionNeverHidesConduct(t *testing.T) {
	families["cluster_signup_conduct_probe"] = "signup_cluster"
	t.Cleanup(func() { delete(families, "cluster_signup_conduct_probe") })

	mixed := map[string]any{
		"cluster_signup_identity":      classed(25, ClassCircumstantial, "one identity"),
		"cluster_signup_conduct_probe": classed(30, ClassSubstantive, "something the workspace did"),
	}
	state, score := Evaluate(mixed)
	if score != 55 {
		t.Errorf("score = %d, want 55: one weight per class, not per family", score)
	}
	if state != models.OrgRiskRestricted {
		t.Errorf("state = %q, want restricted: the conduct finding still counts", state)
	}
}

// Shape is capped in total, so a workspace that looks unusual on every axis
// still cannot be restricted by looking unusual.
func TestCircumstantialEvidenceIsCappedAndNeverRestricts(t *testing.T) {
	loud := map[string]any{
		"cluster_signup_identity": classed(25, ClassCircumstantial, "one identity"),
		"mailbox_burst":           classed(15, ClassCircumstantial, "a burst"),
		"login_anomalies":         classed(20, ClassCircumstantial, "odd sign-ins"),
		"something_else":          classed(40, ClassCircumstantial, "unregistered detector"),
	}
	state, score := Evaluate(loud)
	if score != CircumstantialCap {
		t.Errorf("score = %d, want %d", score, CircumstantialCap)
	}
	if state != models.OrgRiskWatch {
		t.Errorf("state = %q, want watch", state)
	}
}

// Conduct on its own decides the band. A workspace drawing complaints past the
// band providers act on does not need a cluster to be restricted.
func TestSubstantiveEvidenceRestrictsAlone(t *testing.T) {
	harm := map[string]any{
		"recipient_harm": classed(50, ClassSubstantive, "recipients reported 0.41% of the last 900 sends as spam"),
	}
	if state, _ := Evaluate(harm); state != models.OrgRiskRestricted {
		t.Errorf("state = %q, want restricted", state)
	}

	// The warning band alone is watch: it is real, but one moderate reading is
	// not worth cutting a customer's volume over.
	warned := map[string]any{
		"recipient_harm": classed(SubstantiveFloor, ClassSubstantive, "recipients reported 0.12% of the last 900 sends as spam"),
	}
	if state, _ := Evaluate(warned); state != models.OrgRiskWatch {
		t.Errorf("state = %q, want watch", state)
	}
	// With the shape beside it, the same reading restricts.
	warned["mailbox_burst"] = classed(15, ClassCircumstantial, "a burst")
	warned["cluster_signup_identity"] = classed(25, ClassCircumstantial, "one identity")
	if state, _ := Evaluate(warned); state != models.OrgRiskRestricted {
		t.Errorf("state = %q, want restricted", state)
	}
}

// Findings written before the class was stored are classified by key, so a
// deploy does not quietly release everything already flagged.
func TestLegacySignalsAreClassifiedByKey(t *testing.T) {
	legacy := map[string]any{
		"signup":       sig(35, "signup used a disposable email domain"),
		"list_quality": sig(30, "62% of this list is unusable"),
	}
	state, score := Evaluate(legacy)
	if score != 65 {
		t.Errorf("score = %d, want 65", score)
	}
	if state != models.OrgRiskRestricted {
		t.Errorf("state = %q, want restricted: both are substantive by key", state)
	}

	// An unknown key with no class reads as shape, which restricts nothing.
	unknown := map[string]any{"mystery": sig(90, "a detector nobody registered")}
	if state, _ = Evaluate(unknown); state != models.OrgRiskWatch {
		t.Errorf("state = %q, want watch for an unclassified finding", state)
	}
}

// RecordSignal stores the class beside the weight, and fills it in from the key
// when a caller leaves it empty.
func TestRecordSignalPersistsClass(t *testing.T) {
	repo := &stubRiskRepo{risk: &models.OrgRisk{State: models.OrgRiskTrusted, Signals: map[string]any{}}}
	svc := NewService(repo)
	org := uuid.New()

	if _, err := svc.RecordSignal(context.Background(), org, Signal{
		Key: "mailbox_burst", Weight: 15, Detail: "a burst", Class: ClassCircumstantial,
	}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if _, err := svc.RecordSignal(context.Background(), org, Signal{
		Key: "cluster_signup_identity", Weight: 25, Detail: "one identity",
	}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}
	if _, err := svc.RecordSignal(context.Background(), org, Signal{
		Key: "list_quality", Weight: 30, Detail: "a bad list",
	}); err != nil {
		t.Fatalf("RecordSignal: %v", err)
	}

	for key, want := range map[string]SignalClass{
		"mailbox_burst": ClassCircumstantial,
		"list_quality":  ClassSubstantive,
	} {
		entry, ok := repo.risk.Signals[key].(map[string]any)
		if !ok {
			t.Fatalf("%s was not recorded", key)
		}
		if got := entry["class"]; got != string(want) {
			t.Errorf("%s class = %v, want %q", key, got, want)
		}
	}
	if repo.risk.State != models.OrgRiskRestricted || repo.risk.Score != 70 {
		t.Errorf("state = %q/%d, want restricted/70: a bad list plus shape",
			repo.risk.State, repo.risk.Score)
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
	copy := *s.risk
	return &copy, nil
}
func (s *stubRiskRepo) GetOrgRiskStates(context.Context, []uuid.UUID) (map[uuid.UUID]models.OrgRiskState, error) {
	return nil, nil
}
func (s *stubRiskRepo) UpdateOrgRiskSignals(_ context.Context, _ uuid.UUID,
	derive func(map[string]any) (map[string]any, models.OrgRiskState, int, string)) (*models.OrgRisk, error) {
	signals, state, score, reason := derive(s.risk.Signals)
	s.risk.Signals, s.risk.State, s.risk.Score, s.risk.Reason = signals, state, score, reason
	copy := *s.risk
	return &copy, nil
}
func (s *stubRiskRepo) OrgsWithSignal(context.Context, string) ([]uuid.UUID, error) {
	return nil, nil
}
func (s *stubRiskRepo) SetOrgRiskState(_ context.Context, _ uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, error) {
	s.risk.State, s.risk.Reason = state, reason
	copy := *s.risk
	return &copy, nil
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

	if _, err := svc.SetState(context.Background(), org, models.OrgRiskSuspended, "manual"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if len(rec.entries) != 2 || rec.entries[1] != "org_risk:watch -> suspended" {
		t.Errorf("entries = %v, want the operator transition logged", rec.entries)
	}
}
