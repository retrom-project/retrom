package libraryimport

import dbapi "retrom/internal/database"

type ReviewInputs struct{ executor dbapi.Executor }

func BindReviewInputs(executor dbapi.Executor) *ReviewInputs {
	return &ReviewInputs{executor: executor}
}
