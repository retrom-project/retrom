package libraryimport

import (
	"context"
	"fmt"
	"time"
)

func (worker *ImportWorker) runQueue(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worker.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-worker.wake:
		}
	}
}

func (worker *ImportWorker) drain(ctx context.Context) {
	ids, err := worker.dependencies.Queue.Queued(ctx)
	if err != nil {
		worker.report(ctx, fmt.Errorf("discover import work: %w", err))
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		work, found, err := worker.dependencies.Queue.Claim(ctx, id)
		if err != nil {
			worker.report(ctx, fmt.Errorf("claim import work: %w", err))
			continue
		}
		if !found {
			continue
		}
		worker.report(ctx, worker.runClaimed(ctx, work))
	}
}

func (worker *ImportWorker) runMaintenance(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for ctx.Err() == nil {
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := worker.dependencies.Recovery.Recover(bounded)
		cancel()
		worker.report(ctx, fmtWorkerError("maintain import executions", err))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-worker.maintenanceWake:
		}
	}
}

func fmtWorkerError(action string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", action, err)
}
