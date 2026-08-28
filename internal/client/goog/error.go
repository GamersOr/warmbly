package goog

import (
	"errors"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/pkg/mailauth"
	"google.golang.org/api/googleapi"
)

func HandleError(err error) *errx.MailError {
	if err == nil {
		return nil
	}
	// errors.As, not a type assertion: the API client wraps its error on some
	// paths, and an unwrapped assertion sends a real 401 down the transport
	// branch below.
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch gerr.Code {
		case 401:
			return errx.ErrMailGoogleAuth
		case 402:
			return errx.ErrMailGooglePayment
		case 403:
			return errx.ErrMailGoogleForbidden(gerr.Message)
		default:
			respErr := errx.ErrMailGoogleUnknown(gerr.Code, gerr.Message)
			log.Debug().Err(err).Msg("Google Api Error")
			return respErr
		}
	}

	// The token source runs inside the API call, so a grant the user revoked
	// (or Google expired) never reaches Gmail to become a 401: it fails the
	// call itself and would otherwise read as an unreachable server, promising
	// a retry that can never succeed.
	if f := mailauth.ClassifyTokenError(err); f.Refused() {
		log.Debug().
			Str("oauth_error", f.ErrorCode).
			Str("oauth_description", f.Description).
			Int("status", f.Status).
			Bool("revoked", f.Revoked()).
			Msg("Gmail token refresh refused")
		if f.Revoked() {
			return errx.ErrMailGoogleAuth
		}
	}

	// Non-API failures (DNS, TLS, timeouts) are transient transport errors.
	// Returning nil here would silently swallow them and leave callers holding
	// a typed-nil *MailError in an error interface.
	log.Debug().Err(err).Msg("Google transport error")
	return errx.ErrMailServerUnreachable
}
