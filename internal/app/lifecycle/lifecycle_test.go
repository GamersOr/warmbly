package lifecycle

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

func TestDecideRestsOnRealTrouble(t *testing.T) {
	now := time.Now()
	for _, h := range []models.WarmupHealthState{
		models.WarmupHealthThrottled, models.WarmupHealthQuarantined, models.WarmupHealthBlocked,
	} {
		d := Decide(models.SendLifecycleActive, nil, h, now)
		if d.Next != models.SendLifecycleResting {
			t.Errorf("health %q gave %q, want resting", h, d.Next)
		}
		if d.Reason == "" {
			t.Errorf("health %q rested with no reason", h)
		}
	}
}

// Watch is the band defined to change nothing a customer can feel. Leaving
// cold rotation is very much something they feel.
func TestDecideDoesNotRestOnWatch(t *testing.T) {
	if d := Decide(models.SendLifecycleActive, nil, models.WarmupHealthWatch, time.Now()); d.Next != models.SendLifecycleActive {
		t.Errorf("watch gave %q, want active", d.Next)
	}
}

func TestDecideResumesOnlyAfterProbation(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	served := now.Add(-models.RestProbation)
	if d := Decide(models.SendLifecycleResting, &served, models.WarmupHealthHealthy, now); d.Next != models.SendLifecycleActive {
		t.Errorf("a recovered mailbox that served its rest gave %q, want active", d.Next)
	}

	fresh := now.Add(-time.Hour)
	if d := Decide(models.SendLifecycleResting, &fresh, models.WarmupHealthHealthy, now); d.Next != models.SendLifecycleResting {
		t.Errorf("an hour of rest gave %q, want it still resting", d.Next)
	}

	if d := Decide(models.SendLifecycleResting, &served, models.WarmupHealthWatch, now); d.Next != models.SendLifecycleResting {
		t.Errorf("a still-degraded mailbox gave %q, want resting", d.Next)
	}
}

// Reserve is the owner's decision, in both directions.
func TestDecideNeverTouchesReserveOrWarming(t *testing.T) {
	now := time.Now()
	for _, h := range []models.WarmupHealthState{
		models.WarmupHealthHealthy, models.WarmupHealthThrottled, models.WarmupHealthBlocked,
	} {
		if d := Decide(models.SendLifecycleReserve, nil, h, now); d.Next != models.SendLifecycleReserve {
			t.Errorf("health %q moved a reserved mailbox to %q", h, d.Next)
		}
		if d := Decide(models.SendLifecycleWarming, nil, h, now); d.Next != models.SendLifecycleWarming {
			t.Errorf("health %q moved a warming mailbox to %q", h, d.Next)
		}
	}
}

// A row written before the column existed reads as empty and must be treated
// as active rather than left in limbo.
func TestDecideTreatsAnUnsetStateAsActive(t *testing.T) {
	if d := Decide("", nil, models.WarmupHealthHealthy, time.Now()); d.Next != models.SendLifecycleActive {
		t.Errorf("an unset lifecycle gave %q, want active", d.Next)
	}
}
