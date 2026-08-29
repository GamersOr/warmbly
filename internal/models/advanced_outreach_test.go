package models

import "testing"

// The content-score floor reaches the API as a plain integer. Left unclamped, a
// floor above 100 flags every campaign forever and a floor at or below 0 is a
// control that does nothing, since the readers fall back to the default.
func TestNormalizeClampsTheContentScoreFloor(t *testing.T) {
	for _, tc := range []struct{ in, want int }{
		{-40, 1},
		{0, 1},
		{1, 1},
		{60, 60},
		{100, 100},
		{5000, 100},
	} {
		s := DefaultAdvancedOutreachSettings()
		s.Preflight.MinContentScore = tc.in
		s.Normalize()
		if s.Preflight.MinContentScore != tc.want {
			t.Errorf("floor %d normalized to %d, want %d", tc.in, s.Preflight.MinContentScore, tc.want)
		}
	}
}

func TestNormalizeLeavesTheDefaultsAlone(t *testing.T) {
	s := DefaultAdvancedOutreachSettings()
	before := s
	s.Normalize()
	if s.Preflight != before.Preflight {
		t.Errorf("defaults changed under Normalize: %+v -> %+v", before.Preflight, s.Preflight)
	}
}
