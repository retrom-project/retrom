package pegasusimport

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/pegasusimport"
)

type ScanPublication struct {
	repository model.ScanRepository
	now        func() time.Time
}

func NewScanPublication(repository model.ScanRepository, now func() time.Time) *ScanPublication {
	return &ScanPublication{repository: repository, now: now}
}

func (service *ScanPublication) Save(
	ctx context.Context,
	id model.ExecutionIdentity,
	projection model.ScanProjection,
) error {
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

func (service *ScanPublication) Headers(
	ctx context.Context,
	id model.ExecutionIdentity,
	headers model.ScanHeaders,
) error {
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

func (service *ScanPublication) headerBatch(
	ctx context.Context,
	id model.ExecutionIdentity,
	headers model.ScanHeaders,
) error {
	lease, err := service.ownerLease(ctx, id)
	if err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	if err := service.repository.CommitScanHeaders(ctx, lease, headers); err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	return nil
}

func (service *ScanPublication) Items(
	ctx context.Context, id model.ExecutionIdentity, items []model.ScanItem,
) error {
	if len(items) > 500 {
		return model.ErrScanLimit
	}
	lease, err := service.ownerLease(ctx, id)
	if err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	if err := service.repository.CommitScanItems(ctx, lease, items); err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	return nil
}

func (service *ScanPublication) Finish(
	ctx context.Context,
	id model.ExecutionIdentity,
	summary model.ScanSummary,
) error {
	lease, err := service.ownerLease(ctx, id)
	if err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	actual, err := service.repository.LoadScanShape(ctx, lease.Before.ImportID)
	if err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	if actual != summary.Shape {
		return model.ErrVersionConflict
	}
	lease.NowMS = service.now().UnixMilli()
	if err := ValidateExecution(lease.Before, id, lease.NowMS); err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	if err := service.repository.CommitScanFinish(ctx, lease, summary); err != nil {
		return fmt.Errorf("persist Pegasus scan: %w", err)
	}
	return nil
}

func (service *ScanPublication) ownerLease(
	ctx context.Context, id model.ExecutionIdentity,
) (model.ScanLease, error) {
	if err := ctx.Err(); err != nil {
		return model.ScanLease{}, fmt.Errorf("publish Pegasus scan: %w", err)
	}
	before, err := service.repository.LoadScanOwner(ctx, id.JobID)
	if err != nil {
		return model.ScanLease{}, fmt.Errorf("read Pegasus scanner ownership: %w", err)
	}
	now := service.now().UnixMilli()
	if err := ValidateExecution(before, id, now); err != nil {
		return model.ScanLease{}, err
	}
	if before.Kind != "SERVER_PEGASUS_SCAN" || before.JobState != "RUNNING" || before.ImportState != "SCANNING" {
		return model.ScanLease{}, model.ErrVersionConflict
	}
	return model.ScanLease{Before: before, NowMS: now}, nil
}
