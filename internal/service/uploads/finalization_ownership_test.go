package uploads

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/uploads"
)

func TestFinalizationCannotReportCompletedAfterOwnershipChange(t *testing.T) {
	run := model.Run{UploadID: "upload", JobID: "job", FinalizationNo: 1, ExecutionNo: 1, WorkerID: "worker", Attempt: 1, Deadline: 300}
	repository := &finalizationStopRepository{}
	service := New(repository, nil, t.TempDir(), func() time.Time { return time.UnixMilli(100) })
	err := service.finalizeFiles(t.Context(), finalizationClaim{Run: run})
	if !errors.Is(err, model.ErrExecutionLost) {
		t.Fatalf("lost final ownership reported success: %v", err)
	}
}

type finalizationStopRepository struct {
	workerRepository
	reads int
}

func (r *finalizationStopRepository) CommitReadCandidates(_ context.Context, _ model.FinalizationOwnershipCommand) ([]model.Candidate, bool, error) {
	r.reads++
	return nil, false, nil
}

func (r *finalizationStopRepository) CommitFinishFinalization(_ context.Context, _ model.FinishFinalizationCommand) (bool, error) {
	return true, nil
}
