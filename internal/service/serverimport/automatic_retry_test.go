package serverimport

import "testing"

func TestAutomaticRetryStaysWithinExecutionBudget(t *testing.T) {
	for _, test := range []struct{ attempt, maximum, terminal, deadline, want int64 }{
		{1, 4, 0, 100000, 1100},
		{2, 4, 0, 100000, 5100},
		{3, 4, 0, 100000, 30100},
		{4, 4, 0, 100000, 0},
		{1, 4, 1, 100000, 0},
		{1, 4, 0, 1100, 0},
		{1, 4, 0, 0, 0},
	} {
		at, retry := AutomaticRetryAt(test.attempt, test.maximum, test.terminal, test.deadline, 100)
		if at != test.want || retry != (test.want != 0) {
			t.Fatalf("retry budget %+v: %d/%v", test, at, retry)
		}
	}
}
