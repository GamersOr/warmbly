package tasks

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// warmupReconcileBatch caps how many mailboxes a single reconcile pass will
// (re)seed. Plenty for steady state; the next tick mops up any overflow.
const warmupReconcileBatch = 500

// ReconcileWarmupSchedules (re)seeds warmup chains for mailboxes that should
// be warming but have no pending warmup task — either because warmup was just
// enabled, the mailbox joined a live campaign (health-check lane), or a prior
// chain wound down. Returns the number of chains seeded this pass.
//
// This is the single bootstrap for warmup: enabling warmup or starting a
// campaign does not itself enqueue a task, so without this pass a freshly
// enabled mailbox would never start warming.
func (s *tasksService) ReconcileWarmupSchedules(ctx context.Context, limit int) (int, error) {
	// Same lost-callback backstop as the campaign reconciler: a pending
	// warmup task stranded past its slot blocks the candidate query below.
	if n, err := s.taskRepo.CancelOverduePendingTasks(ctx, "warmup", overduePendingGrace); err == nil && n > 0 {
		log.Info().Int64("cancelled", n).Msg("warmup reconcile: cancelled overdue pending tasks")
	}

	ids, err := s.emailRepo.ListWarmupScheduleCandidates(ctx, limit)
	if err != nil {
		return 0, err
	}

	seeded := 0
	for _, id := range ids {
		account, xerr := s.emailRepo.GetByID(ctx, id)
		if xerr != nil || account == nil || account.OrganizationID == nil {
			continue
		}
		if s.featureGate != nil {
			canWarmup, xerr := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
			if !canWarmup {
				// Only a definite "not entitled" evicts; an unreadable
				// subscription leaves the membership alone.
				if s.warmupHealth != nil && xerr == nil {
					_ = s.warmupHealth.RemoveFromAllPools(ctx, account.ID)
				}
				continue
			}
		}

		// EnsureWarmupScheduled is idempotent and returns ErrWarmupNotEnabled
		// for mailboxes that raced out of an eligible state — both benign, so
		// we skip rather than abort the whole pass.
		if err := s.EnsureWarmupScheduled(ctx, id); err != nil {
			continue
		}
		seeded++
	}
	return seeded, nil
}

// StartWarmupReconciler runs ReconcileWarmupSchedules on an interval until the
// context is cancelled. Mirrors the other background sweeps (warmup health,
// dead-worker) and is started from the backend, which owns Cloud Tasks.
func (s *tasksService) StartWarmupReconciler(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Seed once on boot so chains recover promptly after a restart instead of
	// waiting a full interval.
	s.reconcileOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reconcileOnce(ctx)
		}
	}
}

func (s *tasksService) reconcileOnce(ctx context.Context) {
	rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	seeded, err := s.ReconcileWarmupSchedules(rctx, warmupReconcileBatch)
	if err != nil {
		log.Warn().Err(err).Msg("warmup reconcile pass failed")
	} else if seeded > 0 {
		log.Info().Int("seeded", seeded).Msg("warmup reconcile seeded chains")
	}

	moved, removed, err := s.ReconcileWarmupPoolMembership(rctx)
	if err != nil {
		log.Warn().Err(err).Msg("warmup pool membership reconcile pass failed")
		return
	}
	if moved > 0 || removed > 0 {
		log.Info().Int("moved", moved).Int("removed", removed).Msg("warmup pool membership reconciled")
	}
}

// ReconcileWarmupPoolMembership walks every warmup pool participant and makes
// its membership agree with the mailbox's entitlement: a mailbox that may no
// longer warm leaves warmup, and one whose tier changed is moved to the pool
// that tier belongs to.
//
// The warmup task fixes membership too, but only for mailboxes it still runs
// for. A mailbox that stops warming (warmup switched off, campaign finished,
// subscription lapsed) has no chain and is not a schedule candidate, so nothing
// would ever revisit it and its row would sit in the pool forever (issue #211).
// This is the pass that ends that.
//
// Roles are left alone: a mailbox demoted to recipient_only (failing domain
// auth, say) must not be quietly promoted back by a sweep that knows nothing
// about why it was demoted.
func (s *tasksService) ReconcileWarmupPoolMembership(ctx context.Context) (moved int, removed int, err error) {
	if s.warmupRepo == nil || s.warmupHealth == nil {
		return 0, 0, nil
	}

	ids, err := s.warmupRepo.GetAllParticipantAccountIDs(ctx)
	if err != nil {
		return 0, 0, err
	}

	// One subscription lookup per organization, not per mailbox: a workspace
	// with fifty inboxes is one question asked fifty times otherwise.
	entitled := map[uuid.UUID]bool{}

	for _, id := range ids {
		account, xerr := s.emailRepo.GetByID(ctx, id)
		if xerr != nil {
			continue
		}
		if account == nil || account.Status != "active" || account.OrganizationID == nil {
			// No mailbox, no active mailbox, or no workspace to check an
			// entitlement against: it does not belong in a shared pool.
			if xerr := s.warmupHealth.RemoveFromAllPools(ctx, id); xerr == nil {
				removed++
			}
			continue
		}

		if s.featureGate != nil {
			ok, cached := entitled[*account.OrganizationID]
			if !cached {
				canWarmup, xerr := s.featureGate.CanUseWarmup(ctx, *account.OrganizationID)
				if xerr != nil {
					// Entitlement unknown: leave this mailbox exactly as it is.
					continue
				}
				ok = canWarmup
				entitled[*account.OrganizationID] = ok
			}
			if !ok {
				if xerr := s.warmupHealth.RemoveFromAllPools(ctx, account.ID); xerr == nil {
					removed++
					log.Info().
						Str("email_account_id", account.ID.String()).
						Msg("warmup reconcile: mailbox left warmup, organization is no longer entitled")
				}
				continue
			}
		}

		poolType := s.resolveWarmupPoolType(ctx, account)
		didMove, xerr := s.warmupHealth.MovePoolMembership(ctx, account.ID, poolType)
		if xerr != nil {
			continue
		}
		if didMove {
			moved++
			log.Info().
				Str("email_account_id", account.ID.String()).
				Str("pool_type", poolType).
				Msg("warmup reconcile: mailbox moved to the pool its tier belongs to")
		}
	}

	return moved, removed, nil
}
