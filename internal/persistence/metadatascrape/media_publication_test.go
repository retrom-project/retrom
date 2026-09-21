package metadatascrape

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"retrom/internal/hasheous"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/testsupport"
)

func TestMediaFinalEventFailureRollsBackBlobAndReadyAsset(t *testing.T) {
	fixture := newMediaFixture(t)
	cause := errors.New("media completion event unavailable")
	hits := 0
	faulted := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO job_events") && len(args) == 4 && args[0].Value == "SUCCEEDED" && args[3].Value == fixture.jobID {
				hits++
				return cause
			}
			return nil
		},
	})
	source := mediaSource(func(context.Context, hasheous.AssetRef, int64) (hasheous.AssetData, error) {
		return hasheous.AssetData{Bytes: []byte("media"), ReceivedBytes: 5, Width: 1, Height: 1, MediaType: "image/png"}, nil
	})
	worker := metadatascrape.NewMediaWorker(NewMedia(faulted), source, fixture.blobs, fixture.clock)
	if err := worker.Run(t.Context(), fixture.jobID); !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("final cause=%v hits=%d", err, hits)
	}
	snapshot := fixture.snapshot(t)
	if snapshot.Asset.Status != "FETCHING" || snapshot.Job.State != "RUNNING" || snapshot.Charged != 5 || snapshot.Asset.Reserved != 0 {
		t.Fatalf("partial publication or lost bytes: %+v", snapshot)
	}
	var count int
	if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rolled back media registered %d blobs", count)
	}
	if stats := faulted.Stats(); stats.InUse != 0 {
		t.Fatalf("media publication leaked %d connections", stats.InUse)
	}
}

func TestMediaCloseJoinsReadAndAccountsKnownBytes(t *testing.T) {
	fixture := newMediaFixture(t)
	entered := make(chan struct{})
	stopped := make(chan error, 1)
	source := mediaSource(func(ctx context.Context, _ hasheous.AssetRef, _ int64) (hasheous.AssetData, error) {
		close(entered)
		<-ctx.Done()
		cause := context.Cause(ctx)
		stopped <- cause
		return hasheous.AssetData{ReceivedBytes: 7}, cause
	})
	service := metadatascrape.NewWithMedia(nil, nil, fixture.worker(source), fixture.clock)
	if !service.ResumeMediaJob(t.Context(), fixture.jobID) {
		t.Fatal("media dispatch rejected")
	}
	<-entered
	service.Close()
	if cause := <-stopped; !errors.Is(cause, metadatascrape.ErrWorkerClosed) {
		t.Fatalf("source Close cause=%v", cause)
	}
	snapshot := fixture.snapshot(t)
	if snapshot.Job.State != "FAILED" || snapshot.Charged != 7 || snapshot.Asset.Reserved != 0 {
		t.Fatalf("Close failed to settle/join: %+v", snapshot)
	}
}
