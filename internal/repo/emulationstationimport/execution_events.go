package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	application "retrom/internal/model/emulationstationimport"
)

func (records executionRecords) finishEvent(ctx context.Context, change application.ExecutionFinish) error {
	event := change.JobState
	data := map[string]any{"schemaVersion": 1}
	switch change.JobState {
	case "QUEUED":
		event = "RETRY_SCHEDULED"
		data["executionNo"], data["attempt"] = change.Before.ExecutionNo, change.Before.Attempt
		data["retryAtMs"] = change.AvailableAtMS
		data["errorCode"], data["errorRetryable"] = change.Code, true
	case "FAILED":
		data["code"], data["retryable"] = change.Code, change.Retryable
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode EmulationStation execution event: %w", err)
	}
	result, err := records.executor.ExecContext(
		ctx,
		`INSERT INTO job_events
(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,?,?,?)`,
		change.Before.JobID,
		change.Before.ImportID,
		event,
		string(encoded),
		change.NowMS,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
