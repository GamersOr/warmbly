package wmail

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/client/goog"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

// The checkpoint row is keyed (user_id, email_id) with a foreign key to users,
// so an event carrying zero UUIDs is rejected by Postgres and the mailbox never
// gets a checkpoint at all.
func TestNewHistoryIDCarriesRowKey(t *testing.T) {
	userID := uuid.New()
	emailID := uuid.New()

	var got *models.JobEventHistoryIDUpdate
	w := &WMail{
		UserID: userID,
		ID:     emailID,
		onEvent: func(jobType models.JobEventType, body any) error {
			if jobType != models.JobEventTypeHistoryIDUpdate {
				t.Errorf("published %v, want %v", jobType, models.JobEventTypeHistoryIDUpdate)
			}
			got = body.(*models.JobEventHistoryIDUpdate)
			return nil
		},
	}

	if err := w.NewHistoryID(65207); err != nil {
		t.Fatalf("NewHistoryID: %v", err)
	}

	if got == nil {
		t.Fatal("no event was published")
	}
	if got.UserID != userID {
		t.Errorf("UserID = %v, want %v", got.UserID, userID)
	}
	if got.EmailID != emailID {
		t.Errorf("EmailID = %v, want %v", got.EmailID, emailID)
	}
	if got.HistoryID != 65207 {
		t.Errorf("HistoryID = %d, want 65207", got.HistoryID)
	}
	if got.UserID == uuid.Nil || got.EmailID == uuid.Nil {
		t.Error("event carries a nil UUID, which violates the users foreign key")
	}
}

// fakeGmail is the Gmail API as one backfill sees it: a messages.list that can
// be made to refuse, and a messages.get for whatever it did list.
type fakeGmail struct {
	ids       []string
	failList  int // list calls left to refuse (-1 for always)
	listCalls int
}

func (g *fakeGmail) serve(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		id, isGet := strings.CutPrefix(r.URL.Path, "/gmail/v1/users/me/messages/")
		if isGet {
			_, _ = fmt.Fprintf(w, `{"id":%q,"threadId":"t-%s","payload":{"headers":[{"name":"Message-Id","value":"<%s@gmail.test>"},{"name":"Subject","value":"history"}]}}`, id, id, id)
			return
		}
		if g.failList != 0 {
			if g.failList > 0 {
				g.failList--
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"injected"}}`))
			return
		}
		g.listCalls++
		msgs := make([]string, 0, len(g.ids))
		for _, id := range g.ids {
			msgs = append(msgs, fmt.Sprintf(`{"id":%q,"threadId":"t-%s"}`, id, id))
		}
		_, _ = fmt.Fprintf(w, `{"messages":[%s]}`, strings.Join(msgs, ","))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newGoogleTestMail(t *testing.T, srv *httptest.Server, events *[]captured) *WMail {
	t.Helper()
	w := &WMail{
		ID:                        uuid.New(),
		UserID:                    uuid.New(),
		Email:                     "box@gmail.test",
		EmailType:                 models.InboxProviderGoogle,
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		gov:                       newGovernor(uuid.New(), nil, nil, models.SyncPolicy{}),
	}
	w.onEvent = func(kind models.JobEventType, body any) error {
		*events = append(*events, captured{eventType: kind, body: body})
		return nil
	}
	w.tracker = newSyncTracker(nil, func(models.SyncState) error { return nil })

	client := &goog.Client{
		Email:          w.Email,
		OnMessageAdded: w.onGoogleMessageAdded,
		OnTokenRefresh: func(context.Context, *oauth2.Token) error { return nil },
	}
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: rewriteToTestServer(srv.URL)})
	token := &oauth2.Token{AccessToken: "live", Expiry: time.Now().Add(time.Hour)}
	if merr := client.Init(ctx, token, oauth2.Config{}); merr != nil {
		t.Fatalf("client init: %v", merr.Message)
	}
	w.GoogleData = &GoogleData{Client: client}
	return w
}

// The Graph defect's shape, checked on the Gmail import: a refused listing
// ends the pass with the page token where it was, and never reports the
// history as imported.
func TestGoogleBackfillRetriesAfterATransientFailure(t *testing.T) {
	g := &fakeGmail{ids: []string{"g1", "g2"}, failList: 1}
	var events []captured
	w := newGoogleTestMail(t, g.serve(t), &events)

	if merr := w.googleBackfill(t.Context(), &tickStats{}); merr == nil {
		t.Fatal("a refused listing was swallowed; the pass must end so the import is retried")
	}
	if st := w.tracker.state.BackfillStatus; st == models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s after a failed listing", st)
	}
	if tok := w.tracker.state.BackfillCursor.PageToken; tok != "" {
		t.Errorf("page token = %q, want it held where it was", tok)
	}

	if merr := w.googleBackfill(t.Context(), &tickStats{}); merr != nil {
		t.Fatalf("second pass: %v", merr.Message)
	}
	if st := w.tracker.state.BackfillStatus; st != models.SyncBackfillComplete {
		t.Fatalf("backfill status = %s, want %s", st, models.SyncBackfillComplete)
	}
	if got := len(importedIDs(events)); got != 2 {
		t.Errorf("imported %d messages, want 2: %v", got, importedIDs(events))
	}
	if g.listCalls != 1 {
		t.Errorf("listed %d times, want the one call that succeeded", g.listCalls)
	}
}
