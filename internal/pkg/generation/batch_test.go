package generation

import (
	"testing"

	"github.com/openai/openai-go/v2"
)

// A batch that fails as a whole produces no error file, so the message the
// provider puts on the batch itself is the only account of why. Losing it is
// how "batch failed" ended up being everything an operator was told.
func TestBatchFailureReason(t *testing.T) {
	cases := []struct {
		name string
		errs []openai.BatchError
		want string
	}{
		{"none", nil, ""},
		{
			"single",
			[]openai.BatchError{{Code: "invalid_request", Message: "Cannot find file file-abc, or organization org-xyz does not have access to it."}},
			"Cannot find file file-abc, or organization org-xyz does not have access to it.",
		},
		{
			"counts the rest",
			[]openai.BatchError{
				{Message: "first reason"},
				{Message: "second reason"},
				{Message: "third reason"},
			},
			"first reason (+2 more)",
		},
		{
			"skips a blank message",
			[]openai.BatchError{{Code: "empty"}, {Message: "  the real one  "}},
			"the real one (+1 more)",
		},
		{"all blank", []openai.BatchError{{Code: "a"}, {Code: "b"}}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := batchFailureReason(tc.errs); got != tc.want {
				t.Errorf("batchFailureReason() = %q, want %q", got, tc.want)
			}
		})
	}
}
