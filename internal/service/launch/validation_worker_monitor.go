package launch

import (
	"context"
	"time"

	model "retrom/internal/model/launch"
)

type validationRealTicker struct{ *time.Ticker }

func (ticker validationRealTicker) Ticks() <-chan time.Time { return ticker.C }

type validationMonitor struct {
	context context.Context
	stop    chan struct{}
	done    chan struct{}
}

func (service *ValidationWorker) monitor(
	ctx context.Context,
	claim model.ValidationClaim,
	cancel context.CancelCauseFunc,
) *validationMonitor {
	monitor := &validationMonitor{context: ctx, stop: make(chan struct{}), done: make(chan struct{})}
	ticker := service.environment.NewTicker(15 * time.Second)
	go func() {
		defer close(monitor.done)
		defer ticker.Stop()
		for {
			select {
			case <-monitor.stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.Ticks():
				if err := service.heartbeat(ctx, claim); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return monitor
}

func (monitor *validationMonitor) Close() error {
	close(monitor.stop)
	<-monitor.done
	cause := context.Cause(monitor.context)
	return validationStageError("validation monitor", cause)
}

func (service *ValidationWorker) heartbeat(ctx context.Context, claim model.ValidationClaim) error {
	err := service.repository.WithWorker(ctx, func(scope model.ValidationWorkerScope) error {
		current, found, err := scope.Jobs.Read(ctx, claim.Job.ID)
		if err != nil {
			return validationStageError("read heartbeat owner", err)
		}
		now := service.environment.Now().UnixMilli()
		if !found {
			return model.ErrValidationOwnership
		}
		if err := validationOwnerError(current, claim, now); err != nil {
			return err
		}
		lease := min(now+int64(time.Minute/time.Millisecond), *current.DeadlineMS)
		return scope.Jobs.Renew(ctx, claim, now, lease)
	})
	return validationStageError("heartbeat validation", err)
}
