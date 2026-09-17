package emulationstationimport

import (
	model "retrom/internal/model/emulationstationimport"
	library "retrom/internal/model/libraryimport"
)

// ReviewPreparation returns the domain transitions required to complete a reserved review.
// The caller supplies its own transactional ownership check before applying these transitions.
func ReviewPreparation(review model.ExecutionReview) ([]string, error) {
	return reviewPreparation(review)
}

// AppendReviewMetadataWarnings preserves canonical warning bounds and cumulative omission counts.
func AppendReviewMetadataWarnings(encoded string, additions []library.ServerMetadataWarning) (string, error) {
	return appendExecutionWarnings(encoded, additions)
}
