package libraryimport

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	model "retrom/internal/model/libraryimport"
)

type ImportBatchCancellations struct {
	repository model.ImportBatchCancellationRepository
	now        func() time.Time
}

func NewImportBatchCancellations(
	repository model.ImportBatchCancellationRepository, now func() time.Time,
) *ImportBatchCancellations {
	if now == nil {
		now = time.Now
	}
	return &ImportBatchCancellations{repository: repository, now: now}
}

func (service *ImportBatchCancellations) Cancel(
	ctx context.Context, request model.ImportBatchCancellationRequest,
) (model.ImportBatchCancellationResult, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ImportID == "" || request.ExpectedVersion < 1 || request.Reason == "" ||
		utf8.RuneCountInString(request.Reason) > 500 || !validField(request.Reason, 500, true) {
		return model.ImportBatchCancellationResult{}, model.ErrInvalid
	}
	result, err := service.repository.Cancel(ctx, request, service.now().UnixMilli())
	if err != nil {
		return model.ImportBatchCancellationResult{}, fmt.Errorf("cancel import batch: %w", err)
	}
	return result, nil
}
