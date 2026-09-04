package imap

import (
	"errors"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// imapStatus is the most specific description the server gave for a failed
// command. The bracketed response code is optional (RFC 9051 7.1): a bare
// NO/BAD carries its reason in the free-form text only, so fall back to that
// instead of reporting an empty status.
func imapStatus(err *imap.Error) string {
	code, text := string(err.Code), strings.TrimSpace(err.Text)
	switch {
	case code != "" && text != "":
		return code + ": " + text
	case code != "":
		return code
	default:
		return text
	}
}

func (c *Client) handleError(err error) *errx.MailError {
	var imapErr *imap.Error
	if errors.As(err, &imapErr) {
		switch imapErr.Code {
		case imap.ResponseCodeAuthenticationFailed:
			if c.AuthType == models.AuthOAuth2 {
				return errx.ErrMailAuthenticationFailed
			} else {
				return errx.ErrMailInvalidCredentials
			}
		case imap.ResponseCodeAuthorizationFailed:
			return errx.ErrMailAuthorizationFailed
		default:
			return errx.ErrMailUnknownImapError(imapStatus(imapErr))
		}
	}

	return nil
}
