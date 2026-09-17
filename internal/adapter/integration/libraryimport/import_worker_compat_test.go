//go:build integration

package libraryimport

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	libraryimportmodel "retrom/internal/model/libraryimport"
	repository "retrom/internal/repo/libraryimport"
	libraryimportservice "retrom/internal/service/libraryimport"
)

const importGroupLease = libraryimportmodel.ImportExecutionLease

type queuedCreationWork struct {
	importID, jobID, workerID, actorUserID             string
	request                                            CreateRequest
	targetSnapshot                                     importGroupTargetSnapshot
	executionStartedAt, executionNo, executionDeadline int64
	attempt                                            int
}

func (work queuedCreationWork) creationIntent() *libraryimportmodel.QueuedImportExecution {
	return &libraryimportmodel.QueuedImportExecution{
		ImportID:    work.importID,
		JobID:       work.jobID,
		WorkerID:    work.workerID,
		ActorUserID: work.actorUserID,
		ExecutionNo: work.executionNo,
		Attempt:     int64(work.attempt),
		StartedAtMS: work.executionStartedAt,
		DeadlineMS:  work.executionDeadline,
		Target:      work.targetSnapshot,
	}
}

func (service *Service) testExecutions() *libraryimportservice.ImportExecutions {
	return libraryimportservice.NewImportExecutions(repository.NewImportExecutions(service.database), service.now)
}

func (service *Service) claimImportGroup(ctx context.Context, id string) (queuedCreationWork, error) {
	work, found, err := service.testExecutions().Claim(ctx, id)
	if err != nil {
		return queuedCreationWork{}, err
	}
	if !found {
		return queuedCreationWork{}, ErrInvalid
	}
	value := work.Execution
	return queuedCreationWork{
		importID:           value.ImportID,
		jobID:              value.JobID,
		workerID:           value.WorkerID,
		actorUserID:        value.ActorUserID,
		request:            work.Request,
		targetSnapshot:     value.Target,
		executionStartedAt: value.StartedAtMS,
		executionNo:        value.ExecutionNo,
		executionDeadline:  value.DeadlineMS,
		attempt:            int(value.Attempt),
	}, nil
}

func (service *Service) recordImportGroupProgress(
	ctx context.Context,
	work queuedCreationWork,
	_ string,
	count int,
) error {
	return service.testExecutions().Progress(ctx, *work.creationIntent(), count)
}

func (service *Service) finishImportGroupFailure(ctx context.Context, work queuedCreationWork, cause error) {
	cleanup.Error("test ordinary import failure", service.testExecutions().Fail(ctx, *work.creationIntent(), cause))
}

type (
	creationPlan              = libraryimportmodel.PreparedImport
	importGroupTargetSnapshot = libraryimportmodel.ImportTargetSnapshot
)

func (service *Service) prepareCreation(ctx context.Context, request CreateRequest) (creationPlan, error) {
	prepared, err := service.importPreparation().Prepare(ctx, request)
	if err != nil {
		return creationPlan{}, fmt.Errorf("prepare import creation: %w", err)
	}
	return prepared, nil
}
