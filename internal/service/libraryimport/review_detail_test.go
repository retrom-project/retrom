package libraryimport

import (
	"context"
	"errors"
	"reflect"
	"testing"

	model "retrom/internal/model/libraryimport"
)

type detailRepositoryFixture struct {
	result model.ReviewDetail
	err    error
}

func (fixture detailRepositoryFixture) LoadReviewDetail(_ context.Context, _ string) (model.ReviewDetail, error) {
	return fixture.result, fixture.err
}

func TestReviewDetailChecksVisibilityBeforeChildren(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{model.ErrReviewNotFound, errors.New("query headline failed")} {
		service := NewReviewDetails(detailRepositoryFixture{err: cause})
		result, err := service.Get(t.Context(), "hidden")
		if !errors.Is(err, cause) || !reflect.DeepEqual(result, model.ReviewDetail{}) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}
