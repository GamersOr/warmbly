// Package orgrisk fuses an organization's abuse signals into one posture.
// Every other control watches a single subject, so an actor weak on several
// axes at once stays under all of them. This is the subject that sees them.
package orgrisk

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Signal is one piece of evidence about an organization.
type Signal struct {
	// Key identifies the detector. Re-recording a key replaces its value.
	Key string
	// Weight is how many points this contributes, 0-100.
	Weight int
	// Detail is the human sentence an admin reads.
	Detail string
	// Class is what kind of evidence this is. An empty class falls back to the
	// key, so a detector that forgets it cannot accidentally restrict anyone.
	Class SignalClass
	// Evidence is structured facts a detector reads back on its next run, such
	// as the counts a running assessment accumulates. Never scored.
	Evidence map[string]any
}

// SignalClass separates how a workspace looks from what it did. An agency and
// a ring look alike, so only the second kind may cost a customer anything.
type SignalClass string

const (
	// ClassCircumstantial is shape: a shared address, a shared operator
	// identity, a burst of mailboxes. All have ordinary explanations, so shape
	// accumulates as evidence and restricts nothing.
	ClassCircumstantial SignalClass = "circumstantial"
	// ClassSubstantive is what an ordinary customer does not do: a throwaway
	// signup domain, a dead list, complaints from recipients.
	ClassSubstantive SignalClass = "substantive"
)

// Band thresholds, deliberately far apart: nothing is taken away until several
// detectors agree.
const (
	WatchScore      = 25
	RestrictedScore = 50
	SuspendedScore  = 85

	// CircumstantialCap bounds what shape contributes in total, so several
	// benign-explainable findings cannot add up to a verdict.
	CircumstantialCap = 40
	// SubstantiveFloor is the substantive evidence a band needs before it takes
	// anything away. Below it the posture tops out at watch.
	SubstantiveFloor = 25
)

// substantiveKeys classifies findings whose class did not travel with them.
// Anything not listed reads as circumstantial.
var substantiveKeys = map[string]bool{
	// The aggregate written before the disposable finding was split out.
	"signup":            true,
	"signup_disposable": true,
	"list_quality":      true,
	"recipient_harm":    true,
}

// families are detectors that describe one fact from two angles: one operator
// opening several workspaces is found by address and by identity. A family
// contributes its heaviest member once per class rather than twice.
var families = map[string]string{
	"cluster_signup_ip":       "signup_cluster",
	"cluster_signup_identity": "signup_cluster",
}

// BandFor maps a fused score to its posture.
func BandFor(score int) models.OrgRiskState {
	switch {
	case score >= SuspendedScore:
		return models.OrgRiskSuspended
	case score >= RestrictedScore:
		return models.OrgRiskRestricted
	case score >= WatchScore:
		return models.OrgRiskWatch
	default:
		return models.OrgRiskTrusted
	}
}

// Service is the risk posture of organizations.
type Service interface {
	// Get returns the organization's record. A row that has never been
	// evaluated reads as trusted rather than as an error.
	Get(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error)
	// RecordSignal adds or replaces one detector's finding and re-derives the
	// band. Returns the record after the change.
	RecordSignal(ctx context.Context, orgID uuid.UUID, sig Signal) (*models.OrgRisk, *errx.Error)
	// ClearSignal removes a detector's finding, for when it no longer holds.
	ClearSignal(ctx context.Context, orgID uuid.UUID, key string) (*models.OrgRisk, *errx.Error)
	// OrgsWithSignal lists organizations carrying a detector's finding, so a
	// recurring sweep can retract the ones that no longer match.
	OrgsWithSignal(ctx context.Context, key string) ([]uuid.UUID, *errx.Error)
	// SetState is an operator's manual override, which outranks the score.
	SetState(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, *errx.Error)
}

type service struct {
	repo  repository.OrgRiskRepository
	audit AuditLogger
}

// AuditLogger is the narrow slice of the audit service a transition needs. It
// is declared here so this package does not depend on the audit package.
type AuditLogger interface {
	LogAction(ctx context.Context, orgID, actorID uuid.UUID, action models.AuditAction,
		entityType models.AuditEntityType, entityID *uuid.UUID, ipAddress, userAgent string,
		changes, metadata map[string]string)
}

func NewService(repo repository.OrgRiskRepository) Service {
	return &service{repo: repo}
}

// WireAudit attaches the audit logger. A transition rides the audit spine, so
// every teammate's dashboard reflects it without a bespoke emit site.
func (s *service) WireAudit(a AuditLogger) { s.audit = a }

// AuditAware is the optional capability the caller uses to attach the logger.
type AuditAware interface {
	WireAudit(a AuditLogger)
}

// auditTransition records a band change. Only a CHANGE is logged: a detector
// re-recording the same finding must not fill the feed with no-ops.
func (s *service) auditTransition(ctx context.Context, orgID uuid.UUID, before, after *models.OrgRisk, actor uuid.UUID) {
	if s.audit == nil || after == nil {
		return
	}
	if before != nil && before.State == after.State {
		return
	}
	from := string(models.OrgRiskTrusted)
	if before != nil {
		from = string(before.State)
	}
	s.audit.LogAction(ctx, orgID, actor, models.AuditActionUpdate, models.AuditEntityOrgRisk, &orgID, "", "",
		map[string]string{"risk_state": from + " -> " + string(after.State)},
		map[string]string{"reason": after.Reason, "score": strconv.Itoa(after.Score)})
}

func (s *service) Get(ctx context.Context, orgID uuid.UUID) (*models.OrgRisk, *errx.Error) {
	risk, err := s.repo.GetOrgRisk(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return risk, nil
}

func (s *service) RecordSignal(ctx context.Context, orgID uuid.UUID, sig Signal) (*models.OrgRisk, *errx.Error) {
	if sig.Key == "" {
		return nil, errx.New(errx.BadRequest, "a signal needs a key")
	}
	if sig.Weight < 0 {
		sig.Weight = 0
	}
	if sig.Weight > 100 {
		sig.Weight = 100
	}
	class := sig.Class
	if class != ClassCircumstantial && class != ClassSubstantive {
		class = classOf(sig.Key, nil)
	}
	return s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
		entry := map[string]any{
			"weight": sig.Weight, "detail": sig.Detail, "class": string(class),
		}
		if len(sig.Evidence) > 0 {
			entry["evidence"] = sig.Evidence
		}
		signals[sig.Key] = entry
		return signals
	})
}

func (s *service) ClearSignal(ctx context.Context, orgID uuid.UUID, key string) (*models.OrgRisk, *errx.Error) {
	return s.apply(ctx, orgID, func(signals map[string]any) map[string]any {
		delete(signals, key)
		return signals
	})
}

func (s *service) OrgsWithSignal(ctx context.Context, key string) ([]uuid.UUID, *errx.Error) {
	ids, err := s.repo.OrgsWithSignal(ctx, key)
	if err != nil {
		return nil, errx.InternalError()
	}
	return ids, nil
}

func (s *service) SetState(ctx context.Context, orgID uuid.UUID, state models.OrgRiskState, reason string) (*models.OrgRisk, *errx.Error) {
	if !state.Valid() {
		return nil, errx.New(errx.BadRequest, "unknown risk state")
	}
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.SetOrgRiskState(ctx, orgID, state, reason)
	if err != nil {
		return nil, errx.InternalError()
	}
	s.auditTransition(ctx, orgID, before, risk, uuid.Nil)
	return risk, nil
}

// apply mutates the signal set and re-derives score, band and reason from it,
// inside the repository's transaction so two detectors firing at once cannot
// each write a band derived from a stale signal set.
func (s *service) apply(ctx context.Context, orgID uuid.UUID, mutate func(map[string]any) map[string]any) (*models.OrgRisk, *errx.Error) {
	before, _ := s.repo.GetOrgRisk(ctx, orgID)
	risk, err := s.repo.UpdateOrgRiskSignals(ctx, orgID, func(signals map[string]any) (map[string]any, models.OrgRiskState, int, string) {
		signals = mutate(signals)
		state, score := Evaluate(signals)
		return signals, state, score, Reason(signals)
	})
	if err != nil {
		return nil, errx.InternalError()
	}
	s.auditTransition(ctx, orgID, before, risk, uuid.Nil)
	return risk, nil
}

// Evaluate fuses the evidence into the band and score to store. Shape raises
// the score; only substantive evidence lets a band take something away.
func Evaluate(signals map[string]any) (models.OrgRiskState, int) {
	circumstantial, substantive := fuse(signals)
	if circumstantial > CircumstantialCap {
		circumstantial = CircumstantialCap
	}
	score := circumstantial + substantive
	if score > 100 {
		score = 100
	}
	band := BandFor(score)
	if substantive < SubstantiveFloor {
		band = atMost(band, models.OrgRiskWatch)
	}
	return band, score
}

// Score is the fused weight behind the band: one weight per family, shape
// capped, total capped at 100.
func Score(signals map[string]any) int {
	_, score := Evaluate(signals)
	return score
}

// fuse reduces the evidence to one weight per family and totals each class.
// Heaviest per class, so shape can never hide conduct filed in the same family.
func fuse(signals map[string]any) (circumstantial, substantive int) {
	type member struct {
		family string
		class  SignalClass
	}
	heaviest := make(map[member]int, len(signals))
	for key, raw := range signals {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		m := member{family: key, class: classOf(key, entry["class"])}
		if f, named := families[key]; named {
			m.family = f
		}
		if weight := weightOf(entry["weight"]); weight > heaviest[m] {
			heaviest[m] = weight
		}
	}
	for m, weight := range heaviest {
		if m.class == ClassSubstantive {
			substantive += weight
			continue
		}
		circumstantial += weight
	}
	return circumstantial, substantive
}

// classOf reads a finding's class, falling back to the key for rows written
// before the class travelled with the weight.
func classOf(key string, raw any) SignalClass {
	if s, ok := raw.(string); ok {
		switch SignalClass(s) {
		case ClassSubstantive:
			return ClassSubstantive
		case ClassCircumstantial:
			return ClassCircumstantial
		}
	}
	if substantiveKeys[key] {
		return ClassSubstantive
	}
	return ClassCircumstantial
}

// atMost lowers a band to a ceiling, so a rule can hold a verdict back without
// knowing which band the score produced.
func atMost(band, ceiling models.OrgRiskState) models.OrgRiskState {
	if bandRank(band) > bandRank(ceiling) {
		return ceiling
	}
	return band
}

func bandRank(state models.OrgRiskState) int {
	switch state {
	case models.OrgRiskWatch:
		return 1
	case models.OrgRiskRestricted:
		return 2
	case models.OrgRiskSuspended:
		return 3
	default:
		return 0
	}
}

// Reason is the heaviest signals, in a sentence, so an operator sees WHY
// without opening the evidence blob.
func Reason(signals map[string]any) string {
	type entry struct {
		detail string
		weight int
	}
	entries := make([]entry, 0, len(signals))
	for key, raw := range signals {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		detail, _ := m["detail"].(string)
		if detail == "" {
			detail = key
		}
		entries = append(entries, entry{detail: detail, weight: weightOf(m["weight"])})
	}
	if len(entries) == 0 {
		return ""
	}
	// Heaviest first, then by text so the sentence is stable across runs
	// rather than reordering with Go's map iteration.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].weight != entries[j].weight {
			return entries[i].weight > entries[j].weight
		}
		return entries[i].detail < entries[j].detail
	})
	if len(entries) > 3 {
		entries = entries[:3]
	}
	out := ""
	for i, e := range entries {
		if i > 0 {
			out += "; "
		}
		out += e.detail
	}
	return out
}

// HasSignal reports whether a detector's finding is currently recorded.
func HasSignal(risk *models.OrgRisk, key string) bool {
	if risk == nil {
		return false
	}
	_, ok := risk.Signals[key].(map[string]any)
	return ok
}

// EvidenceInt reads one number a detector stored beside its finding. Zero when
// the finding, the evidence or the field is absent.
func EvidenceInt(risk *models.OrgRisk, key, field string) int {
	if risk == nil {
		return 0
	}
	entry, ok := risk.Signals[key].(map[string]any)
	if !ok {
		return 0
	}
	evidence, ok := entry["evidence"].(map[string]any)
	if !ok {
		return 0
	}
	return weightOf(evidence[field])
}

// weightOf reads a number back out of jsonb, where a Go int round-trips as a
// float64.
func weightOf(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case fmt.Stringer:
		return 0
	default:
		return 0
	}
}
