package wmail

import (
	"context"
	"fmt"
	"testing"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// fakeImapConn is the sync pass's view of a server. Only the methods a pass
// calls are implemented; the embedded nil interface makes anything else panic
// rather than silently pass.
type fakeImapConn struct {
	ImapConn
	folders  []models.Mailbox
	changed  []goimap.UID
	fetches  int
	released int
}

func (c *fakeImapConn) Folders() ([]models.Mailbox, *errx.MailError) { return c.folders, nil }

func (c *fakeImapConn) ReleaseMailbox() { c.released++ }

func (c *fakeImapConn) SelectForSync(string) (uint32, *errx.MailError) {
	return uint32(len(c.changed)), nil
}

func (c *fakeImapConn) SearchChangedSince(uint64) ([]goimap.UID, *errx.MailError) {
	return append([]goimap.UID(nil), c.changed...), nil
}

func (c *fakeImapConn) FetchEnvelopes(_ context.Context, uids []goimap.UID) ([]*imap.Fetched, *errx.MailError) {
	c.fetches++
	out := make([]*imap.Fetched, 0, len(uids))
	for _, uid := range uids {
		out = append(out, &imap.Fetched{Email: &models.EmailMessageData{
			UID:       uint32(uid),
			MessageID: fmt.Sprintf("<%d@fake.test>", uid),
			Subject:   "hello",
		}})
	}
	return out, nil
}

func (c *fakeImapConn) FetchBody(*imap.Fetched) {}

// fixedBudget is a syncBudget that admits a fixed number of messages and then
// denies on the daily window, which is how a real governor answers once the
// mailbox's day is spent. Redis is unreachable from a unit test, so the fake
// stands in for the counters, not for the decision the pass makes from them.
type fixedBudget struct {
	allow    int
	admitted int
	// observed totals what ObserveLive was told, so a test can check that a
	// held backlog is not re-counted toward the flood threshold every pass.
	observed int
}

func (b *fixedBudget) Policy() models.SyncPolicy               { return normalizePolicy(models.SyncPolicy{}) }
func (b *fixedBudget) SetPolicy(models.SyncPolicy)             {}
func (b *fixedBudget) RecordThrottledDay(context.Context) bool { return false }

func (b *fixedBudget) Admit(context.Context, SyncLane) Admission {
	if b.admitted >= b.allow {
		return Admission{Reason: models.SyncThrottleDaily, Until: time.Now().Add(time.Hour)}
	}
	b.admitted++
	return Admission{OK: true}
}

func (b *fixedBudget) ObserveLive(_ context.Context, n int) bool {
	b.observed += n
	return false
}

// newIMAPTestMail builds the smallest WMail that can run an IMAP pass.
func newIMAPTestMail(conn ImapConn, budget syncBudget, saved *models.Mailbox) (*WMail, *[]captured) {
	var events []captured
	w := &WMail{
		UserID:                    uuid.New(),
		ID:                        uuid.New(),
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		gov:                       budget,
		SmtpImapData: &SmtpImapData{
			ImapClient: conn,
			Mailboxes:  []*models.Mailbox{saved},
		},
	}
	w.onEvent = func(kind models.JobEventType, body any) error {
		events = append(events, captured{eventType: kind, body: body})
		return nil
	}
	// The backfill is a separate lane with its own early return; keep it out
	// of the way so these tests only exercise the live batch loop.
	w.tracker = newSyncTracker(
		&models.SyncState{BackfillStatus: models.SyncBackfillComplete},
		func(models.SyncState) error { return nil },
	)
	return w, &events
}

func uidRange(n int) []goimap.UID {
	out := make([]goimap.UID, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, goimap.UID(i))
	}
	return out
}

func relayedModSeq(t *testing.T, events []captured) uint64 {
	t.Helper()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].eventType != models.JobEventTypeMailboxUpdate {
			continue
		}
		return events[i].body.(*models.JobEventMailboxUpdate).Data.HighestModSeq
	}
	t.Fatal("no MAILBOX_UPDATE was relayed")
	return 0
}

func hasEvent(events []captured, kind models.JobEventType) bool {
	for _, e := range events {
		if e.eventType == kind {
			return true
		}
	}
	return false
}

// A mailbox unfrozen on a long backlog must not walk the whole thing: once
// the live lane is denied, the folder stops fetching and holds its
// mod-sequence, so the backlog is not re-offered to the flood detector batch
// after batch until the mailbox deactivates itself.
func TestImapSyncStopsFetchingOnceTheLiveLaneIsDenied(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 90_000}},
		changed: uidRange(3 * config.ImapFetchBatchSize),
	}
	budget := &fixedBudget{allow: 0}
	w, events := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if conn.fetches != 1 {
		t.Errorf("fetched %d batches after the lane was denied, want 1", conn.fetches)
	}
	if got := w.SmtpImapData.Mailboxes[0].HighestModSeq; got != 100 {
		t.Errorf("mod-sequence advanced to %d; a deferred backlog must hold it at 100", got)
	}
	if got := relayedModSeq(t, *events); got != 100 {
		t.Errorf("relayed mod-sequence = %d, want the held 100", got)
	}
	if hasEvent(*events, models.JobEventTypeEmailRateLimited) {
		t.Error("mailbox was deactivated by its own deferred backlog")
	}

	// The next pass is re-offered the same mail. It must not read as a fresh
	// flood: only messages the pass has never classified are observed.
	seenAfterFirst := budget.observed
	if seenAfterFirst != config.ImapFetchBatchSize {
		t.Fatalf("observed %d new messages, want one batch", seenAfterFirst)
	}
	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if budget.observed != seenAfterFirst {
		t.Errorf("observed %d after the second pass, want %d: the held backlog was counted twice",
			budget.observed, seenAfterFirst)
	}
	if conn.fetches != 2 {
		t.Errorf("fetched %d batches over two passes, want 2", conn.fetches)
	}
}

// The denial stops the loop at the batch it happened in, not before it:
// everything admitted up to that point is stored, and only the rest waits.
func TestImapSyncKeepsWhatFitBeforeTheDenial(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 90_000}},
		changed: uidRange(3 * config.ImapFetchBatchSize),
	}
	budget := &fixedBudget{allow: config.ImapFetchBatchSize + 50}
	w, events := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if conn.fetches != 2 {
		t.Errorf("fetched %d batches, want 2 (the full first and the one that ran out)", conn.fetches)
	}
	if budget.admitted != config.ImapFetchBatchSize+50 {
		t.Errorf("admitted %d, want the whole budget spent", budget.admitted)
	}
	if got := w.SmtpImapData.Mailboxes[0].HighestModSeq; got != 100 {
		t.Errorf("mod-sequence advanced to %d with mail still on the server", got)
	}
	if hasEvent(*events, models.JobEventTypeEmailRateLimited) {
		t.Error("mailbox was deactivated by a plain budget denial")
	}
}

// The control case: with budget to spare the pass still walks every batch and
// the folder's mod-sequence moves to what the server reported.
func TestImapSyncWalksEveryBatchWithinBudget(t *testing.T) {
	conn := &fakeImapConn{
		folders: []models.Mailbox{{Name: "INBOX", UIDValidity: 7, HighestModSeq: 90_000}},
		changed: uidRange(3 * config.ImapFetchBatchSize),
	}
	budget := &fixedBudget{allow: 10 * config.ImapFetchBatchSize}
	w, _ := newIMAPTestMail(conn, budget, &models.Mailbox{Name: "INBOX", UIDValidity: 7, HighestModSeq: 100})

	if err := w.Sync(t.Context()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if conn.fetches != 3 {
		t.Errorf("fetched %d batches, want 3", conn.fetches)
	}
	if got := w.SmtpImapData.Mailboxes[0].HighestModSeq; got != 90_000 {
		t.Errorf("mod-sequence = %d, want 90000 once every change was stored", got)
	}
	if conn.released != 1 {
		t.Errorf("released the mailbox %d times, want once before LIST-STATUS", conn.released)
	}
}
