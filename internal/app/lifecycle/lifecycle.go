// Package lifecycle decides when a cold mailbox should rest and when it may
// come back.
//
// Cold sending ran at full volume until a hard health band tripped, so a
// mailbox showing early fatigue either kept going or was quarantined, with
// nothing in between. Resting pulls it out of cold rotation while warmup keeps
// its reputation alive, and returns it after a clean probation.
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
}

// Changed reports whether the mailbox should move.
func (d Decision) Changed(current models.SendLifecycle) bool { return d.Next != current }

// Decide maps a mailbox's warmup health onto its cold lifecycle.
//
// Rests on throttled and worse, never on watch: watch is the band defined to
// change nothing a customer can feel, and leaving cold rotation is very much
// something they feel.
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
		state := models.SendLifecycleState{State: current, Since: since}
		if health == models.WarmupHealthHealthy && state.ReadyToResume(now) {
			return Decision{Next: models.SendLifecycleActive, Reason: "recovered and served its rest"}
		}
		return Decision{Next: current}
	}
	return Decision{Next: models.SendLifecycleActive}
}
