package emulationstationimport

import "math"

// PlanCompletion determines the terminal state from completion counts.
func PlanCompletion(before LeaseSnapshot, counts CompletionCounts, now int64) (CompletionChange, error) {
	if counts.Unfinished > 0 {
		return CompletionChange{}, ErrActive
	}
	if !validCompletionCounts(counts) {
		return CompletionChange{}, ErrInvalid
	}
	state := "COMPLETED"
	if counts.Terminal.Failed > 0 || counts.Terminal.Blocked > 0 {
		state = "PARTIAL_FAILURE"
	}
	return CompletionChange{
		Before:      before,
		Counts:      counts,
		ImportState: state,
		Retryable:   counts.RetryableFailed > 0,
		NowMS:       now,
	}, nil
}

func validCompletionCounts(counts CompletionCounts) bool {
	if counts.ExpectedItems < 0 ||
		counts.Unfinished != 0 ||
		counts.RetryableFailed < 0 ||
		counts.RetryableFailed > counts.Terminal.Failed ||
		counts.MediaWarnings < 0 {
		return false
	}
	terminal := counts.Terminal
	sum := int64(0)
	for _, count := range []int64{
		terminal.SkippedMapping,
		terminal.ReviewPending,
		terminal.Published,
		terminal.ReviewDiscarded,
		terminal.Existing,
		terminal.Blocked,
		terminal.Failed,
		terminal.Cancelled,
	} {
		if count < 0 || sum > math.MaxInt64-count {
			return false
		}
		sum += count
	}
	return sum == counts.ExpectedItems
}
