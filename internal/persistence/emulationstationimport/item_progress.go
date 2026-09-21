package emulationstationimport

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/dbexec"
	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/emulationstationimport"
)

func refreshItemProgress(
	ctx context.Context,
	executor dbexec.Executor,
	before application.LeaseSnapshot,
	id, outcome string,
	now int64,
) error {
	counts, err := LoadTerminalItemCounts(ctx, executor, before.ImportID)
	if err != nil {
		return err
	}
	values := terminalCountValues(counts)
	values = append(values, before.ImportID, now)
	result, err := recordstore.UpdateEmulationstationImports(ctx, executor, recordstore.Update{
		Set: `skipped_mapping_item_count=?,review_pending_item_count=?,published_item_count=?,review_discarded_item_count=?,
existing_item_count=?,blocked_item_count=?,failed_item_count=?,cancelled_item_count=?,
media_warning_count=(SELECT count(*) FROM emulationstation_import_items item,json_each(item.warnings_json)
warning
WHERE item.import_id=? AND json_extract(warning.value,'$.pathKind') IN
('COVER','VIDEO')),version=version+1,updated_at_ms=?`,
		Values: values, Scope: recordstore.Scope{
			Where: `id=? AND version=? AND state=?`,
			Args:  []any{before.ImportID, before.ImportVersion, before.ImportState},
		},
	})
	if err := requireWorkflowChange(result, err, application.ErrVersionConflict); err != nil {
		return err
	}
	encoded, err := json.Marshal(map[string]any{"schemaVersion": 1, "itemId": id, "outcome": outcome})
	if err != nil {
		return fmt.Errorf("encode EmulationStation item progress: %w", err)
	}
	result, err = executor.ExecContext(
		ctx,
		`INSERT INTO job_events(job_id,scope_type,scope_id,event_type,data_json,created_at_ms)
VALUES(?,'EMULATIONSTATION_IMPORT',?,'PROGRESS',?,?)`,
		before.JobID,
		before.ImportID,
		string(encoded),
		now,
	)
	return requireWorkflowChange(result, err, application.ErrVersionConflict)
}
