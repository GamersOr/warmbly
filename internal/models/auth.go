package models

// ExternalAuthProviders is what GET /auth/providers advertises so one shipped
// native app binary can discover which social sign-in options a (self-)hosted
// backend supports. Client IDs here are public identifiers, not secrets.
//
// Browser social sign-in is separate and lives behind GET /auth/config: the
// dashboard needs to know which buttons to render, not which client to
// authenticate against, because the whole flow runs server-side.
type ExternalAuthProviders struct {
	AppleBundleID     string
	GoogleIOSClientID string
}
