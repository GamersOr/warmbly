package wmail

import (
	"context"
	"time"

	goimap "github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/client/smtpimap/imap"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// ImapConn is everything the worker drives on one IMAP connection.
// SmtpImapData holds this interface rather than *imap.Client so a sync pass
// can be run against a fake and its round trips counted; production always
// holds an *imap.Client.
type ImapConn interface {
	// Sync pass.
	Folders() ([]models.Mailbox, *errx.MailError)
	ReleaseMailbox()
	SelectForSync(mailbox string) (uint32, *errx.MailError)
	SearchChangedSince(modSeq uint64) ([]goimap.UID, *errx.MailError)
	SearchSince(since time.Time) ([]goimap.UID, *errx.MailError)
	FetchEnvelopes(ctx context.Context, uids []goimap.UID) ([]*imap.Fetched, *errx.MailError)
	FetchBody(f *imap.Fetched)

	// Send path.
	AppendToSent(ctx context.Context, raw []byte, sentAt time.Time) error

	// Warmup actions.
	MarkAsRead(ctx context.Context, mailboxName string, uid uint32) error
	MarkImportant(ctx context.Context, mailboxName string, uid uint32) error
	MoveToFolder(ctx context.Context, sourceMailbox, dstFolder string, uid uint32) error
	RemoveFromSpam(ctx context.Context, sourceMailbox, inboxName string, uid uint32) error
}

var _ ImapConn = (*imap.Client)(nil)
