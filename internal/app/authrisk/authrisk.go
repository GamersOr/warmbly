// Package authrisk compares a sign-in against where the account was last used.
//
// A hijacked workspace owns warmed mailboxes and decryptable credentials, so it
// is worth more to an attacker than an ordinary SaaS account. The existing
// device fingerprint already challenges an unfamiliar browser; this adds the
// question that fingerprint cannot answer, which is whether the sign-in could
// physically have come from the same person.
package authrisk

import (
	"fmt"
	"math"
	"time"
)

// Place is one observed sign-in location.
type Place struct {
	CountryCode string
	Latitude    float64
	Longitude   float64
	At          time.Time
}

// HasPosition reports whether the coordinates are usable. A zero pair is both
// "unknown" and a real point at sea, so it is treated as unknown.
func (p Place) HasPosition() bool { return p.Latitude != 0 || p.Longitude != 0 }

// Verdict is what the comparison concluded.
type Verdict struct {
	// Flagged asks the caller for a step-up challenge.
	Flagged bool
	Reason  string
	// ImpliedKmh is the speed the journey would have required, 0 when it could
	// not be computed.
	ImpliedKmh float64
}

const (
	// MaxPlausibleKmh is faster than a commercial flight including transfers.
	// Above it the two sign-ins cannot be the same person travelling.
	MaxPlausibleKmh = 1000
	// MinDistanceKm is the distance below which speed is not worth computing.
	// City centroids are coarse, and two sign-ins from opposite sides of one
	// metro area would otherwise imply an absurd speed over a tiny gap.
	MinDistanceKm = 500
	// MinElapsed guards against dividing by an interval so short that any
	// distance looks impossible. Two sign-ins seconds apart from different
	// cities are far more likely to be a proxy than a journey.
	MinElapsed = 2 * time.Minute
)

// Assess compares a new sign-in against the most recent previous one.
//
// It answers only what the data supports. With no history, no coordinates, or
// an interval too short to reason about, it declines to flag: a false step-up
// locks a real user out of their own account, which is a worse outcome than a
// missed signal on one sign-in.
func Assess(prev *Place, current Place) Verdict {
	if prev == nil {
		// First recorded sign-in. There is nothing to compare against, and
		// challenging it would challenge every new user.
		return Verdict{}
	}

	elapsed := current.At.Sub(prev.At)
	if elapsed < 0 {
		// Clock skew between instances or a backdated row. Not a signal.
		return Verdict{}
	}

	if prev.HasPosition() && current.HasPosition() && elapsed >= MinElapsed {
		km := haversineKm(prev.Latitude, prev.Longitude, current.Latitude, current.Longitude)
		if km >= MinDistanceKm {
			kmh := km / elapsed.Hours()
			if kmh > MaxPlausibleKmh {
				return Verdict{
					Flagged:    true,
					ImpliedKmh: kmh,
					Reason: fmt.Sprintf(
						"sign-in %.0f km from the previous one %s earlier, which would need %.0f km/h",
						km, elapsed.Round(time.Minute), kmh),
				}
			}
		}
	}

	// A country change is a weaker signal, used only when coordinates are
	// missing so it does not double-count a journey already judged plausible.
	if !prev.HasPosition() || !current.HasPosition() {
		if prev.CountryCode != "" && current.CountryCode != "" &&
			prev.CountryCode != current.CountryCode && elapsed < time.Hour {
			return Verdict{
				Flagged: true,
				Reason: fmt.Sprintf("sign-in from %s within %s of one from %s",
					current.CountryCode, elapsed.Round(time.Minute), prev.CountryCode),
			}
		}
	}

	return Verdict{}
}

// haversineKm is the great-circle distance between two points.
func haversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371
	dLat := radians(lat2 - lat1)
	dLon := radians(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(radians(lat1))*math.Cos(radians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func radians(deg float64) float64 { return deg * math.Pi / 180 }
