package libraryimport

import (
	"encoding/json"
	"fmt"

	model "retrom/internal/model/libraryimport"
)

func importMoment(value int64) *int64 { return &value }
func importTruth(value bool) *bool    { return &value }
func importTransition(before model.ImportWorkerSnapshot, now int64) model.ImportWorkerTransition {
	return model.ImportWorkerTransition{
		Before: before,
		AtMS:   now,
		Job: model.ImportWorkerProjection{
			StartedAtMS: before.StartedAtMS, DeadlineAtMS: before.DeadlineAtMS,
			Execution:           before.Creation.Execution,
			State:               before.Creation.JobState,
			AvailableAtMS:       before.AvailableAtMS,
			LeaseUntilMS:        before.LeaseUntilMS,
			HeartbeatAtMS:       before.HeartbeatAtMS,
			FinishedAtMS:        before.FinishedAtMS,
			ErrorCode:           before.ErrorCode,
			Retryable:           before.Retryable,
			CancelRequestedAtMS: before.CancelRequestedAtMS,
			CancelReason:        before.CancelReason,
		},
		Parent: &model.ImportWorkerParent{
			State:               before.Creation.ImportState,
			CompletedAtMS:       before.ParentCompletedAtMS,
			CancelRequestedAtMS: before.ParentCancelRequestedAtMS,
			CancelReason:        before.ParentCancelReason,
			ErrorCode:           before.ParentErrorCode,
		},
	}
}

func importWorkerEvent(
	execution model.QueuedImportExecution,
	kind string,
	now int64,
	fields map[string]any,
) (model.CreationEvent, error) {
	fields["schemaVersion"], fields["executionNo"], fields["attempt"] = 1, execution.ExecutionNo, execution.Attempt
	encoded, err := json.Marshal(fields)
	if err != nil {
		return model.CreationEvent{}, fmt.Errorf("encode import worker event: %w", err)
	}
	return model.CreationEvent{
		JobID:     execution.JobID,
		ScopeType: "IMPORT_GROUP",
		ScopeID:   execution.ImportID,
		Kind:      kind,
		DataJSON:  string(encoded),
		NowMS:     now,
	}, nil
}

func clearImportExecutionOwner(change *model.ImportWorkerTransition) {
	change.Job.Execution.WorkerID = ""
	change.Job.LeaseUntilMS = nil
	change.Job.HeartbeatAtMS = nil
}

func importFailedTransition(
	before model.ImportWorkerSnapshot,
	code string,
	retryable bool,
	now int64,
) (model.ImportWorkerTransition, error) {
	change := importTransition(before, now)
	clearImportExecutionOwner(&change)
	change.Job.State = "FAILED"
	change.Job.FinishedAtMS = importMoment(now)
	change.Job.ErrorCode = creationOptional(code)
	change.Job.Retryable = importTruth(retryable)
	change.Parent.State = "FAILED"
	change.Parent.CompletedAtMS = importMoment(now)
	change.Parent.ErrorCode = creationOptional(code)
	event, err := importWorkerEvent(
		before.Creation.Execution,
		"FAILED",
		now,
		map[string]any{"errorCode": code, "errorRetryable": retryable},
	)
	if err != nil {
		return model.ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}

func importCancelledTransition(
	before model.ImportWorkerSnapshot,
	reason string,
	now int64,
) (model.ImportWorkerTransition, error) {
	change := importTransition(before, now)
	clearImportExecutionOwner(&change)
	change.Job.State = "CANCELLED"
	change.Job.FinishedAtMS = importMoment(now)
	if change.Job.CancelRequestedAtMS == nil {
		change.Job.CancelRequestedAtMS = importMoment(now)
	}
	if change.Job.CancelReason == nil {
		change.Job.CancelReason = creationOptional(reason)
	}
	change.Parent.State = "CANCELLED"
	change.Parent.CompletedAtMS = importMoment(now)
	if change.Parent.CancelRequestedAtMS == nil {
		change.Parent.CancelRequestedAtMS = importMoment(now)
	}
	if change.Parent.CancelReason == nil {
		change.Parent.CancelReason = creationOptional(reason)
	}
	event, err := importWorkerEvent(before.Creation.Execution, "CANCELLED", now, map[string]any{"state": "CANCELLED"})
	if err != nil {
		return model.ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}

func importRetryTransition(
	before model.ImportWorkerSnapshot,
	code string,
	now, available int64,
) (model.ImportWorkerTransition, error) {
	change := importTransition(before, now)
	clearImportExecutionOwner(&change)
	change.Job.State = "QUEUED"
	change.Job.AvailableAtMS = available
	change.Job.FinishedAtMS = nil
	change.Job.ErrorCode = creationOptional(code)
	change.Job.Retryable = importTruth(true)
	change.Parent.State = "QUEUED"
	change.Parent.CompletedAtMS = nil
	change.Parent.ErrorCode = creationOptional(code)
	event, err := importWorkerEvent(before.Creation.Execution, "RETRY_SCHEDULED", now, map[string]any{
		"errorCode": code, "errorRetryable": true, "availableAtMs": available,
	})
	if err != nil {
		return model.ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}
