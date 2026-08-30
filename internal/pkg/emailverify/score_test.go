package emailverify

import (
	"testing"
	"time"
)

func TestScoreSilenceIsNotEvidence(t *testing.T) {
	now := time.Now()
	probeValid := Verdict{Status: StatusValid, Source: "probe", CheckedAt: now.Add(-24 * time.Hour)}
	// A contact sent to five times with nothing recorded: nothing changes.
	got := Score(probeValid, nil, now)
	if got.Status != StatusValid || got.Confidence != 75 {
		t.Fatalf("silence changed the verdict: %+v", got)
	}
}

func TestScoreRealMailBeatsTheProbe(t *testing.T) {
	now := time.Now()
	probeInvalid := Verdict{Status: StatusInvalid, Source: "probe", CheckedAt: now.Add(-time.Hour)}
	got := Score(probeInvalid, []Evidence{{Kind: EvidenceReplied, ObservedAt: now.Add(-3 * 24 * time.Hour)}}, now)
	if got.Status != StatusValid || !got.Decisive {
		t.Fatalf("a reply did not override the probe: %+v", got)
	}
	// Two clean deliveries are enough; one is not.
	one := Score(probeInvalid, []Evidence{{Kind: EvidenceDelivered, ObservedAt: now}}, now)
	if one.Status != StatusInvalid {
		t.Fatalf("one delivery overrode the probe: %+v", one)
	}
	two := Score(probeInvalid, []Evidence{{Kind: EvidenceDelivered, ObservedAt: now, Detail: "a"}, {Kind: EvidenceDelivered, ObservedAt: now.Add(-time.Hour), Detail: "b"}}, now)
	if two.Status != StatusValid {
		t.Fatalf("two deliveries did not override the probe: %+v", two)
	}
}

func TestScoreNewerBounceWinsOverOlderEngagement(t *testing.T) {
	now := time.Now()
	v := Verdict{Status: StatusValid, Source: "provider"}
	got := Score(v, []Evidence{
		{Kind: EvidenceReplied, ObservedAt: now.Add(-60 * 24 * time.Hour)},
		{Kind: EvidenceBouncedRecipient, ObservedAt: now.Add(-24 * time.Hour), Detail: "550 5.1.1 user unknown"},
	}, now)
	if got.Status != StatusInvalid || !got.Decisive {
		t.Fatalf("a newer bounce did not win: %+v", got)
	}
	// And a reply after the bounce (the mailbox came back) wins again.
	back := Score(v, []Evidence{
		{Kind: EvidenceBouncedRecipient, ObservedAt: now.Add(-60 * 24 * time.Hour)},
		{Kind: EvidenceReplied, ObservedAt: now.Add(-24 * time.Hour)},
	}, now)
	if back.Status != StatusValid {
		t.Fatalf("a reply after a bounce did not win: %+v", back)
	}
}

func TestScoreManualWinsOutright(t *testing.T) {
	now := time.Now()
	got := Score(Verdict{Status: StatusValid, Source: "manual"}, []Evidence{{Kind: EvidenceBouncedRecipient, ObservedAt: now}}, now)
	if got.Status != StatusValid {
		t.Fatalf("manual lost to evidence: %+v", got)
	}
}

func TestScoreEvidenceDecays(t *testing.T) {
	now := time.Now()
	v := Verdict{Status: StatusUnknown, Source: "probe"}
	fresh := Score(v, []Evidence{{Kind: EvidenceOpened, ObservedAt: now}}, now)
	old := Score(v, []Evidence{{Kind: EvidenceOpened, ObservedAt: now.Add(-3 * 365 * 24 * time.Hour)}}, now)
	if fresh.Status != StatusValid {
		t.Fatalf("a fresh human open should decide: %+v", fresh)
	}
	if old.Status != StatusUnknown || old.Confidence >= fresh.Confidence {
		t.Fatalf("a three-year-old open should only nudge: %+v", old)
	}
}

func TestNamesRecipient(t *testing.T) {
	yes := []string{"550 5.1.1 The email account that you tried to reach does not exist", "User unknown", "smtp; 550 5.1.1 <a@b>: Recipient address rejected: User unknown in virtual mailbox table"}
	no := []string{"552 5.2.2 Mailbox full", "550 5.7.1 Service unavailable, Client host blocked using Spamhaus", "451 4.7.1 Greylisted, try again later", "554 5.7.1 Relay access denied"}
	for _, s := range yes {
		if !NamesRecipient(s) {
			t.Fatalf("should name recipient: %q", s)
		}
	}
	for _, s := range no {
		if NamesRecipient(s) {
			t.Fatalf("must not name recipient: %q", s)
		}
	}
}
