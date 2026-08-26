package jobs

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks of warmup verification against a real Postgres. Skipped unless
// WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/consumer/ -run Live -v
//
// Issue #193: Microsoft Graph drops custom headers in transit and re-stamps the
// Message-ID, so warmup sent from an Outlook mailbox arrives at every recipient
// carrying no verify header and under an id Warmbly never minted. Matched only
// on the header it counted for nobody and was filed in the recipient's unibox
// as ordinary mail. What is worth proving here is the recipient-side
// resolution, which is all SQL.

type warmupFixture struct {
	senderUser, senderOrg, sender       uuid.UUID
	partnerUser, partnerOrg, partner    uuid.UUID
	senderEmail, partnerEmail           string
	task                                uuid.UUID
	subject, mintedMessageID, sentMsgID string
}

func newWarmupFixture(t *testing.T, handle *db.DB) *warmupFixture {
	t.Helper()
	ctx := context.Background()
	pool := handle.Pool
	f := &warmupFixture{
		senderUser: uuid.New(), senderOrg: uuid.New(), sender: uuid.New(),
		partnerUser: uuid.New(), partnerOrg: uuid.New(), partner: uuid.New(),
		task:            uuid.New(),
		subject:         "quick learning question",
		mintedMessageID: "<" + uuid.NewString() + "@outlook.com>",
		sentMsgID:       "<AS8P" + uuid.NewString()[:8] + "@AS8P123.eurprd04.prod.outlook.com>",
	}
	f.senderEmail = "wsender-" + f.sender.String()[:8] + "@outlook.com"
	f.partnerEmail = "wpartner-" + f.partner.String()[:8] + "@test.local"

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	mailbox := func(user, org, id uuid.UUID, email, provider string) {
		exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Warm', 'Up')`,
			user, "wu-"+user.String()[:8]+"@test.local")
		exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Warmup Live', $2, $3)`,
			org, "wu-"+org.String()[:8], user)
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
		          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'Warmup', '', '', $5, 'active', 50, 600, 'UTC')`,
			id, user, org, email, provider)
	}
	mailbox(f.senderUser, f.senderOrg, f.sender, f.senderEmail, "outlook")
	mailbox(f.partnerUser, f.partnerOrg, f.partner, f.partnerEmail, "smtp_imap")

	// completed_at is backdated past the reply path's 45-minute human-timing
	// floor so GetLatestReplyCandidate can see this send.
	now := time.Now()
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, scheduled_at, completed_at, message_id)
	      VALUES ($1, 'warmup', $2, 'completed', $3, NOW() - INTERVAL '2 hours', $4)`,
		f.task, f.sender, now, f.mintedMessageID)

	t.Cleanup(func() {
		c := context.Background()
		for _, s := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM unibox_emails WHERE email_id = $1`, f.partner},
			{`DELETE FROM warmup_received WHERE sender_account_id = $1`, f.sender},
			{`DELETE FROM warmup_tokens WHERE sender_account_id = $1`, f.sender},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.sender},
			{`DELETE FROM email_accounts WHERE id = $1`, f.sender},
			{`DELETE FROM email_accounts WHERE id = $1`, f.partner},
			{`DELETE FROM organizations WHERE id = $1`, f.senderOrg},
			{`DELETE FROM organizations WHERE id = $1`, f.partnerOrg},
			{`DELETE FROM users WHERE id = $1`, f.senderUser},
			{`DELETE FROM users WHERE id = $1`, f.partnerUser},
		} {
			if _, err := pool.Exec(c, s.sql, s.arg); err != nil {
				t.Errorf("cleanup %q: %v", s.sql, err)
			}
		}
	})
	return f
}

// mintToken writes the token the warmup task would have created.
func (f *warmupFixture) mintToken(t *testing.T, repo repository.WarmupRepository) uuid.UUID {
	t.Helper()
	token := uuid.New()
	err := repo.CreateWarmupToken(context.Background(), &models.WarmupToken{
		Token:              token,
		TaskID:             f.task,
		SenderAccountID:    f.sender,
		RecipientAccountID: f.partner,
		ConversationTheme:  "learning",
		ContentSource:      models.WarmupContentSourceStatic,
		Subject:            f.subject,
		ExpiresAt:          time.Now().Add(7 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create warmup token: %v", err)
	}
	return token
}

// arrival is the inbound event the recipient's worker produces. Outlook-sent
// warmup has no verify pseudo-flag and a Message-ID Warmbly never minted.
func (f *warmupFixture) arrival(messageID string, flags []string) *models.JobEventNewEmail {
	return &models.JobEventNewEmail{
		UserID: f.partnerUser,
		Message: &models.EmailMessageStoreData{
			ID:        uuid.New(),
			EmailID:   f.partner,
			MessageID: messageID,
			Subject:   f.subject,
			FromAddr:  []string{"Warm Up <" + f.senderEmail + ">"},
			ToAddr:    []string{f.partnerEmail},
			Flags:     flags,
		},
	}
}

func liveWarmupService(t *testing.T) (*JobsService, *db.DB) {
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
	return &JobsService{
		TaskRepo:         repository.NewTaskRepository(handle.Pool),
		WarmupRepo:       repository.NewWarmupRepository(handle.Pool),
		UniboxRepository: repository.NewUniboxRepository(handle),
		EmailRepository:  repository.NewEmailRepostory(handle, nil),
	}, handle
}

// The regression: a warmup email that arrives with no verify header must still
// be verified, consumed and kept out of the recipient's unibox.
func TestLiveWarmupVerifiesByDeliveredMessageIDWhenTheHeaderIsStripped(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	token := f.mintToken(t, s.WarmupRepo)

	// The worker answers the send with the id Exchange stamped, not ours.
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID: f.task, Success: true, MessageID: f.sentMsgID,
	}); err != nil {
		t.Fatalf("HandleEmailSent: %v", err)
	}

	// The task now carries what the recipient will actually see, so warmup
	// reply threading and campaign reply matching can find it.
	task, err := s.TaskRepo.GetTask(ctx, f.task)
	if err != nil || task == nil {
		t.Fatalf("get task: %v", err)
	}
	if task.MessageID != f.sentMsgID {
		t.Errorf("task message_id = %q, want the delivered id %q", task.MessageID, f.sentMsgID)
	}

	stored, err := s.WarmupRepo.FindWarmupToken(ctx, token)
	if err != nil || stored == nil {
		t.Fatalf("find token: %v", err)
	}
	if stored.SentMessageID != f.sentMsgID {
		t.Errorf("token sent_message_id = %q, want %q", stored.SentMessageID, f.sentMsgID)
	}

	// Arrival at the partner: no verify flag anywhere.
	if !s.handleUnmarkedWarmupEmail(ctx, f.arrival(f.sentMsgID, nil)) {
		t.Fatal("warmup arriving without its verify header must still be verified")
	}
	if stored, _ = s.WarmupRepo.FindWarmupToken(ctx, token); stored == nil || stored.ConsumedAt == nil {
		t.Error("the token must be consumed, or the send counts for nobody")
	}
}

// The Message-ID is the strongest key, but it depends on the worker's send
// result having landed first. When it has not, the pending token for this
// exact sender/recipient/subject still resolves.
func TestLiveWarmupVerifiesByPairAndSubjectBeforeTheSendResultLands(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	token := f.mintToken(t, s.WarmupRepo)

	// No HandleEmailSent yet: sent_message_id is still empty.
	if !s.handleUnmarkedWarmupEmail(ctx, f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)) {
		t.Fatal("a pending token for this pair and subject must resolve")
	}
	stored, err := s.WarmupRepo.FindWarmupToken(ctx, token)
	if err != nil || stored == nil || stored.ConsumedAt == nil {
		t.Errorf("token should be consumed: %+v (err %v)", stored, err)
	}
}

// The pair fallback must not swallow real mail. A different subject from the
// same sender is ordinary mail and belongs in the unibox.
func TestLiveWarmupPairFallbackIgnoresADifferentSubject(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	f.mintToken(t, s.WarmupRepo)

	e := f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)
	e.Message.Subject = "your invoice is overdue"
	if s.handleUnmarkedWarmupEmail(ctx, e) {
		t.Error("mail with a different subject must not claim a pending warmup token")
	}
}

// ...and neither must mail from someone who is not the token's sender.
func TestLiveWarmupPairFallbackIgnoresADifferentSender(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	f.mintToken(t, s.WarmupRepo)

	e := f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)
	e.Message.FromAddr = []string{"Someone Else <stranger@example.com>"}
	if s.handleUnmarkedWarmupEmail(ctx, e) {
		t.Error("mail from a different sender must not claim a pending warmup token")
	}
}

// A token that was already consumed cannot be claimed twice, so a duplicate
// delivery of the same warmup message falls through to normal processing
// rather than firing the engagement plan again.
func TestLiveWarmupConsumedTokenIsNotReclaimed(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	token := f.mintToken(t, s.WarmupRepo)
	if err := s.WarmupRepo.ConsumeWarmupToken(ctx, token); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if s.handleUnmarkedWarmupEmail(ctx, f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)) {
		t.Error("an already-consumed token must not be claimed again")
	}
}

// The header path is untouched: providers that keep the header still verify
// through it, and the header-less lookup is not consulted.
func TestLiveWarmupStillVerifiesThroughTheHeaderWhenItSurvives(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	token := f.mintToken(t, s.WarmupRepo)

	e := f.arrival(f.mintedMessageID, []string{config.WarmupVerifyHeader + ":" + token.String()})
	handled, err := s.handleWarmupEmail(ctx, e, token.String())
	if err != nil {
		t.Fatalf("handleWarmupEmail: %v", err)
	}
	if !handled {
		t.Fatal("a valid verify header must still verify")
	}
	stored, _ := s.WarmupRepo.FindWarmupToken(ctx, token)
	if stored == nil || stored.ConsumedAt == nil {
		t.Error("the token must be consumed")
	}
}

// The symptom the issue reports: unverified warmup mail becomes an ordinary
// unibox entry in the recipient's inbox. This drives the whole handler, so it
// proves the message never reaches the unibox at all.
func TestLiveWarmupEmailWithNoHeaderNeverReachesTheUnibox(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	f.mintToken(t, s.WarmupRepo)

	e := f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)
	if err := s.HandleNewEmail(ctx, e); err != nil {
		t.Fatalf("HandleNewEmail: %v", err)
	}
	var stored int
	if err := handle.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM unibox_emails WHERE email_id = $1`, f.partner).Scan(&stored); err != nil {
		t.Fatalf("count unibox: %v", err)
	}
	if stored != 0 {
		t.Errorf("verified warmup mail must not be filed in the unibox, found %d entries", stored)
	}

	// A message with no pending token is ordinary mail and still lands.
	other := f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)
	other.Message.Subject = "actual customer question"
	if err := s.HandleNewEmail(ctx, other); err != nil {
		t.Fatalf("HandleNewEmail (ordinary): %v", err)
	}
	if err := handle.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM unibox_emails WHERE email_id = $1`, f.partner).Scan(&stored); err != nil {
		t.Fatalf("count unibox: %v", err)
	}
	if stored != 1 {
		t.Errorf("ordinary mail must still be filed in the unibox, found %d entries", stored)
	}
}

func TestFirstSenderAddress(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Warm Up <a@b.com>", "a@b.com"},
		{"a@b.com", "a@b.com"},
		{`"Up, Warm" <a@b.com>`, "a@b.com"},
		{"<a@b.com>", "a@b.com"},
		{"not an address", ""},
		{"", ""},
	} {
		if got := firstSenderAddress([]string{tc.in}); got != tc.want {
			t.Errorf("firstSenderAddress(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := firstSenderAddress(nil); got != "" {
		t.Errorf("firstSenderAddress(nil) = %q, want empty", got)
	}
}

// The other half of the Message-ID rewrite: a partner replying to this send
// threads on whatever the reply candidate reports. Reporting the id we minted
// points In-Reply-To at a message the Outlook mailbox has never held, so the
// reply lands as a new conversation and warmup stops looking conversational.
func TestLiveWarmupReplyCandidateUsesTheDeliveredMessageID(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	f.mintToken(t, s.WarmupRepo)

	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID: f.task, Success: true, MessageID: f.sentMsgID,
	}); err != nil {
		t.Fatalf("HandleEmailSent: %v", err)
	}

	candidate, err := s.WarmupRepo.GetLatestReplyCandidate(ctx, f.sender, f.partner)
	if err != nil {
		t.Fatalf("GetLatestReplyCandidate: %v", err)
	}
	if candidate == nil {
		t.Fatal("the send should be replyable")
	}
	if candidate.MessageID != f.sentMsgID {
		t.Errorf("reply candidate message id = %q, want the delivered id %q", candidate.MessageID, f.sentMsgID)
	}
}

// An expired token is not claimable: a stale pending row must not swallow mail
// that arrives days later.
func TestLiveWarmupExpiredTokenIsNotClaimed(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	token := f.mintToken(t, s.WarmupRepo)
	if _, err := handle.Pool.Exec(ctx,
		`UPDATE warmup_tokens SET expires_at = NOW() - INTERVAL '1 hour' WHERE token = $1`, token); err != nil {
		t.Fatalf("expire token: %v", err)
	}
	if s.handleUnmarkedWarmupEmail(ctx, f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)) {
		t.Error("an expired token must not be claimed")
	}
}

// The pair fallback is time-boxed, so an old pending token cannot claim mail
// that happens to repeat the subject much later.
func TestLiveWarmupPairFallbackIsTimeBoxed(t *testing.T) {
	s, handle := liveWarmupService(t)
	ctx := context.Background()
	f := newWarmupFixture(t, handle)
	token := f.mintToken(t, s.WarmupRepo)
	if _, err := handle.Pool.Exec(ctx,
		`UPDATE warmup_tokens SET created_at = NOW() - INTERVAL '5 days' WHERE token = $1`, token); err != nil {
		t.Fatalf("backdate token: %v", err)
	}
	if s.handleUnmarkedWarmupEmail(ctx, f.arrival("<"+uuid.NewString()+"@outlook.com>", nil)) {
		t.Error("the pair fallback must not reach back past its window")
	}

	// The Message-ID key has no such window: it is exact, so it still resolves.
	if err := s.HandleEmailSent(ctx, models.SendEmailResult{
		TaskID: f.task, Success: true, MessageID: f.sentMsgID,
	}); err != nil {
		t.Fatalf("HandleEmailSent: %v", err)
	}
	if !s.handleUnmarkedWarmupEmail(ctx, f.arrival(f.sentMsgID, nil)) {
		t.Error("an exact delivered Message-ID must still resolve an older token")
	}
}
