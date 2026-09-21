package payloadrelease

import (
	"testing"
	"time"
)

func TestTerminalStateMatricesOnlyReleaseFinalScopes(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		state     string
		retryable bool
		terminal  bool
	}{
		{name: "import published", state: "PUBLISHED", terminal: true},
		{name: "import discarded", state: "DISCARDED", terminal: true},
		{name: "import final failure", state: "FAILED_FINAL", terminal: true},
		{name: "import cancelled", state: "CANCELLED", terminal: true},
		{name: "import review pending", state: "REVIEW_PENDING"},
		{name: "import retryable failure", state: "FAILED_RETRYABLE", retryable: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := terminalImportItem(test.state); got != test.terminal {
				t.Fatalf("terminalImportItem(%q) = %v, want %v", test.state, got, test.terminal)
			}
		})
	}

	for _, test := range []struct {
		name      string
		state     string
		retryable bool
		terminal  bool
	}{
		{name: "Pegasus published through public item", state: "PUBLISHED", terminal: true},
		{name: "Pegasus review discarded", state: "REVIEW_DISCARDED", terminal: true},
		{name: "Pegasus duplicate", state: "SKIPPED_EXISTING", terminal: true},
		{name: "Pegasus mapping skipped", state: "SKIPPED_MAPPING", terminal: true},
		{name: "Pegasus cancelled", state: "CANCELLED", terminal: true},
		{name: "Pegasus final source failure", state: "SOURCE_CHANGED", terminal: true},
		{name: "Pegasus retryable source failure", state: "SOURCE_CHANGED", retryable: true},
		{name: "Pegasus final read failure", state: "READ_FAILED", terminal: true},
		{name: "Pegasus retryable read failure", state: "READ_FAILED", retryable: true},
		{name: "Pegasus review pending", state: "REVIEW_PENDING"},
		{name: "Pegasus running", state: "COPYING"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := terminalPegasusItem(test.state, test.retryable); got != test.terminal {
				t.Fatalf("terminalPegasusItem(%q, %v) = %v, want %v", test.state, test.retryable, got, test.terminal)
			}
		})
	}
}

func TestPayloadReleaseRetryScheduleIsBounded(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		attempt int64
		want    time.Duration
	}{
		{attempt: 1, want: time.Second},
		{attempt: 2, want: 5 * time.Second},
		{attempt: 3, want: 30 * time.Second},
		{attempt: 4, want: 30 * time.Second},
	} {
		if got := releaseRetryDelay(test.attempt); got != test.want {
			t.Fatalf("releaseRetryDelay(%d) = %s, want %s", test.attempt, got, test.want)
		}
	}
}
