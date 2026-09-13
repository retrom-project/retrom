package libraryimport

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type ImportBatchCancellations struct {
	repository ImportBatchCancellationRepository
	now        func() time.Time
}

func NewImportBatchCancellations(
	repository ImportBatchCancellationRepository, now func() time.Time,
) *ImportBatchCancellations {
	if now == nil {
		now = time.Now
	}
	return &ImportBatchCancellations{repository: repository, now: now}
}

func (service *ImportBatchCancellations) Cancel(
	ctx context.Context, request ImportBatchCancellationRequest,
) (ImportBatchCancellationResult, error) {
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ImportID == "" || request.ExpectedVersion < 1 || request.Reason == "" ||
		utf8.RuneCountInString(request.Reason) > 500 || !validField(request.Reason, 500, true) {
		return ImportBatchCancellationResult{}, ErrInvalid
	}
	result, err := service.repository.Cancel(ctx, request, service.now().UnixMilli())
	if err != nil {
		return ImportBatchCancellationResult{}, fmt.Errorf("cancel import batch: %w", err)
	}
	return result, nil
}
