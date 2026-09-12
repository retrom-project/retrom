package recordstore

import (
	"context"
	"database/sql"
)

func UpdateSaveStates(
	ctx context.Context, db DBTX, change Update,
) (sql.Result, error) {
	return updateRecords(
		ctx,
		db,
		change,
		"save_states",
		"id,checkpoint_format,disc_index,source_launch_session_id",
		SaveStatesUpdateRule,
	)
}

const SaveStatesUpdateRule = `
WITH previous(id,checkpoint_format,disc_index,source_launch_session_id) AS (VALUES(?,?,?,?))
SELECT CASE
-- save_states_disc_update
WHEN ((candidate.source_launch_session_id IS NOT previous.source_launch_session_id OR
candidate.disc_index IS NOT previous.disc_index) AND ((
  EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=candidate.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND (
    candidate.disc_index IS NULL OR candidate.disc_index >= (
      SELECT count(*) FROM launch_external_files external
      WHERE external.launch_session_id=candidate.source_launch_session_id AND external.kind='DISC'
    )
  )
  OR NOT EXISTS(
    SELECT 1 FROM launch_content_files content
    WHERE content.launch_session_id=candidate.source_launch_session_id
    AND content.format_version='RETROM_MULTIDISC_M3U_V1'
  ) AND candidate.disc_index IS NOT NULL
))) THEN 'save state disc index mismatch'
-- save_states_runtime_target_immutable
WHEN ((candidate.checkpoint_format IS NOT previous.checkpoint_format) AND (1=1)) THEN
'immutable runtime checkpoint snapshot'
-- save_states_source_launch_immutable
WHEN ((candidate.source_launch_session_id IS NOT previous.source_launch_session_id) AND
(previous.source_launch_session_id IS NOT candidate.source_launch_session_id)) THEN
'save state source launch is immutable'
ELSE '' END
FROM save_states candidate CROSS JOIN previous
WHERE candidate.id=previous.id`
