package emulationstationimport

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/adapter/files/serversource"
	model "retrom/internal/model/emulationstationimport"
)

type ScanSourceProvider interface {
	ForScan(model.Execution) (ScannerSource, error)
}
type ScanPublisher interface {
	Reset(context.Context, model.Execution) error
	Rejected(context.Context, model.Execution, model.ScanProjection) error
	Publish(context.Context, model.Execution, model.ScanProjection) error
}
type ScanExecutor struct {
	sources   ScanSourceProvider
	publisher ScanPublisher
}

func NewScanExecutor(
	sources ScanSourceProvider,
	publisher ScanPublisher,
) *ScanExecutor {
	return &ScanExecutor{sources: sources, publisher: publisher}
}

type ExecutionError struct {
	Failure model.ExecutionFailure
	Cause   error
}

func (failure *ExecutionError) Error() string {
	return fmt.Sprintf("%s: %v", failure.Failure.Code, failure.Cause)
}
func (failure *ExecutionError) Unwrap() error { return failure.Cause }
func scanExecutionFailure(
	code string,
	retryable bool,
	cause error,
) error {
	return &ExecutionError{Failure: model.ExecutionFailure{Code: code, Retryable: retryable}, Cause: cause}
}

func (executor *ScanExecutor) Execute(ctx context.Context, unit model.Execution) error {
	source, err := executor.sources.ForScan(unit)
	if err != nil {
		return fmt.Errorf("open EmulationStation scanner source: %w", err)
	}
	if err := executor.publisher.Reset(ctx, unit); err != nil {
		return scanExecutionFailure("INTERNAL_ERROR", true, err)
	}
	result, err := NewScanner(source).Scan(ctx, unit.ReleaseYearMax)
	if err != nil {
		if errors.Is(err, ErrNoValidGamelist) {
			if writeErr := executor.publisher.Rejected(
				ctx,
				unit,
				result,
			); writeErr != nil {
				return scanExecutionFailure(
					"INTERNAL_ERROR",
					true,
					errors.Join(err, writeErr),
				)
			}
		}
		return scanExecutionFailure(scanErrorCode(err), errors.Is(err, serversource.ErrRootUnavailable), err)
	}
	if err := executor.publisher.Publish(ctx, unit, result); err != nil {
		if errors.Is(
			err, model.ErrVersionConflict,
		) || ctx.Err() != nil {
			return fmt.Errorf(
				"stop EmulationStation scan publication: %w",
				err,
			)
		}
		cleanupErr := executor.publisher.Reset(ctx, unit)
		return scanExecutionFailure("INTERNAL_ERROR", true, errors.Join(err, cleanupErr))
	}
	return nil
}

func scanErrorCode(err error) string {
	for _, candidate := range []error{
		serversource.ErrRootUnavailable,
		ErrGamelistAbsent,
		ErrNoValidGamelist,
		ErrScanLimit, model.ErrSourceChanged,
	} {
		if errors.Is(err, candidate) {
			return candidate.Error()
		}
	}
	return "INTERNAL_ERROR"
}
