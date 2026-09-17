package uploads

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFinalizationCannotReportCompletedAfterOwnershipChange(t *testing.T) {
	jobID := "job"
	run := Run{UploadID: "upload", JobID: jobID, FinalizationNo: 1, ExecutionNo: 1, WorkerID: "worker", Attempt: 1, Deadline: 300}
	repository := &finalizationStopRepository{workerRepository: &workerRepository{
		current: SessionState{ID: "upload", State: "FINALIZING", FinalizeJobID: &jobID, FinalizationNo: 1},
		job:     Job{ID: jobID, State: "RUNNING", ExecutionNo: 1, WorkerID: "worker", Attempt: 1, Deadline: 300, Lease: 200},
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

func (repository *finalizationStopRepository) CommitWrite(_ context.Context, work func(WriteScope) error) error {
	repository.writes++
	if repository.writes == 2 {
		repository.job.ExecutionNo = 2
	}
	return work(WriteScope{
		Sessions: workerSessions{current: repository.current}, Jobs: workerJobs{repository: repository.workerRepository},
		Finalize: noFinalizationFiles{},
	})
}

type noFinalizationFiles struct{ FinalizationRecords }

func (noFinalizationFiles) Candidates(context.Context, string) ([]Candidate, error) { return nil, nil }
