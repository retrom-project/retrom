package pegasusimport

import (
	library "retrom/internal/model/libraryimport"
	model "retrom/internal/model/pegasusimport"
)

// MergeReviewMetadataWarnings shares the handoff's duplicate suppression with
// offline restore. It does not grant authority to modify a source or review.
func MergeReviewMetadataWarnings(
	existing []map[string]any, additions []library.ServerMetadataWarning,
) []map[string]any {
	return model.MergeReviewMetadataWarnings(existing, additions)
}
