package libraryimport

import (
	"context"
	"fmt"

	model "retrom/internal/model/libraryimport"
)

type ReviewDetails struct{ repository model.ReviewDetailRepository }

func NewReviewDetails(repository model.ReviewDetailRepository) *ReviewDetails {
	return &ReviewDetails{repository: repository}
}

func (service *ReviewDetails) Get(ctx context.Context, itemID string) (model.ReviewDetail, error) {
	result, err := service.repository.LoadReviewDetail(ctx, itemID)
	if err != nil {
		return model.ReviewDetail{}, fmt.Errorf("read review detail: %w", err)
	}
	return result, nil
}
