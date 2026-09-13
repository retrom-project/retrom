package metadatascrape

import (
	"context"
	"errors"
	"testing"

	jobpersistence "retrom/internal/persistence/jobs"
	"retrom/internal/service/jobs"
	"retrom/internal/service/metadatascrape"

	"retrom/internal/adapter/metadata/hasheous"
)

func TestMediaRunSerializesFrozenPositions(t *testing.T) {
	fixture := newMediaFixture(t, hasheous.AssetRef{ProviderAssetID: "first", Kind: "COVER", Path: "/api/v1/images/first"},
		hasheous.AssetRef{ProviderAssetID: "second", Kind: "COVER", Ordinal: 1, Path: "/api/v1/images/second"})
	var second string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT media_fetch_job_id FROM scrape_candidate_assets WHERE ordinal=1`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	source := mediaSource(func(_ context.Context, ref hasheous.AssetRef, _ int64) (hasheous.AssetData, error) {
		if ref.ProviderAssetID == "first" {
			close(entered)
			<-release
		}
		return hasheous.AssetData{Bytes: []byte("media"), ReceivedBytes: 5, Width: 1, Height: 1, MediaType: "image/png"}, nil
	})
	go func() { completed <- fixture.worker(source).Run(t.Context(), fixture.jobID) }()
	<-entered
	if err := fixture.worker(source).Run(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	var attempt int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT attempt_count FROM jobs WHERE id=?`, second).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if attempt != 0 {
		t.Fatalf("later frozen position consumed %d attempts during prior read", attempt)
	}
	if err := fixture.worker(source).Run(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	snapshot := fixture.snapshot(t)
	if snapshot.Asset.Order != 0 || !snapshot.Frozen || snapshot.Charged != 10 {
		t.Fatalf("frozen order/budget=%+v", snapshot)
	}
}

func TestManualMediaRetryWaitsForCurrentlyRunningLaterPosition(t *testing.T) {
	fixture := newMediaFixture(t, hasheous.AssetRef{ProviderAssetID: "first", Kind: "COVER", Path: "/api/v1/images/first"},
		hasheous.AssetRef{ProviderAssetID: "second", Kind: "COVER", Ordinal: 1, Path: "/api/v1/images/second"})
	var second string
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT media_fetch_job_id FROM scrape_candidate_assets WHERE ordinal=1`).Scan(&second); err != nil {
		t.Fatal(err)
	}
	entered, release, completed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	firstCalls := 0
	source := mediaSource(func(_ context.Context, ref hasheous.AssetRef, _ int64) (hasheous.AssetData, error) {
		if ref.ProviderAssetID == "first" {
			firstCalls++
			if firstCalls == 1 {
				return hasheous.AssetData{}, hasheous.ErrAssetNetwork
			}
		} else {
			close(entered)
			<-release
		}
		return hasheous.AssetData{Bytes: []byte("media"), ReceivedBytes: 5, Width: 1, Height: 1, MediaType: "image/png"}, nil
	})
	worker := fixture.worker(source)
	if err := worker.Run(t.Context(), fixture.jobID); !errors.Is(err, hasheous.ErrAssetNetwork) {
		t.Fatal(err)
	}
	version := fixture.snapshot(t).Job.Version
	go func() { completed <- worker.Run(t.Context(), second) }()
	<-entered
	_, err := jobs.New(jobpersistence.New(fixture.database), fixture.clock).Retry(t.Context(), fixture.jobID, version)
	if err != nil {
		close(release)
		<-completed
		t.Fatal(err)
	}
	err = worker.Run(t.Context(), fixture.jobID)
	snapshot := fixture.snapshot(t)
	close(release)
	if finishErr := <-completed; finishErr != nil {
		t.Fatal(finishErr)
	}
	if err != nil || firstCalls != 1 || snapshot.Job.State != "QUEUED" || snapshot.Job.Attempt != 0 {
		t.Fatalf("manual retry overlapped current run position: calls=%d state=%s attempt=%d cause=%v", firstCalls, snapshot.Job.State, snapshot.Job.Attempt, err)
	}
	if err := worker.Run(t.Context(), fixture.jobID); err != nil {
		t.Fatal(err)
	}
	if firstCalls != 2 {
		t.Fatalf("retry did not resume after active position: %d", firstCalls)
	}
}

func TestMediaCapacityIncludesCancellingReadUntilLeaseEnds(t *testing.T) {
	fixture := newMediaFixture(t)
	now := fixture.now.UnixMilli()
	recoveryExec(t, fixture.database, `UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,
 worker_id='stopping',attempt_count=1,leased_until_ms=?,execution_deadline_at_ms=? WHERE id=?`, now, now+60000, now+1800000, fixture.jobID)
	err := NewMedia(fixture.database).WithWrite(t.Context(), func(scope metadatascrape.MediaScope) error {
		count, err := scope.Read.Running(t.Context(), now)
		if err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("canceling source disappeared from media capacity: %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
