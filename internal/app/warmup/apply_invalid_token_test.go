package warmup

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Embeds the interface so only the methods this path touches need stubbing; any
// other call panics loudly rather than silently passing.
type stubWarmupRepo struct {
	repository.WarmupRepository

	recordCalls  int
	scoreCalls   int
	getHealthErr error
	metricsErr   error
	updateErr    error
	health       *models.WarmupParticipantHealth
}

func (r *stubWarmupRepo) RecordInvalidTokenAttempt(_ context.Context, _ uuid.UUID, _ string) error {
	r.recordCalls++
	return nil
}

func (r *stubWarmupRepo) IncrementSpamScore(_ context.Context, _ uuid.UUID, _ int) (int, error) {
	r.scoreCalls++
	return 0, nil
}

func (r *stubWarmupRepo) GetParticipantHealth(_ context.Context, _ uuid.UUID, _ string) (*models.WarmupParticipantHealth, error) {
	if r.getHealthErr != nil {
		return nil, r.getHealthErr
	}
	return r.health, nil
}

func (r *stubWarmupRepo) GetParticipantHealthForAccount(_ context.Context, _ uuid.UUID) (*models.WarmupParticipantHealth, error) {
	if r.getHealthErr != nil {
		return nil, r.getHealthErr
	}
	return r.health, nil
}

func (r *stubWarmupRepo) UpdateParticipantHealth(_ context.Context, _ uuid.UUID, _ models.WarmupHealthState, _ *time.Time, _ string, _ float64) error {
	return r.updateErr
}

func (r *stubWarmupRepo) SumWarmupSentSince(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, r.metricsErr
}

func (r *stubWarmupRepo) CountSpamPlacementsSince(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

func (r *stubWarmupRepo) CountUserComplaintsSince(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

func (r *stubWarmupRepo) CountRecentInvalidAttempts(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

func (r *stubWarmupRepo) GetSpamScore(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (r *stubWarmupRepo) CountDeliverabilityEventsByAccount(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (int, error) {
	return 0, nil
}

func (r *stubWarmupRepo) CountDeliveredByAccount(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

// The invariant the caller depends on: applyInvalidWarmupAttempt re-records the
// attempt and the score whenever this returns an error, so once anything has
// been persisted the call must not report failure. Reporting one is how every
// attempt came to be counted twice (#195).
func TestApplyInvalidTokenAttemptNeverReportsFailureAfterRecording(t *testing.T) {
	cases := []struct {
		name string
		repo *stubWarmupRepo
	}{
		{
			// The failure mode that actually happened: the pool probe itself
			// breaks, so the fallback read breaks with it.
			name: "pool probe fails",
			repo: &stubWarmupRepo{getHealthErr: errors.New("connection reset by peer")},
		},
		{
			name: "metrics load fails",
			repo: &stubWarmupRepo{
				health:     &models.WarmupParticipantHealth{HealthState: models.WarmupHealthHealthy},
				metricsErr: errors.New("statement timeout"),
			},
		},
		{
			name: "persist fails",
			repo: &stubWarmupRepo{
				health:    &models.WarmupParticipantHealth{HealthState: models.WarmupHealthHealthy},
				updateErr: errors.New("deadlock detected"),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewService(c.repo).ApplyInvalidTokenAttempt(context.Background(), uuid.New(), "tok", 5)

			if err != nil {
				t.Fatalf("returned %v after the attempt was persisted; the caller's degraded path will record it again", err)
			}
			if c.repo.recordCalls != 1 {
				t.Fatalf("attempt recorded %d times, want 1", c.repo.recordCalls)
			}
			if c.repo.scoreCalls != 1 {
				t.Fatalf("score incremented %d times, want 1", c.repo.scoreCalls)
			}
		})
	}
}

// A failure BEFORE anything is persisted must still be reported, so the caller's
// degraded path can retry the recording. That path is what it documents itself
// as being for.
func TestApplyInvalidTokenAttemptReportsAFailedRecording(t *testing.T) {
	repo := &failingRecordRepo{}

	_, err := NewService(repo).ApplyInvalidTokenAttempt(context.Background(), uuid.New(), "tok", 5)
	if err == nil {
		t.Fatal("expected an error so the caller retries the recording")
	}
}

type failingRecordRepo struct {
	repository.WarmupRepository
}

func (r *failingRecordRepo) RecordInvalidTokenAttempt(_ context.Context, _ uuid.UUID, _ string) error {
	return errors.New("write failed")
}

// The evaluation runs on the mailbox's own pool row. Probing "premium" first
// and treating "not in that pool" as a hard failure is what stopped the band
// ever firing for a free-pool account (#195); membership is now read per
// account, which is exact because a mailbox is in exactly one pool (#211).
func TestEvaluateAnyPoolEvaluatesTheMailboxesOwnPool(t *testing.T) {
	repo := &ownPoolRepo{
		stubWarmupRepo: stubWarmupRepo{
			health: &models.WarmupParticipantHealth{
				PoolType:    "free",
				HealthState: models.WarmupHealthHealthy,
			},
		},
	}

	health, err := NewService(repo).(*service).evaluateAndPersistAnyPool(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if health == nil {
		t.Fatal("expected the free-pool participant")
	}
	if !repo.updated {
		t.Fatal("the evaluation never persisted a decision")
	}
	if repo.readBackPool != "free" {
		t.Fatalf("read the decision back from pool %q, want the pool the mailbox is actually in", repo.readBackPool)
	}
}

type ownPoolRepo struct {
	stubWarmupRepo
	updated      bool
	readBackPool string
}

func (r *ownPoolRepo) GetParticipantHealth(_ context.Context, _ uuid.UUID, poolType string) (*models.WarmupParticipantHealth, error) {
	r.readBackPool = poolType
	if poolType != "free" {
		return nil, nil // absent, not broken
	}
	return r.health, nil
}

func (r *ownPoolRepo) UpdateParticipantHealth(_ context.Context, _ uuid.UUID, _ models.WarmupHealthState, _ *time.Time, _ string, _ float64) error {
	r.updated = true
	return nil
}
