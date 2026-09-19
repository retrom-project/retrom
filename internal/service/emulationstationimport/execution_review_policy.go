package emulationstationimport

import model "retrom/internal/model/emulationstationimport"

func reviewPreparation(review model.ExecutionReview) ([]string, error) {
	return model.ReviewPreparation(review)
}
