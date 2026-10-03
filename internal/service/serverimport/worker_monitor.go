package serverimport

import (
	"context"
	"sync"
	"time"
)

const cancellationPollInterval = time.Second

// One monitor owns persistence checks for an execution. Walking and hashing
// observe its context locally, including files that are not BIOS candidates.
func (service *Service) monitorExecution(
	ctx context.Context, unit work, done <-chan struct{}, cancel context.CancelCauseFunc,
) {
	poll := time.NewTicker(cancellationPollInterval)
	heartbeat := time.NewTicker(15 * time.Second)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		var err error
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-service.stop:
			cancel(context.Canceled)
			return
		case <-poll.C:
			err = service.leases.Check(ctx, unit)
		case <-heartbeat.C:
			err = service.leases.Heartbeat(ctx, unit)
		}
		if err != nil {
			service.workerError("monitor execution", err)
			cancel(err)
			return
		}
	}
}

type progressThrottle struct {
	mu    sync.Mutex
	phase string
	at    time.Time
}

func (progress *progressThrottle) allow(phase string, current, total int64, now time.Time) bool {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	if phase == progress.phase && current < total && now.Sub(progress.at) < time.Second {
		return false
	}
	progress.phase, progress.at = phase, now
	return true
}
