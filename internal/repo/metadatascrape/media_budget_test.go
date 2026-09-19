package metadatascrape

import (
	"context"
	"errors"
	"io"
	"testing"

	metadatamodel "retrom/internal/model/metadata"

	metadatascrapemodel "retrom/internal/model/metadatascrape"

	jobpersistence "retrom/internal/repo/jobs"
	"retrom/internal/service/jobs"
)

func TestMediaFailedBytesRemainChargedAcrossManualRetry(t *testing.T) {
	fixture := newMediaFixture(t)
	source := mediaSource(func(_ context.Context, _ metadatamodel.AssetReference, limit int64) (metadatamodel.AssetData, error) {
		if limit != metadatamodel.MaximumAssetReadBytes {
			t.Errorf("reservation=%d", limit)
		}
		return metadatamodel.AssetData{ReceivedBytes: 3}, errors.Join(metadatamodel.ErrAssetNetwork, io.ErrUnexpectedEOF)
	})
	err := fixture.worker(source).Run(t.Context(), fixture.jobID)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read cause lost: %v", err)
	}
	first := fixture.snapshot(t)
	if first.Job.State != "FAILED" || first.Asset.Status != "FAILED" || first.Charged != 3 || first.Asset.Reserved != 0 {
		t.Fatalf("failed read accounting=%+v", first)
	}
	_, err = jobs.New(jobpersistence.New(fixture.database), fixture.clock).Retry(t.Context(), fixture.jobID, first.Job.Version)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.worker(source).Run(t.Context(), fixture.jobID); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
	second := fixture.snapshot(t)
	if second.Job.Execution != 2 || second.Charged != 6 || second.Asset.Charged != 6 || second.Asset.Order != first.Asset.Order {
		t.Fatalf("manual retry reset run budget/order: %+v", second)
	}
}

func TestMediaBudgetUsesRemainingReservationAndRejectsFurtherReads(t *testing.T) {
	fixture := newMediaFixture(t)
	recoveryExec(t, fixture.database, `UPDATE metadata_media_runs SET charged_bytes=?`, metadatascrapemodel.MediaRunBudget-2)
	calls := 0
	source := mediaSource(func(_ context.Context, _ metadatamodel.AssetReference, limit int64) (metadatamodel.AssetData, error) {
		calls++
		if limit != 2 {
			t.Errorf("unbounded final reservation=%d", limit)
		}
		return metadatamodel.AssetData{ReceivedBytes: 2}, metadatamodel.ErrAssetReadLimit
	})
	if err := fixture.worker(source).Run(t.Context(), fixture.jobID); !errors.Is(err, metadatamodel.ErrAssetReadLimit) {
		t.Fatal(err)
	}
	first := fixture.snapshot(t)
	if first.Charged != metadatascrapemodel.MediaRunBudget || first.Asset.Reserved != 0 {
		t.Fatalf("budget=%+v", first)
	}
	recoveryExec(t, fixture.database, `UPDATE jobs SET state='QUEUED',finished_at_ms=NULL,error_code=NULL,error_retryable=NULL,
 worker_id=NULL,version=version+1 WHERE id=?`, fixture.jobID)
	if err := fixture.worker(source).Run(t.Context(), fixture.jobID); !errors.Is(err, metadatamodel.ErrAssetReadLimit) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("exhausted budget issued %d requests", calls)
	}
}
