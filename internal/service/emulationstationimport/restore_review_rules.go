package emulationstationimport

import library "retrom/internal/service/libraryimport"

// ReviewPreparation returns the domain transitions required to complete a reserved review.
// The caller supplies its own transactional ownership check before applying these transitions.
func ReviewPreparation(review ExecutionReview) ([]string, error) {
	return reviewPreparation(review)
}

// AppendReviewMetadataWarnings preserves canonical warning bounds and cumulative omission counts.
func AppendReviewMetadataWarnings(encoded string, additions []library.ServerMetadataWarning) (string, error) {
	return appendExecutionWarnings(encoded, additions)
}
