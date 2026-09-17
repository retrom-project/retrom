package libraryimport

import (
	"context"

	libraryservice "retrom/internal/model/libraryimport"
	librarypersistence "retrom/internal/repo/libraryimport"
)

type queuedJobRun struct {
	id          string
	availableAt int64
}

func (service *Service) queuedJobRuns(ctx context.Context, kind string) []queuedJobRun {
	jobs, err := librarypersistence.NewQueuedJobs(service.database).Queued(ctx, kind)
	if err != nil {
		return nil
	}
	queued := make([]queuedJobRun, 0, len(jobs))
	for _, job := range jobs {
		queued = append(queued, queuedJobRun{id: job.ID, availableAt: job.AvailableAtMS})
	}
	return queued
}

var _ libraryservice.QueuedJobReader = (*librarypersistence.QueuedJobs)(nil)
