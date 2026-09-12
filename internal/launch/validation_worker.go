package launch

import (
	"context"
	"time"

	"retrom/internal/cleanup"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"
)

func (service *Service) validationWorker() *application.ValidationWorker {
	return application.NewValidationWorker(
		persistence.NewValidationWorker(service.database),
		application.ValidationWorkerEnvironment{Now: service.now},
	)
}

func (service *Service) resumeValidationJob(parent context.Context, id string) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(context.Canceled)
	done, accepted := service.validationRuns.register(cancel)
	if !accepted {
		return
	}
	defer done()
	cleanup.Error("resume variant validation", service.validationWorker().Run(ctx, id))
}

// ResumeQueuedValidationJobs recovers stale attempts before dispatching due work.
func (service *Service) ResumeQueuedValidationJobs() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx, stop := context.WithCancelCause(ctx)
	defer stop(context.Canceled)
	done, accepted := service.validationRuns.register(stop)
	if !accepted {
		return
	}
	defer done()
	ids, err := service.validationWorker().Recover(ctx)
	if err != nil {
		cleanup.Error("recover variant validations", err)
		return
	}
	for _, id := range ids {
		go service.resumeValidationJob(context.Background(), id)
	}
}
