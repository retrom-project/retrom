package emulationstationimport

import (
	"math"

	model "retrom/internal/model/emulationstationimport"
)

func planRecovery(before model.LeaseSnapshot, now int64) (model.RecoveryChange, error) {
	if !validRecoveryBudget(before) {
		return model.RecoveryChange{}, model.ErrInvalid
	}
	if before.LeaseUntilMS > now {
		return model.RecoveryChange{}, model.ErrVersionConflict
	}
	change := model.RecoveryChange{
		Before:      before,
		JobState:    "QUEUED",
		ImportState: "QUEUED",
		Event:       "RETRY_SCHEDULED",
		NowMS:       now,
	}
	if before.Kind == "SERVER_EMULATIONSTATION_SCAN" {
		change.ImportState, change.Phase = "SCANNING", "DISCOVERING_GAMELISTS"
	} else if before.Kind != "SERVER_EMULATIONSTATION_IMPORT" {
		return model.RecoveryChange{}, model.ErrInvalid
	}
	if before.JobState == "CANCEL_REQUESTED" && before.ImportState == "CANCEL_REQUESTED" {
		change.JobState, change.ImportState, change.Phase = "CANCELLED", "CANCELLED", ""
		change.ItemState, change.Event = "CANCELLED", "CANCELLED"
		planRecoveryProjection(&change)
		return change, nil
	}
	if !recoverableState(before) {
		return model.RecoveryChange{}, model.ErrVersionConflict
	}
	delay := RecoveryDelayMS(before.Attempt)
	if before.JobState == "QUEUED" {
		delay = 0
	}
	if now > math.MaxInt64-delay || before.DeadlineAtMS <= now+delay {
		change.Code = "EMULATIONSTATION_EXECUTION_TIMEOUT"
	} else if before.Attempt >= before.MaxAttempts {
		change.Code = "EMULATIONSTATION_WORKER_ATTEMPTS_EXHAUSTED"
	}
	if change.Code != "" {
		change.JobState, change.ImportState, change.Phase = "FAILED", "FAILED", ""
		change.ItemState, change.Event = "COMMIT_FAILED", "FAILED"
		planRecoveryProjection(&change)
		return change, nil
	}
	if before.JobState == "QUEUED" {
		return model.RecoveryChange{}, model.ErrVersionConflict
	}
	change.AvailableAtMS = now + delay
	planRecoveryProjection(&change)
	return change, nil
}

func validRecoveryBudget(before model.LeaseSnapshot) bool {
	return before.JobVersion > 0 && before.JobVersion < math.MaxInt64 &&
		before.ImportVersion > 0 && before.ImportVersion < math.MaxInt64 &&
		before.ExecutionNo > 0 && before.Attempt > 0 && before.MaxAttempts > 0 &&
		before.StartedAtMS != nil && before.DeadlineAtMS > 0 && recoveryLeaseShape(before)
}

func recoveryLeaseShape(before model.LeaseSnapshot) bool {
	if before.JobState == "QUEUED" {
		return before.WorkerID == "" && before.LeaseUntilMS == 0
	}
	return before.WorkerID != "" && before.LeaseUntilMS > 0
}

func recoverableState(before model.LeaseSnapshot) bool {
	if before.JobState != "RUNNING" && before.JobState != "QUEUED" {
		return false
	}
	if before.Kind == "SERVER_EMULATIONSTATION_SCAN" {
		return before.ImportState == "SCANNING"
	}
	if before.JobState == "QUEUED" {
		return before.ImportState == "QUEUED"
	}
	return before.ImportState == "RUNNING"
}

func RecoveryDelayMS(attempt int64) int64 {
	switch attempt {
	case 1:
		return 1000
	case 2:
		return 5000
	case 3:
		return 30000
	default:
		return 120000
	}
}

func planRecoveryProjection(change *model.RecoveryChange) {
	change.ClearScan = change.Before.Kind == "SERVER_EMULATIONSTATION_SCAN"
	change.TerminalItems = !change.ClearScan && change.JobState != "QUEUED"
	change.SchedulePayload = change.TerminalItems
}
