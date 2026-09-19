package emulationstationimport

// ReviewPreparation returns the allowed state transitions for a review handoff.
func ReviewPreparation(review ExecutionReview) ([]string, error) {
	switch review.State {
	case "PENDING":
		return []string{"COPYING", "VALIDATING"}, nil
	case "COPYING":
		return []string{"VALIDATING"}, nil
	case "VALIDATING":
		return nil, nil
	case "COMMIT_FAILED", "SOURCE_CHANGED", "READ_FAILED":
		if review.Retryable {
			return []string{"PENDING", "COPYING", "VALIDATING"}, nil
		}
	}
	return nil, ErrInvalid
}

// MatchesReviewHandoff checks whether a review matches the handoff request.
func MatchesReviewHandoff(review ExecutionReview, request ReviewHandoffRequest) bool {
	if review.Version < 1 || review.ItemID != request.ItemID ||
		request.LibraryJobID == "" || request.LibraryItemID == "" ||
		review.ReservedJobID != request.LibraryJobID || review.ReservedItemID != request.LibraryItemID {
		return false
	}
	if review.LibraryJobID == "" && review.LibraryItemID == "" {
		return review.State == "COPYING"
	}
	return review.LibraryJobID == request.LibraryJobID && review.LibraryItemID == request.LibraryItemID
}
