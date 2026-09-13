package libraryimport

import (
	"encoding/json"
	"fmt"
)

func importMoment(value int64) *int64 { return &value }
func importTruth(value bool) *bool    { return &value }
func importTransition(before ImportWorkerSnapshot, now int64) ImportWorkerTransition {
	return ImportWorkerTransition{
		Before: before,
		AtMS:   now,
		Job: ImportWorkerProjection{
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
		Parent: &ImportWorkerParent{
			State:               before.Creation.ImportState,
			CompletedAtMS:       before.ParentCompletedAtMS,
			CancelRequestedAtMS: before.ParentCancelRequestedAtMS,
			CancelReason:        before.ParentCancelReason,
			ErrorCode:           before.ParentErrorCode,
		},
	}
}

func importWorkerEvent(
	execution QueuedImportExecution,
	kind string,
	now int64,
	fields map[string]any,
) (CreationEvent, error) {
	fields["schemaVersion"], fields["executionNo"], fields["attempt"] = 1, execution.ExecutionNo, execution.Attempt
	encoded, err := json.Marshal(fields)
	if err != nil {
		return CreationEvent{}, fmt.Errorf("encode import worker event: %w", err)
	}
	return CreationEvent{
		JobID:     execution.JobID,
		ScopeType: "IMPORT_GROUP",
		ScopeID:   execution.ImportID,
		Kind:      kind,
		DataJSON:  string(encoded),
		NowMS:     now,
	}, nil
}

func clearImportExecutionOwner(change *ImportWorkerTransition) {
	change.Job.Execution.WorkerID = ""
	change.Job.LeaseUntilMS = nil
	change.Job.HeartbeatAtMS = nil
}

func importFailedTransition(
	before ImportWorkerSnapshot,
	code string,
	retryable bool,
	now int64,
) (ImportWorkerTransition, error) {
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
		return ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}

func importCancelledTransition(
	before ImportWorkerSnapshot,
	reason string,
	now int64,
) (ImportWorkerTransition, error) {
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
		return ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}

func importRetryTransition(
	before ImportWorkerSnapshot,
	code string,
	now, available int64,
) (ImportWorkerTransition, error) {
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
		return ImportWorkerTransition{}, err
	}
	change.Event = &event
	return change, nil
}
