package tasks

import "github.com/warmbly/warmbly/internal/models"

// trackingOverrideIgnored is a tracking domain that was configured but not
// used, with the sentence the campaign log records for it.
type trackingOverrideIgnored struct {
	Scope   string
	Domain  string
	Message string
}

// resolveTrackingHost picks the host open pixels and click tickets are built
// from: a VERIFIED campaign override wins, then a VERIFIED mailbox domain,
// otherwise this install's own tracking host.
//
// Only a verified override is honored. An unresolved host could point tracking
// at a target somebody else controls (SSRF-adjacent, matching the
// webhook-safety posture), and an unresolvable one is worse than no tracking:
// every link in the email becomes a ticket on a host that does not answer, so
// the recipient cannot reach the destination at all.
func resolveTrackingHost(defaultHost string, account *models.Email, campaign *models.Campaign) (string, []trackingOverrideIgnored) {
	host := defaultHost
	var ignored []trackingOverrideIgnored

	if account != nil && account.TrackingDomain != "" {
		if account.TrackingDomainVerified {
			host = account.TrackingDomain
		} else {
			ignored = append(ignored, trackingOverrideIgnored{
				Scope:   "mailbox",
				Domain:  account.TrackingDomain,
				Message: "Mailbox tracking domain is not verified; tracking through the shared host instead",
			})
		}
	}

	if campaign != nil && campaign.TrackingDomain != "" {
		if campaign.TrackingDomainVerified {
			host = campaign.TrackingDomain
		} else {
			ignored = append(ignored, trackingOverrideIgnored{
				Scope:   "campaign",
				Domain:  campaign.TrackingDomain,
				Message: "Campaign tracking domain is not verified; tracking through the mailbox default instead",
			})
		}
	}

	return host, ignored
}
