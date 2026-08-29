package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// Live cover for the contact timeline's lifecycle events and first-touch
// source attribution (issue #255): creation, campaign and category membership
// changes are written inside the same transactions as the links, and read
// back through ListTimeline with the names resolved at write time.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveContactTimeline -v

func countTimeline(events []models.ContactTimelineEvent, typ models.ContactTimelineEventType, match func(models.ContactTimelineEvent) bool) int {
	n := 0
	for _, e := range events {
		if e.Type == typ && (match == nil || match(e)) {
			n++
		}
	}
	return n
}

func TestLiveContactTimelineLifecycleEvents(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewContactRepostory(handle)

	category := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO categories (id, user_id, title, color, position) VALUES ($1, $2, 'Warm lead', '#ff8800', 0)`,
		category, f.owner); err != nil {
		t.Fatalf("category: %v", err)
	}
	t.Cleanup(func() {
		// Registered after the fixture, so it runs before the fixture drops
		// the contacts and users the category hangs off.
		_, _ = pool.Exec(context.Background(), `DELETE FROM contact_categories WHERE category_id = $1`, category)
		_, _ = pool.Exec(context.Background(), `DELETE FROM categories WHERE id = $1`, category)
	})

	email := "i255-" + uuid.New().String()[:8] + "@test.local"
	created, xerr := repo.Add(ctx, f.owner.String(), f.org, []models.AddContact{{
		FirstName: "Dana", LastName: "Ruiz", Email: email,
		Campaigns:  []string{f.campaign.String()},
		Categories: []string{category.String()},
		Source:     models.ContactSourceManual,
	}})
	if xerr != nil || len(created) != 1 {
		t.Fatalf("add: %v (%d created)", xerr, len(created))
	}
	id := created[0].ID

	timeline := func() []models.ContactTimelineEvent {
		t.Helper()
		res, xerr := repo.ListTimeline(ctx, f.owner, &f.org, id, 50, nil)
		if xerr != nil {
			t.Fatalf("timeline: %v", xerr)
		}
		return res.Data
	}
	byCampaign := func(name string) func(models.ContactTimelineEvent) bool {
		return func(e models.ContactTimelineEvent) bool { return e.CampaignName != nil && *e.CampaignName == name }
	}
	byCategory := func(e models.ContactTimelineEvent) bool {
		return e.CategoryTitle != nil && *e.CategoryTitle == "Warm lead" && e.CategoryID != nil && *e.CategoryID == category
	}

	ev := timeline()
	if n := countTimeline(ev, models.TimelineContactCreated, func(e models.ContactTimelineEvent) bool {
		return e.Source != nil && *e.Source == "manual" && e.UserID != nil && *e.UserID == f.owner
	}); n != 1 {
		t.Fatalf("want one manual contact_created event by the owner, got %d in %+v", n, ev)
	}
	if n := countTimeline(ev, models.TimelineCampaignAdded, byCampaign("Agency partnerships")); n != 1 {
		t.Fatalf("want campaign_added for Agency partnerships, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCategoryAdded, byCategory); n != 1 {
		t.Fatalf("want category_added for Warm lead, got %d", n)
	}

	// Re-adding the same address is an upsert, not a creation: no second
	// contact_created, and the original source survives the import's claim.
	if _, xerr := repo.Add(ctx, f.owner.String(), f.org, []models.AddContact{{
		Email: email, Company: "Pied Piper", Source: models.ContactSourceImport, SourceDetail: "leads.csv",
	}}); xerr != nil {
		t.Fatalf("re-add: %v", xerr)
	}
	if n := countTimeline(timeline(), models.TimelineContactCreated, nil); n != 1 {
		t.Fatalf("an upsert must not log a second creation, got %d", n)
	}
	detail, xerr := repo.GetDetail(ctx, f.owner, &f.org, id)
	if xerr != nil {
		t.Fatalf("detail: %v", xerr)
	}
	if detail.Source != models.ContactSourceManual || detail.FirstSeenAt.IsZero() {
		t.Fatalf("want first-touch source manual with a first-seen time, got %q at %v", detail.Source, detail.FirstSeenAt)
	}

	// Single-contact update: swap campaigns, drop the category.
	if _, xerr := repo.Update(ctx, f.owner.String(), id.String(), f.org, &models.UpdateContact{
		Campaigns:        []string{f.other.String()},
		RemoveCategories: []string{category.String()},
	}); xerr != nil {
		t.Fatalf("update: %v", xerr)
	}
	ev = timeline()
	if n := countTimeline(ev, models.TimelineCampaignRemoved, byCampaign("Agency partnerships")); n != 1 {
		t.Fatalf("want campaign_removed for Agency partnerships, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCampaignAdded, byCampaign("RevOps outreach")); n != 1 {
		t.Fatalf("want campaign_added for RevOps outreach, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCategoryRemoved, byCategory); n != 1 {
		t.Fatalf("want category_removed for Warm lead, got %d", n)
	}

	// Bulk edit: the other direction for both link kinds.
	if _, xerr := repo.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
		Contacts:        []string{id.String()},
		RemoveCampaigns: []string{f.other.String()},
		AddCategories:   []string{category.String()},
	}); xerr != nil {
		t.Fatalf("bulk update: %v", xerr)
	}
	ev = timeline()
	if n := countTimeline(ev, models.TimelineCampaignRemoved, byCampaign("RevOps outreach")); n != 1 {
		t.Fatalf("want campaign_removed for RevOps outreach, got %d", n)
	}
	if n := countTimeline(ev, models.TimelineCategoryAdded, byCategory); n != 2 {
		t.Fatalf("want a second category_added for Warm lead, got %d", n)
	}
	// A no-op bulk edit (already linked) writes nothing.
	if _, xerr := repo.BulkUpdate(ctx, f.owner.String(), f.org, &models.BulkEditContactsData{
		Contacts: []string{id.String()}, AddCategories: []string{category.String()},
	}); xerr != nil {
		t.Fatalf("bulk no-op: %v", xerr)
	}
	if n := countTimeline(timeline(), models.TimelineCategoryAdded, byCategory); n != 2 {
		t.Fatalf("an already-linked category must not log again, got %d", n)
	}

	// Newest first, and the whole feed is in order.
	for i := 1; i < len(ev); i++ {
		if ev[i].At.After(ev[i-1].At) {
			t.Fatalf("timeline out of order at %d: %s after %s", i, ev[i].At, ev[i-1].At)
		}
	}
}

// A contact created from a campaign's Leads tab is attributed to that campaign
// by name, resolved server-side.
func TestLiveContactSourceCampaignResolvesName(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	repo := NewContactRepostory(handle)

	created, xerr := repo.Add(ctx, f.mate.String(), f.org, []models.AddContact{{
		Email:     "i255-" + uuid.New().String()[:8] + "@test.local",
		Campaigns: []string{f.campaign.String()},
		Source:    models.ContactSourceCampaign,
	}})
	if xerr != nil || len(created) != 1 {
		t.Fatalf("add: %v", xerr)
	}
	detail, xerr := repo.GetDetail(ctx, f.mate, &f.org, created[0].ID)
	if xerr != nil {
		t.Fatalf("detail: %v", xerr)
	}
	if detail.Source != models.ContactSourceCampaign || detail.SourceDetail != "Agency partnerships" {
		t.Fatalf("want source campaign/Agency partnerships, got %q/%q", detail.Source, detail.SourceDetail)
	}
	if _, xerr := repo.Add(ctx, f.mate.String(), f.org, []models.AddContact{{
		Email: "i255-bad-" + uuid.New().String()[:8] + "@test.local", Source: "website",
	}}); xerr == nil {
		t.Fatal("an unknown source must be refused, not stored")
	}
}
