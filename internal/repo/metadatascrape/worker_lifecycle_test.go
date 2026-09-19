package metadatascrape

import (
	"context"
	"errors"
	"testing"
	"time"

	metadatascrapemodel "retrom/internal/model/metadatascrape"
	"retrom/internal/service/metadatascrape"
)

func TestMetadataStartupDispatchesDurableJobAndCloseJoinsWork(t *testing.T) {
	database := recoveryDatabase(t)
	entered := make(chan struct{})
	finished := make(chan error, 1)
	processor := recoveryProcess(func(ctx context.Context, _ metadatascrapemodel.WorkerClaim, _ string) (int, string, error) {
		close(entered)
		<-ctx.Done()
		err := context.Cause(ctx)
		finished <- err
		return 0, "METADATA_EXECUTION_INTERRUPTED", err
	})
	worker := metadatascrape.NewWorker(NewWorker(database), processor, recoveryNow)
	service := metadatascrape.New(NewScheduler(database), worker, recoveryNow)
	t.Cleanup(service.Close)
	service.Start(t.Context())
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("durable queued work was not started")
	}
	service.Close()
	if err := <-finished; !errors.Is(err, metadatascrape.ErrWorkerClosed) {
		t.Fatalf("worker cancellation=%v", err)
	}
	var state, run string
	if err := database.QueryRowContext(t.Context(), `SELECT j.state,r.state FROM jobs j JOIN metadata_scrape_runs r ON r.job_id=j.id WHERE j.id='job'`).Scan(&state, &run); err != nil {
		t.Fatal(err)
	}
	if state != "FAILED" || run != "FAILED" {
		t.Fatalf("Close returned before atomic settlement: %s/%s", state, run)
	}
	if service.Dispatch(t.Context(), "run") {
		t.Fatal("closed dispatcher accepted work")
	}
}
