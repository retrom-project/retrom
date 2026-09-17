package pegasusimport

import (
	"context"
	"fmt"
	model "retrom/internal/model/pegasusimport"
	"time"
)

type ScanPublication struct {
	repository model.ScanRepository
	now        func() time.Time
}

func NewScanPublication(repository model.ScanRepository, now func() time.Time) *ScanPublication {
	return &ScanPublication{repository: repository, now: now}
}

func (service *ScanPublication) Save(ctx context.Context, id model.ExecutionIdentity, projection model.ScanProjection) error {
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

func (service *ScanPublication) Headers(ctx context.Context, id model.ExecutionIdentity, headers model.ScanHeaders) error {
	if len(headers.Metadata) > model.MaxMetadataFiles {
		return model.ErrScanLimit
	}
	for offset := 0; offset < len(headers.Metadata); offset += 500 {
		batch := model.ScanHeaders{Metadata: headers.Metadata[offset:min(offset+500, len(headers.Metadata))]}
		if err := service.headerBatch(ctx, id, batch); err != nil {
			return err
		}
	}
	for offset := 0; offset < len(headers.Collections); offset += 500 {
		batch := model.ScanHeaders{Collections: headers.Collections[offset:min(offset+500, len(headers.Collections))]}
		if err := service.headerBatch(ctx, id, batch); err != nil {
			return err
		}
	}
	if len(headers.Metadata) == 0 && len(headers.Collections) == 0 {
		return service.headerBatch(ctx, id, headers)
	}
	return nil
}

func (service *ScanPublication) headerBatch(ctx context.Context, id model.ExecutionIdentity, headers model.ScanHeaders) error {
	return service.withOwner(ctx, id, func(scope model.ScanScope, owner model.ScanLease) error {
		return scope.Write.Headers(ctx, owner, headers)
	})
}

func (service *ScanPublication) Items(ctx context.Context, id model.ExecutionIdentity, items []model.ScanItem) error {
	if len(items) > 500 {
		return model.ErrScanLimit
	}
	return service.withOwner(ctx, id, func(scope model.ScanScope, owner model.ScanLease) error {
		return scope.Write.Items(ctx, owner, items)
	})
}

func (service *ScanPublication) Finish(ctx context.Context, id model.ExecutionIdentity, summary model.ScanSummary) error {
	return service.withOwner(ctx, id, func(scope model.ScanScope, owner model.ScanLease) error {
		actual, err := scope.Read.Shape(ctx, owner.Before.ImportID)
		if err != nil {
			return fmt.Errorf("read persisted Pegasus scan shape: %w", err)
		}
		if actual != summary.Shape {
			return model.ErrVersionConflict
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
	ctx context.Context, id model.ExecutionIdentity, work func(model.ScanScope, model.ScanLease) error,
) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish Pegasus scan: %w", err)
	}
	err := service.repository.WithScan(ctx, func(scope model.ScanScope) error {
		before, err := scope.Read.Current(ctx, id.JobID)
		if err != nil {
			return fmt.Errorf("read Pegasus scanner ownership: %w", err)
		}
		now := service.now().UnixMilli()
		if err := ValidateExecution(before, id, now); err != nil {
			return err
		}
		if before.Kind != "SERVER_PEGASUS_SCAN" || before.JobState != "RUNNING" || before.ImportState != "SCANNING" {
			return model.ErrVersionConflict
		}
		return work(scope, model.ScanLease{Before: before, NowMS: now})
	})
	if err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	return nil
}
