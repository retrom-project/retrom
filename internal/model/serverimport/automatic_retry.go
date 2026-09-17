package serverimport

import "time"

// AutomaticRetryAt keeps every automatic attempt within the original execution
// budget. It is a pure function used by the repo layer.
func AutomaticRetryAt(
	attempt, maximum, terminalItems, deadline, now int64,
) (int64, bool) {
	if terminalItems != 0 || attempt >= maximum || deadline <= now {
		return 0, false
	}
	delays := []time.Duration{
		time.Second,
		5 * time.Second,
		30 * time.Second,
		120 * time.Second,
	}
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= int64(len(delays)) {
		index = int64(len(delays)) - 1
	}
	available := now + delays[index].Milliseconds()
	if available >= deadline {
		return 0, false
	}
	return available, true
}
