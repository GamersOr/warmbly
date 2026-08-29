package advanced

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func emailStep(pos int, subject, body string) models.Sequence {
	return models.Sequence{
		Kind:      "email",
		Position:  pos,
		Subject:   subject,
		BodyHTML:  "<p>" + body + "</p>",
		BodyPlain: body,
	}
}

// A wait or action node has no subject and no body. Scoring it as copy made
// every campaign that used one fail preflight with "scores 55/100 for spam
// signals" about a step that was never an email.
func TestWorstStepContentScoreSkipsNonEmailSteps(t *testing.T) {
	good := strings.Repeat("A real sentence about the recipient's work. ", 5)
	seqs := []models.Sequence{
		emailStep(0, "Quick question about hiring", good),
		{Kind: "wait", Position: 1},
		{Kind: "action", Position: 2},
		emailStep(3, "Following up on my note", good),
	}

	worst, _, _, scored := worstStepContentScore(seqs, 0)
	if scored != 2 {
		t.Errorf("scored %d steps, want the 2 email steps", scored)
	}
	if worst != 100 {
		t.Errorf("clean campaign scored %d, want 100", worst)
	}
}

// A campaign of nothing but control nodes has no copy to judge, which must read
// as "nothing to score" rather than as a perfect or a failing score.
func TestWorstStepContentScoreReportsNothingToScore(t *testing.T) {
	_, _, _, scored := worstStepContentScore([]models.Sequence{
		{Kind: "wait", Position: 0},
		{Kind: "action", Position: 1},
	}, 0)
	if scored != 0 {
		t.Errorf("scored %d steps, want 0", scored)
	}
}

// The reported step number must be the step's position, so preflight and the
// per-send warning name the same step.
func TestWorstStepContentScoreReportsThePositionOfTheWorstStep(t *testing.T) {
	good := strings.Repeat("A real sentence about the recipient's work. ", 5)
	seqs := []models.Sequence{
		emailStep(0, "Quick question about hiring", good),
		{Kind: "wait", Position: 1},
		emailStep(2, "FREE CASH PRIZE GUARANTEED!!!", "Act now, click here, 100% free, risk free."),
	}

	worst, step, issue, scored := worstStepContentScore(seqs, 0)
	if scored != 2 {
		t.Fatalf("scored %d steps, want 2", scored)
	}
	if step != 3 {
		t.Errorf("worst step reported as %d, want 3 (position 2)", step)
	}
	if worst >= 60 {
		t.Errorf("obviously spammy copy scored %d, want it below the default floor", worst)
	}
	if issue == "" {
		t.Error("no leading issue reported for the worst step")
	}
}

// An empty step list falls out with nothing scored: the caller reports that
// rather than treating it as passing content.
func TestWorstStepContentScoreOnEmptyCampaign(t *testing.T) {
	if _, _, _, scored := worstStepContentScore(nil, 0); scored != 0 {
		t.Errorf("scored %d steps on an empty campaign, want 0", scored)
	}
}

// Attachments are campaign-wide, so preflight weighs them the way the send path
// does instead of reporting a score the activity feed later contradicts.
func TestWorstStepContentScoreCountsAttachments(t *testing.T) {
	good := strings.Repeat("A real sentence about the recipient's work. ", 5)
	seqs := []models.Sequence{emailStep(0, "Quick question about hiring", good)}

	clean, _, _, _ := worstStepContentScore(seqs, 0)
	withAtt, _, _, _ := worstStepContentScore(seqs, 2)
	if withAtt >= clean {
		t.Errorf("attachment score %d not below the clean %d", withAtt, clean)
	}
}
