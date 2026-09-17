package uploads

import (
	"context"
	"errors"
	model "retrom/internal/model/uploads"
	"testing"
	"time"
)

func TestFinalizationCannotReportCompletedAfterOwnershipChange(t *testing.T) {
	jobID := "job"
	run := model.Run{UploadID: "upload", JobID: jobID, FinalizationNo: 1, ExecutionNo: 1, WorkerID: "worker", Attempt: 1, Deadline: 300}
	repository := &finalizationStopRepository{workerRepository: &workerRepository{
		current: model.SessionState{ID: "upload", State: "FINALIZING", FinalizeJobID: &jobID, FinalizationNo: 1},
		job:     model.Job{ID: jobID, State: "RUNNING", ExecutionNo: 1, WorkerID: "worker", Attempt: 1, Deadline: 300, Lease: 200},
	}}
	service := New(repository, nil, t.TempDir(), func() time.Time { return time.UnixMilli(100) })
	err := service.finalizeFiles(t.Context(), finalizationClaim{Run: run})
	if !errors.Is(err, ErrExecutionLost) {
		t.Fatalf("lost final ownership reported success: %v", err)
	}
}

type finalizationStopRepository struct {
	*workerRepository
	writes int
}

func (repository *finalizationStopRepository) CommitWrite(_ context.Context, work func(model.WriteScope) error) error {
	repository.writes++
	if repository.writes == 2 {
		repository.job.ExecutionNo = 2
	}
	return work(model.WriteScope{
		Sessions: workerSessions{current: repository.current}, Jobs: workerJobs{repository: repository.workerRepository},
		Finalize: noFinalizationFiles{},
	})
}

type noFinalizationFiles struct{ model.FinalizationRecords }

func (noFinalizationFiles) Candidates(context.Context, string) ([]model.Candidate, error) {
	return nil, nil
}
