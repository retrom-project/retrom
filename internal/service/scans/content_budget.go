package scans

import (
	"context"

	"retrom/internal/model"
	"retrom/internal/storage"
)

type contentBudgetKey struct{}

type contentBudget struct {
	files int
	bytes int64
}

func contentReadLimit(ctx context.Context) (int64, error) {
	budget, _ := ctx.Value(contentBudgetKey{}).(*contentBudget)
	if budget == nil {
		return storage.MaximumFileSize, nil
	}
	if budget.files >= maximumGameFiles {
		return 0, model.ErrInvalid
	}
	return min(storage.MaximumFileSize, maximumContentSize-budget.bytes), nil
}

func recordContent(ctx context.Context, size int64) {
	budget, _ := ctx.Value(contentBudgetKey{}).(*contentBudget)
	if budget != nil {
		budget.files++
		budget.bytes += size
	}
}
