package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"

	metadatamodel "retrom/internal/model/metadata"
	"retrom/internal/service/metadatascrape"
)

func TestMediaCrashReservationAndOriginalDeadlineSurviveRecovery(t *testing.T) {
	fixture := newMediaFixture(t)
	now := fixture.now.UnixMilli()
	recoveryExec(t, fixture.database, `UPDATE jobs SET state='RUNNING',attempt_count=1,worker_id='crashed',
 execution_started_at_ms=?,execution_deadline_at_ms=?,leased_until_ms=? WHERE id=?`, now-1790000, now+10000, now-1, fixture.jobID)
	recoveryExec(t, fixture.database, `UPDATE metadata_media_runs SET charged_bytes=123`)
	recoveryExec(t, fixture.database, `UPDATE scrape_candidate_assets SET status='FETCHING',media_charged_bytes=123,media_reserved_bytes=123`)
	calls := 0
	source := mediaSource(func(ctx context.Context, _ metadatamodel.AssetReference, _ int64) (metadatamodel.AssetData, error) {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 10*time.Second {
			t.Errorf("recovered execution received a new deadline: %v", deadline)
		}
		return metadatamodel.AssetData{ReceivedBytes: 7}, metadatamodel.ErrAssetDecodeFailed
	})
	worker := fixture.worker(source)
	if err := worker.Run(t.Context(), fixture.jobID); err != nil {
		t.Fatal(err)
	}
	queued := fixture.snapshot(t)
	if queued.Job.State != "QUEUED" || queued.Job.AvailableAt != now+1000 || queued.Charged != 123 || calls != 0 {
		t.Fatalf("recovery=%+v", queued)
	}
	fixture.now = fixture.now.Add(time.Second)
	if err := worker.Run(t.Context(), fixture.jobID); !errors.Is(err, metadatamodel.ErrAssetDecodeFailed) {
		t.Fatal(err)
	}
	final := fixture.snapshot(t)
	if final.Job.Attempt != 2 || final.Job.Deadline != now+10000 || final.Charged != 130 || final.Asset.Reserved != 0 {
		t.Fatalf("crash reservation refunded or deadline reset: %+v", final)
	}
}

func TestMediaDeadlinePrecedesFutureAvailability(t *testing.T) {
	fixture := newMediaFixture(t)
	now := fixture.now.UnixMilli()
	recoveryExec(t, fixture.database, `UPDATE jobs SET execution_deadline_at_ms=?,available_at_ms=? WHERE id=?`, now, now+999999, fixture.jobID)
	worker := fixture.worker(mediaSource(func(context.Context, metadatamodel.AssetReference, int64) (metadatamodel.AssetData, error) {
		t.Fatal("expired job downloaded")
		return metadatamodel.AssetData{}, nil
	}))
	ids, err := worker.Recover(t.Context())
	if err != nil || len(ids) != 1 {
		t.Fatalf("deadline discovery=%v/%v", ids, err)
	}
	if err := worker.Run(t.Context(), fixture.jobID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expiry cause=%v", err)
	}
	if snapshot := fixture.snapshot(t); snapshot.Job.State != "FAILED" || snapshot.Job.Attempt != 0 || snapshot.Charged != 0 {
		t.Fatalf("deadline execution=%+v", snapshot)
	}
}

func TestMediaStaticCancellationReconcilesAsset(t *testing.T) {
	fixture := newMediaFixture(t)
	recoveryExec(t, fixture.database, `UPDATE jobs SET state='CANCELLED',cancel_requested_at_ms=?,finished_at_ms=?,version=version+1 WHERE id=?`,
		fixture.now.UnixMilli(), fixture.now.UnixMilli(), fixture.jobID)
	worker := metadatascrape.NewMediaWorker(NewMedia(fixture.database), nil, nil, fixture.clock)
	if err := worker.Run(t.Context(), fixture.jobID); err != nil {
		t.Fatal(err)
	}
	snapshot := fixture.snapshot(t)
	if snapshot.Job.State != "CANCELLED" || snapshot.Asset.Status != "CANCELLED" || snapshot.Job.Attempt != 0 {
		t.Fatalf("cancel=%+v", snapshot)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM job_events WHERE job_id=?`, fixture.jobID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("static cancellation duplicated event: %d", count)
	}
}
