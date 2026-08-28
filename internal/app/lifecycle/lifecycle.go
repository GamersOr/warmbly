// Package lifecycle decides when a cold mailbox rests and when it may return.
// Resting removes it from cold rotation while warmup keeps its reputation
// alive; it returns only after a clean probation.
package lifecycle

import (
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

// Decision is what the rebalancer should do with one mailbox.
type Decision struct {
	// Next is the state to move to, equal to the current one when nothing
	// changes.
	Next   models.SendLifecycle
	Reason string
	// RestartProbation asks the caller to re-stamp the clock without changing
	// state. Probation has to measure HEALTHY time: a mailbox that sat resting
	// and unhealthy for three days would otherwise resume on its first healthy
	// tick, having served no clean time at all.
	RestartProbation bool
}

// Changed reports whether the mailbox should move.
func (d Decision) Changed(current models.SendLifecycle) bool { return d.Next != current }

// Decide maps warmup health onto the cold lifecycle. Rests at throttled and
// worse, never at watch: watch is defined to change nothing a customer feels.
func Decide(current models.SendLifecycle, since *time.Time, health models.WarmupHealthState, now time.Time) Decision {
	if !current.AutoManaged() {
		return Decision{Next: current}
	}

	switch health {
	case models.WarmupHealthThrottled:
		return Decision{Next: models.SendLifecycleResting,
			Reason: "warmup health is throttled; resting on warmup traffic to recover"}
	case models.WarmupHealthQuarantined, models.WarmupHealthBlocked:
		return Decision{Next: models.SendLifecycleResting,
			Reason: "warmup health is " + string(health) + "; out of cold rotation until it recovers"}
	}

	// Healthy or watch. A resting mailbox returns only after a probation, so
	// one good hour cannot bounce it straight back to full cold volume.
	if current == models.SendLifecycleResting {
		if health != models.WarmupHealthHealthy {
			// Still not healthy: the clean streak starts again from here.
			return Decision{Next: current, RestartProbation: true}
		}
		state := models.SendLifecycleState{State: current, Since: since}
		if state.ReadyToResume(now) {
			return Decision{Next: models.SendLifecycleActive, Reason: "recovered and served its rest"}
		}
		return Decision{Next: current}
	}
	return Decision{Next: models.SendLifecycleActive}
}
