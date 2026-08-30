// Package segment manages saved contact audiences (issue #266). A segment is
// a filter definition plus manual overrides; membership is computed at read
// time by the repository's SQL compiler, so nothing here schedules work.
package segment

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// CampaignWaker wakes a campaign's parked send chain after leads are added.
type CampaignWaker interface {
	WakeCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []string)
}

type Service interface {
	List(ctx context.Context, orgID uuid.UUID) ([]models.Segment, *errx.Error)
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error)
	Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error)
	Update(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error)
	Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error
	Preview(ctx context.Context, orgID uuid.UUID, in *models.SegmentPreview) (int, *errx.Error)
	SetMembers(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentMembersWrite) (int, *errx.Error)
	MemberModes(ctx context.Context, orgID, id uuid.UUID, contactIDs []string) (map[uuid.UUID]models.SegmentMemberMode, *errx.Error)
	AddToCampaign(ctx context.Context, orgID uuid.UUID, actor string, id uuid.UUID, in *models.SegmentAddToCampaign) (*models.SegmentAddToCampaignResult, *errx.Error)
	// Fields describes every filterable field for the condition builder.
	Fields(ctx context.Context, orgID uuid.UUID) ([]models.SegmentFieldSpec, *errx.Error)
	SegmentsForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactSegment, *errx.Error)
	Overrides(ctx context.Context, orgID, id uuid.UUID) ([]models.SegmentOverride, *errx.Error)
	SetCampaignWaker(w CampaignWaker)
}

// CustomFieldLister is the slice of the contact repository Fields needs.
type CustomFieldLister interface {
	DistinctCustomFieldKeys(ctx context.Context, orgID uuid.UUID) ([]string, error)
}

type service struct {
	repo   repository.SegmentRepository
	fields CustomFieldLister
	waker  CampaignWaker
}

func NewService(repo repository.SegmentRepository, fields CustomFieldLister) Service {
	return &service{repo: repo, fields: fields}
}

func (s *service) SetCampaignWaker(w CampaignWaker) { s.waker = w }

func (s *service) List(ctx context.Context, orgID uuid.UUID) ([]models.Segment, *errx.Error) {
	return s.repo.List(ctx, orgID)
}

func (s *service) Get(ctx context.Context, orgID, id uuid.UUID) (*models.Segment, *errx.Error) {
	return s.repo.Get(ctx, orgID, id)
}

// applyWrite folds a create/update body into seg, validating each field it
// sets. selfID guards self-reference on update.
func applyWrite(seg *models.Segment, in *models.SegmentWrite, selfID *uuid.UUID) *errx.Error {
	if in.Name != nil {
		name, xerr := models.ValidateSegmentName(*in.Name)
		if xerr != nil {
			return xerr
		}
		seg.Name = name
	}
	if in.Description != nil {
		d := strings.TrimSpace(*in.Description)
		if len(d) > models.SegmentMaxDescLen {
			return errx.New(errx.BadRequest, fmt.Sprintf("description must be at most %d characters", models.SegmentMaxDescLen))
		}
		seg.Description = d
	}
	if in.Color != nil {
		color, xerr := models.ValidateSegmentColor(*in.Color)
		if xerr != nil {
			return xerr
		}
		seg.Color = color
	}
	if in.Match != nil {
		if xerr := models.ValidateSegmentMatch(*in.Match); xerr != nil {
			return xerr
		}
		seg.Match = *in.Match
	}
	if in.Conditions != nil {
		conds := *in.Conditions
		if conds == nil {
			conds = []models.SegmentCondition{}
		}
		if xerr := models.ValidateSegmentConditions(conds, selfID); xerr != nil {
			return xerr
		}
		seg.Conditions = conds
	}
	return nil
}

// checkReferences makes sure every referenced segment exists in the org and
// that following references from it never leads back to selfID.
func (s *service) checkReferences(ctx context.Context, orgID uuid.UUID, conds []models.SegmentCondition, selfID *uuid.UUID) *errx.Error {
	refs := models.SegmentReferences(conds)
	if len(refs) == 0 {
		return nil
	}
	seen := map[uuid.UUID]bool{}
	pending := refs
	for depth := 0; len(pending) > 0; depth++ {
		if depth > models.SegmentMaxNestingDeep {
			return errx.New(errx.BadRequest, fmt.Sprintf("segments can be nested at most %d levels deep", models.SegmentMaxNestingDeep))
		}
		var next []uuid.UUID
		for _, id := range pending {
			if selfID != nil && id == *selfID {
				return errx.New(errx.BadRequest, "segments cannot reference each other in a loop")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			ref, xerr := s.repo.Get(ctx, orgID, id)
			if xerr != nil {
				if xerr.Code == errx.NotFound {
					return errx.New(errx.BadRequest, "a referenced segment does not exist")
				}
				return xerr
			}
			next = append(next, models.SegmentReferences(ref.Conditions)...)
		}
		pending = next
	}
	return nil
}

func (s *service) Create(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error) {
	seg := &models.Segment{Color: "#0284c7", Match: models.SegmentMatchAll, Conditions: []models.SegmentCondition{}}
	if in.Name == nil {
		return nil, errx.New(errx.BadRequest, "segment name is required")
	}
	if xerr := applyWrite(seg, in, nil); xerr != nil {
		return nil, xerr
	}
	if xerr := s.checkReferences(ctx, orgID, seg.Conditions, nil); xerr != nil {
		return nil, xerr
	}
	return s.repo.Create(ctx, orgID, createdBy, seg)
}

func (s *service) Update(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentWrite) (*models.Segment, *errx.Error) {
	seg, xerr := s.repo.Get(ctx, orgID, id)
	if xerr != nil {
		return nil, xerr
	}
	if xerr := applyWrite(seg, in, &id); xerr != nil {
		return nil, xerr
	}
	if in.Conditions != nil {
		if xerr := s.checkReferences(ctx, orgID, seg.Conditions, &id); xerr != nil {
			return nil, xerr
		}
	}
	return s.repo.Update(ctx, orgID, seg)
}

func (s *service) Delete(ctx context.Context, orgID, id uuid.UUID) *errx.Error {
	names, xerr := s.repo.ReferencedBy(ctx, orgID, id)
	if xerr != nil {
		return xerr
	}
	if len(names) > 0 {
		return errx.New(errx.Conflict, "this segment is used by: "+strings.Join(names, ", ")+". Remove it from those segments first")
	}
	return s.repo.Delete(ctx, orgID, id)
}

func (s *service) Preview(ctx context.Context, orgID uuid.UUID, in *models.SegmentPreview) (int, *errx.Error) {
	if in.Match == "" {
		in.Match = models.SegmentMatchAll
	}
	if xerr := models.ValidateSegmentMatch(in.Match); xerr != nil {
		return 0, xerr
	}
	if in.Conditions == nil {
		in.Conditions = []models.SegmentCondition{}
	}
	if xerr := models.ValidateSegmentConditions(in.Conditions, in.ID); xerr != nil {
		return 0, xerr
	}
	if xerr := s.checkReferences(ctx, orgID, in.Conditions, in.ID); xerr != nil {
		return 0, xerr
	}
	if in.ID != nil {
		if _, xerr := s.repo.Get(ctx, orgID, *in.ID); xerr != nil {
			return 0, xerr
		}
	}
	return s.repo.Count(ctx, orgID, in.ID, in.Match, in.Conditions)
}

func parseContactIDs(raw []string) ([]uuid.UUID, *errx.Error) {
	if len(raw) == 0 {
		return nil, errx.New(errx.BadRequest, "no contacts provided")
	}
	if len(raw) > 1000 {
		return nil, errx.New(errx.BadRequest, "at most 1000 contacts per request")
	}
	out := make([]uuid.UUID, 0, len(raw))
	for _, r := range raw {
		id, err := uuid.Parse(r)
		if err != nil {
			return nil, errx.New(errx.BadRequest, "invalid contact id")
		}
		out = append(out, id)
	}
	return out, nil
}

func (s *service) SetMembers(ctx context.Context, orgID, id uuid.UUID, in *models.SegmentMembersWrite) (int, *errx.Error) {
	switch in.Mode {
	case models.SegmentMemberInclude, models.SegmentMemberExclude, models.SegmentMemberAuto:
	default:
		return 0, errx.New(errx.BadRequest, "mode must be include, exclude or auto")
	}
	ids, xerr := parseContactIDs(in.Contacts)
	if xerr != nil {
		return 0, xerr
	}
	if _, xerr := s.repo.Get(ctx, orgID, id); xerr != nil {
		return 0, xerr
	}
	return s.repo.SetMembers(ctx, orgID, id, ids, in.Mode)
}

func (s *service) MemberModes(ctx context.Context, orgID, id uuid.UUID, contactIDs []string) (map[uuid.UUID]models.SegmentMemberMode, *errx.Error) {
	ids, xerr := parseContactIDs(contactIDs)
	if xerr != nil {
		return nil, xerr
	}
	if _, xerr := s.repo.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	return s.repo.MemberModes(ctx, id, ids)
}

func (s *service) AddToCampaign(ctx context.Context, orgID uuid.UUID, actor string, id uuid.UUID, in *models.SegmentAddToCampaign) (*models.SegmentAddToCampaignResult, *errx.Error) {
	campaignID, err := uuid.Parse(in.CampaignID)
	if err != nil {
		return nil, errx.New(errx.BadRequest, "invalid campaign id")
	}
	res, xerr := s.repo.AddToCampaign(ctx, orgID, actor, id, campaignID)
	if xerr != nil {
		return nil, xerr
	}
	if s.waker != nil && res.Added > 0 {
		s.waker.WakeCampaigns(ctx, orgID, []string{campaignID.String()})
	}
	return res, nil
}

func (s *service) Fields(ctx context.Context, orgID uuid.UUID) ([]models.SegmentFieldSpec, *errx.Error) {
	out := make([]models.SegmentFieldSpec, 0, len(models.SegmentFieldCatalog)+16)
	out = append(out, models.SegmentFieldCatalog...)
	if s.fields == nil {
		return out, nil
	}
	keys, err := s.fields.DistinctCustomFieldKeys(ctx, orgID)
	if err != nil {
		return nil, errx.InternalError()
	}
	for _, k := range keys {
		out = append(out, models.SegmentFieldSpec{Field: models.SegmentCustomFieldPrefix + k, Label: k, Group: "Custom field", Kind: models.SegmentFieldText})
	}
	return out, nil
}

func (s *service) SegmentsForContact(ctx context.Context, orgID, contactID uuid.UUID) ([]models.ContactSegment, *errx.Error) {
	return s.repo.SegmentsForContact(ctx, orgID, contactID)
}

func (s *service) Overrides(ctx context.Context, orgID, id uuid.UUID) ([]models.SegmentOverride, *errx.Error) {
	if _, xerr := s.repo.Get(ctx, orgID, id); xerr != nil {
		return nil, xerr
	}
	return s.repo.ListOverrides(ctx, orgID, id)
}
