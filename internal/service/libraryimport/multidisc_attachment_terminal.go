package libraryimport

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type MultiDiscAttachmentTerminals struct {
	repository MultiDiscAttachmentTerminalRepository
	now        func() time.Time
	newID      func() (string, error)
}

func NewMultiDiscAttachmentTerminals(
	repository MultiDiscAttachmentTerminalRepository, now func() time.Time,
) *MultiDiscAttachmentTerminals {
	if now == nil {
		now = time.Now
	}
	return &MultiDiscAttachmentTerminals{repository: repository, now: now, newID: newMultiDiscAttachmentTerminalID}
}

func newMultiDiscAttachmentTerminalID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("allocate multi-disc attachment terminal event ID: %w", err)
	}
	return id.String(), nil
}

func (service *MultiDiscAttachmentTerminals) Reject(
	ctx context.Context, request MultiDiscAttachmentRejectRequest,
) error {
	if !validMultiDiscAttachmentTarget(request.Target) || request.Code == "" {
		return ErrInvalid
	}
	eventID, err := service.newID()
	if err != nil {
		return err
	}
	now := service.now().UnixMilli()
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "errorCode": request.Code, "causeCode": request.Cause,
		"durationMs": multiDiscAttachmentDurationMS(request.Target.ExecutionStartedAtMS, now),
	})
	evidence := multiDiscReviewEventJSON(map[string]any{
		"attachmentKind": "MULTI_DISC", "state": "REJECTED", "errorCode": request.Code,
	})
	if err := service.repository.Reject(ctx, MultiDiscAttachmentRejectWrite{
		Target: request.Target, Actor: request.Actor, Code: request.Code,
		DiagnosticsJSON: string(diagnostics), EvidenceJSON: evidence, EventID: eventID, NowMS: now,
	}); err != nil {
		return fmt.Errorf("reject multi-disc attachment: %w", err)
	}
	return nil
}

func (service *MultiDiscAttachmentTerminals) Retry(
	ctx context.Context, request MultiDiscAttachmentRetryRequest,
) (MultiDiscAttachmentRetryResult, error) {
	if !validMultiDiscAttachmentTarget(request.Target) || request.Code == "" {
		return MultiDiscAttachmentRetryResult{}, ErrInvalid
	}
	now := service.now().UnixMilli()
	diagnostics, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "errorCode": request.Code,
		"durationMs": multiDiscAttachmentDurationMS(request.Target.ExecutionStartedAtMS, now),
	})
	event, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "attempt": 0,
		"durationMs": multiDiscAttachmentDurationMS(request.Target.ExecutionStartedAtMS, now),
		"errorCode":  request.Code,
	})
	write := MultiDiscAttachmentRetryWrite{
		Target: request.Target, Code: request.Code,
		DiagnosticsJSON: string(diagnostics), EventJSON: string(event), NowMS: now,
	}
	result, retryErr := service.repository.TryRetry(ctx, write)
	if retryErr == nil && result.Scheduled {
		return result, nil
	}
	if err := service.repository.FailRetryable(ctx, write); err != nil {
		return MultiDiscAttachmentRetryResult{}, fmt.Errorf("finish retryable multi-disc attachment: %w", err)
	}
	return MultiDiscAttachmentRetryResult{}, nil
}

func (service *MultiDiscAttachmentTerminals) SyncCancellation(
	ctx context.Context, jobID string,
) error {
	if jobID == "" {
		return ErrInvalid
	}
	if err := service.repository.SyncCancellation(ctx, jobID, service.now().UnixMilli()); err != nil {
		return fmt.Errorf("sync multi-disc attachment cancellation: %w", err)
	}
	return nil
}

func (service *MultiDiscAttachmentTerminals) FinishCancellation(
	ctx context.Context, request MultiDiscAttachmentCancellationRequest,
) (bool, error) {
	if !validMultiDiscAttachmentTarget(request.Target) {
		return false, ErrInvalid
	}
	now := service.now().UnixMilli()
	event, _ := json.Marshal(map[string]any{
		"schemaVersion": 1, "state": "CANCELLED",
		"durationMs": multiDiscAttachmentDurationMS(request.Target.ExecutionStartedAtMS, now),
	})
	result, err := service.repository.FinishCancellation(ctx, MultiDiscAttachmentCancellationWrite{
		Target: request.Target, NowMS: now, EventJSON: string(event),
	})
	if err != nil {
		return false, fmt.Errorf("finish multi-disc attachment cancellation: %w", err)
	}
	return result, nil
}

func validMultiDiscAttachmentTarget(target MultiDiscAttachmentTerminalTarget) bool {
	return target.AttachmentID != "" && target.ItemID != "" && target.JobID != "" && target.WorkerID != ""
}

func multiDiscAttachmentDurationMS(startedAtMS, nowMS int64) int64 {
	if startedAtMS <= 0 || nowMS <= startedAtMS {
		return 0
	}
	return nowMS - startedAtMS
}
