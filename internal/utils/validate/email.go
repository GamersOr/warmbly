package validate

import (
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/errx"
)

func Email(email string) bool {
	_, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}
	return true
}

func EmailBulk(emails []string) bool {
	for i := range emails {
		if !Email(emails[i]) {
			return false
		}
	}
	return true
}

var nameRE = regexp.MustCompile(`^\p{L}[\p{L}\p{M}\p{N}'\-\.\s]{0,98}\p{L}$`)

func EmailName(name *string) bool {
	*name = strings.TrimSpace(*name)
	if *name == "" {
		return false
	}
	runes := []rune(*name)
	if len(runes) < 2 {
		return false
	}
	if len(runes) > 100 {
		return false
	}
	if !nameRE.MatchString(*name) {
		return false
	}
	return true
}

// ValidateTrackingDomain checks a mailbox's custom tracking domain against the
// same rule as the campaign override. Callers normalize the value first, so a
// pasted URL is reduced to its host rather than rejected.
func ValidateTrackingDomain(domain string) *errx.Error {
	if domain == "" {
		return errx.ErrEmailTrackingDomain
	}
	if len(domain) > 253 {
		return errx.ErrEmailTrackingDomainLength
	}
	if !TrackingHostname(strings.ToLower(domain)) {
		return errx.ErrEmailTrackingDomain
	}
	return nil
}

// EmailTimezone accepts an IANA zone name the runtime can actually load, or the
// empty string meaning "not configured". A zone that does not resolve would be
// silently coerced to UTC by the scheduler, which is how a mailbox ends up
// sending at the wrong local hour with nothing to point at.
func EmailTimezone(tz string) *errx.Error {
	if tz == "" {
		return nil
	}
	if len(tz) > 64 {
		return errx.ErrEmailTimezone
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return errx.ErrEmailTimezone
	}
	return nil
}
