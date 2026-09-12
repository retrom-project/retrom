package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"retrom/internal/emulationstationmeta"
	persistence "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) scanPublication() *application.ScanPublication {
	return application.NewScanPublication(persistence.NewScanPublication(service.database), service.now)
}

func (service *Service) persistScan(ctx context.Context, unit work, value scanResult) error {
	if err := service.scanPublication().Publish(ctx, unit, application.ScanProjection(value)); err != nil {
		return fmt.Errorf("persist EmulationStation scan: %w", err)
	}
	return nil
}

func (service *Service) persistRejectedScan(ctx context.Context, unit work, value scanResult) error {
	if err := service.scanPublication().Rejected(ctx, unit, application.ScanProjection(value)); err != nil {
		return fmt.Errorf("persist rejected EmulationStation scan: %w", err)
	}
	return nil
}

func (service *Service) persistScanHeaders(ctx context.Context, unit work, value scanResult) error {
	if err := service.scanPublication().Headers(ctx, unit, application.ScanProjection(value)); err != nil {
		return fmt.Errorf("persist EmulationStation scan headers: %w", err)
	}
	return nil
}

func (service *Service) persistScanItems(ctx context.Context, unit work, items []scannedItem) error {
	if err := service.scanPublication().Items(ctx, unit, items); err != nil {
		return fmt.Errorf("persist EmulationStation scan items: %w", err)
	}
	return nil
}

func (service *Service) finishScan(ctx context.Context, unit work, value scanResult) error {
	if err := service.scanPublication().Finish(ctx, unit, application.ScanProjection(value)); err != nil {
		return fmt.Errorf("finish EmulationStation scan: %w", err)
	}
	return nil
}

func (service *Service) clearScanStaging(ctx context.Context, unit work) error {
	if err := service.scanPublication().Reset(ctx, unit); err != nil {
		return fmt.Errorf("clear EmulationStation scan staging: %w", err)
	}
	return nil
}

func (service *Service) fail(ctx context.Context, unit work, code string, retryable bool) {
	now := service.now().UnixMilli()
	deadlineExpired := errors.Is(ctx.Err(), context.DeadlineExceeded) ||
		(unit.DeadlineAtMS > 0 && unit.DeadlineAtMS <= now)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if deadlineExpired {
		code = "EMULATIONSTATION_EXECUTION_TIMEOUT"
		retryable = false
	}
	if retryable {
		outcome, err := service.scheduleAutomaticRetry(ctx, unit, code, now)
		if err == nil {
			switch outcome {
			case retryScheduled:
				return
			case retryDeadlineExhausted:
				code = "EMULATIONSTATION_EXECUTION_TIMEOUT"
				retryable = false
			case retryAttemptsExhausted:
				code = "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED"
				retryable = false
			case retryNotEligible:
			}
		}
	}
	service.persistExecutionFailure(ctx, unit, code, retryable, now)
}

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, emulationstationmeta.ErrTooLarge):
		return emulationstationmeta.ErrTooLarge.Error()
	case errors.Is(err, emulationstationmeta.ErrInvalidUTF8):
		return emulationstationmeta.ErrInvalidUTF8.Error()
	case errors.Is(err, emulationstationmeta.ErrInvalidRoot):
		return emulationstationmeta.ErrInvalidRoot.Error()
	case errors.Is(err, emulationstationmeta.ErrLimitExceeded):
		return emulationstationmeta.ErrLimitExceeded.Error()
	default:
		return emulationstationmeta.ErrInvalidXML.Error()
	}
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int64Pointer(value int64) *int64 { return &value }

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
