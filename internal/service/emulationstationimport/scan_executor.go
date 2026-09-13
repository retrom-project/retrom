package emulationstationimport

import (
	"context"
	"errors"
	"fmt"

	"retrom/internal/adapter/files/serversource"
)

type ScanSourceProvider interface {
	ForScan(Execution) (ScannerSource, error)
}
type ScanPublisher interface {
	Reset(context.Context, Execution) error
	Rejected(context.Context, Execution, ScanProjection) error
	Publish(context.Context, Execution, ScanProjection) error
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
	Failure ExecutionFailure
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
	return &ExecutionError{Failure: ExecutionFailure{Code: code, Retryable: retryable}, Cause: cause}
}

func (executor *ScanExecutor) Execute(ctx context.Context, unit Execution) error {
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
			err,
			ErrVersionConflict,
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
		ErrScanLimit,
		ErrSourceChanged,
	} {
		if errors.Is(err, candidate) {
			return candidate.Error()
		}
	}
	return "INTERNAL_ERROR"
}
