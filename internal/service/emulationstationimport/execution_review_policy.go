package emulationstationimport

import model "retrom/internal/model/emulationstationimport"

func reviewPreparation(review model.ExecutionReview) ([]string, error) {
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
	return nil, model.ErrInvalid
}
