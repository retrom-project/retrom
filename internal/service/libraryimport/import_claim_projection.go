package libraryimport

func importClaimProjection(
	before ImportWorkerSnapshot,
	work ImportWork,
	workerID string,
	now int64,
) (ImportWork, ImportWorkerTransition, error) {
	work.Execution.WorkerID = workerID
	work.Execution.Attempt++
	if before.StartedAtMS == nil {
		work.Execution.StartedAtMS = now
	}
	if before.DeadlineAtMS == nil {
		work.Execution.DeadlineMS = now + ImportExecutionBudget.Milliseconds()
	}
	change := importTransition(before, now)
	change.Job.Execution = work.Execution
	change.Job.StartedAtMS = importMoment(work.Execution.StartedAtMS)
	change.Job.DeadlineAtMS = importMoment(work.Execution.DeadlineMS)
	change.Job.State = "RUNNING"
	change.Job.LeaseUntilMS = importMoment(min(now+ImportExecutionLease.Milliseconds(), work.Execution.DeadlineMS))
	change.Job.HeartbeatAtMS = importMoment(now)
	change.Job.FinishedAtMS = nil
	change.Job.ErrorCode = nil
	change.Job.Retryable = nil
	change.Parent.State = "RUNNING"
	change.Parent.CompletedAtMS = nil
	change.Parent.ErrorCode = nil
	event, err := importWorkerEvent(work.Execution, "STARTED", now,
		map[string]any{"state": "RUNNING", "phase": "INSPECTING"})
	if err != nil {
		return ImportWork{}, ImportWorkerTransition{}, err
	}
	change.Event = &event
	return work, change, nil
}
