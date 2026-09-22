package sourceimport

import (
	"context"
	"fmt"
	"time"
)

type ScanPublication struct {
	repository ScanRepository
	now        func() time.Time
}

func NewScanPublication(repository ScanRepository, now func() time.Time) *ScanPublication {
	return &ScanPublication{repository: repository, now: now}
}

func (service *ScanPublication) Save(ctx context.Context, id ExecutionIdentity, projection ScanProjection) error {
	if err := service.Headers(ctx, id, projection.Headers); err != nil {
		return err
	}
	for offset := 0; offset < len(projection.Items); offset += 500 {
		if err := service.Items(ctx, id, projection.Items[offset:min(offset+500, len(projection.Items))]); err != nil {
			return err
		}
	}
	return service.Finish(ctx, id, projection.Summary)
}

func (service *ScanPublication) Headers(ctx context.Context, id ExecutionIdentity, headers ScanHeaders) error {
	if len(headers.Metadata) > MaxMetadataFiles {
		return ErrScanLimit
	}
	for offset := 0; offset < len(headers.Metadata); offset += 500 {
		batch := ScanHeaders{Metadata: headers.Metadata[offset:min(offset+500, len(headers.Metadata))]}
		if err := service.headerBatch(ctx, id, batch); err != nil {
			return err
		}
	}
	for offset := 0; offset < len(headers.Collections); offset += 500 {
		batch := ScanHeaders{Collections: headers.Collections[offset:min(offset+500, len(headers.Collections))]}
		if err := service.headerBatch(ctx, id, batch); err != nil {
			return err
		}
	}
	if len(headers.Metadata) == 0 && len(headers.Collections) == 0 {
		return service.headerBatch(ctx, id, headers)
	}
	return nil
}

func (service *ScanPublication) headerBatch(ctx context.Context, id ExecutionIdentity, headers ScanHeaders) error {
	return service.withOwner(ctx, id, func(scope ScanScope, owner ScanLease) error {
		return scope.Write.Headers(ctx, owner, headers)
	})
}

func (service *ScanPublication) Items(ctx context.Context, id ExecutionIdentity, items []ScanItem) error {
	if len(items) > 500 {
		return ErrScanLimit
	}
	return service.withOwner(ctx, id, func(scope ScanScope, owner ScanLease) error {
		return scope.Write.Items(ctx, owner, items)
	})
}

func (service *ScanPublication) Finish(ctx context.Context, id ExecutionIdentity, summary ScanSummary) error {
	return service.withOwner(ctx, id, func(scope ScanScope, owner ScanLease) error {
		actual, err := scope.Read.Shape(ctx, owner.Before.ImportID)
		if err != nil {
			return fmt.Errorf("read persisted Source scan shape: %w", err)
		}
		if actual != summary.Shape {
			return ErrVersionConflict
		}
		// Count queries may take time; the final write must still own an unexpired execution.
		owner.NowMS = service.now().UnixMilli()
		if err := ValidateExecution(owner.Before, id, owner.NowMS); err != nil {
			return err
		}
		return scope.Write.Finish(ctx, owner, summary)
	})
}

func (service *ScanPublication) withOwner(
	ctx context.Context, id ExecutionIdentity, work func(ScanScope, ScanLease) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish Source scan: %w", err)
	}
	err := service.repository.WithScan(ctx, func(scope ScanScope) error {
		before, err := scope.Read.Current(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read Source scanner ownership: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before, id, now); err != nil {
			return err
		}
		if before.Kind != "IMPORT_SCAN" || before.JobState != "RUNNING" || before.ImportState != "SCANNING" {
			return ErrVersionConflict
		}
		return work(scope, ScanLease{Before: before, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("persist Source scan: %w", err)
	}
	return nil
}
