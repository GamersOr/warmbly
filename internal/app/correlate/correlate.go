// Package correlate runs the nightly cross-account sweep.
//
// Every other abuse control watches one subject: a rate limit watches a user,
// warmup health a mailbox, verification an address. An actor spreading the same
// behaviour across several accounts stays under all of them. This is the pass
// that looks at the group.
package correlate

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/orgrisk"
	"github.com/warmbly/warmbly/internal/repository"
)

// Weights. Small on purpose: sharing an address is common (an office, a
// co-working space, a VPN), so this contributes evidence rather than a verdict.
const (
	weightSharedIP       = 15
	weightSharedIdentity = 25
	weightMailboxBurst   = 15

	// LookbackWindow bounds how far back accounts are correlated. Two
	// organizations opened from one office a year apart are not a cluster.
	LookbackWindow = 30 * 24 * time.Hour
	// MinClusterMembers is the group size below which nothing is recorded.
	MinClusterMembers = 3
	// MailboxBurstCount and MailboxBurstWindow describe a sending fleet being
	// stood up rather than a business connecting its inboxes.
	MailboxBurstCount  = 15
	MailboxBurstWindow = 24 * time.Hour
)

// Service runs the sweep.
type Service struct {
	repo    repository.CorrelationRepository
	orgRisk orgrisk.Service
}

func NewService(repo repository.CorrelationRepository, risk orgrisk.Service) *Service {
	return &Service{repo: repo, orgRisk: risk}
}

// Start runs the sweep on an interval until the context ends.
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	if s == nil || s.repo == nil || s.orgRisk == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Run(ctx)
		}
	}
}

// Run performs one sweep. Each finding is filed as a signal on every member
// organization; the band it produces is the risk service's decision, not this
// package's.
func (s *Service) Run(ctx context.Context) {
	since := time.Now().Add(-LookbackWindow)
	recorded := 0

	if clusters, err := s.repo.ClustersBySignupIP(ctx, MinClusterMembers, since); err != nil {
		log.Warn().Err(err).Msg("correlation sweep: signup-ip clustering failed")
	} else {
		recorded += s.record(ctx, clusters, "cluster_signup_ip", weightSharedIP,
			"opened alongside %d other workspaces from one address")
	}

	if clusters, err := s.repo.ClustersBySignupIdentity(ctx, MinClusterMembers, since); err != nil {
		log.Warn().Err(err).Msg("correlation sweep: identity clustering failed")
	} else {
		recorded += s.record(ctx, clusters, "cluster_signup_identity", weightSharedIdentity,
			"opened alongside %d other workspaces by the same email identity")
	}

	if bursts, err := s.repo.OrgsConnectingMailboxesFast(ctx, MailboxBurstCount, MailboxBurstWindow); err != nil {
		log.Warn().Err(err).Msg("correlation sweep: mailbox burst query failed")
	} else {
		for _, b := range bursts {
			s.signal(ctx, b.OrganizationIDs, "mailbox_burst", weightMailboxBurst,
				fmt.Sprintf("connected %d or more mailboxes within %s", MailboxBurstCount, MailboxBurstWindow))
			recorded++
		}
	}

	if recorded > 0 {
		log.Info().Int("findings", recorded).Msg("correlation sweep recorded cross-account findings")
	}
}

func (s *Service) record(ctx context.Context, clusters []repository.Cluster, key string, weight int, format string) int {
	n := 0
	for _, c := range clusters {
		if len(c.OrganizationIDs) < MinClusterMembers {
			continue
		}
		// "%d OTHER workspaces", so the sentence reads correctly for a member.
		detail := fmt.Sprintf(format, len(c.OrganizationIDs)-1)
		s.signal(ctx, c.OrganizationIDs, key, weight, detail)
		n++
	}
	return n
}

func (s *Service) signal(ctx context.Context, orgs []uuid.UUID, key string, weight int, detail string) {
	for _, orgID := range orgs {
		if _, err := s.orgRisk.RecordSignal(ctx, orgID, orgrisk.Signal{
			Key: key, Weight: weight, Detail: detail,
		}); err != nil {
			log.Warn().Str("organization_id", orgID.String()).Str("signal", key).
				Msg("correlation sweep: could not record signal")
		}
	}
}
