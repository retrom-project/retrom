package gamecontent

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"

	"retrom/internal/model/gamecontent"
	"retrom/internal/repo/dbexec"
	payloadrepo "retrom/internal/repo/payloadrelease"
)

type retirementRecords struct{ executor dbexec.Executor }

func BindRetirement(executor dbexec.Executor) gamecontent.RetirementScope {
	records := retirementRecords{executor}
	return gamecontent.RetirementScope{
		Read: records, Write: records, GC: payloadrepo.BindGC(executor),
		Payload: payloadrepo.BindScheduling(executor),
	}
}

func (records retirementRecords) Blobs(ctx context.Context, gameID string) ([]string, error) {
	rows, err := records.executor.QueryContext(ctx, `SELECT file.blob_id FROM game_files file WHERE file.game_id=?
UNION ALL
SELECT file.source_archive_blob_id FROM game_files file WHERE file.game_id=?
UNION ALL
SELECT file.blob_id FROM variant_files file
JOIN game_variants variant ON variant.id=file.game_variant_id WHERE variant.game_id=?
UNION ALL
SELECT binding.restore_payload_blob_id FROM launch_game_save_bindings binding
JOIN launch_sessions launch ON launch.id=binding.launch_session_id WHERE launch.game_id=?
UNION ALL
SELECT save.payload_blob_id FROM save_states save WHERE save.game_id=?
UNION ALL
SELECT save.screenshot_blob_id FROM save_states save WHERE save.game_id=?
UNION ALL
SELECT file.blob_id FROM launch_content_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
UNION ALL
SELECT file.blob_id FROM launch_external_files file
JOIN launch_sessions launch ON launch.id=file.launch_session_id WHERE launch.game_id=?
`, gameID, gameID, gameID, gameID, gameID, gameID, gameID, gameID)
	if err != nil {
		return nil, fmt.Errorf("query replacement blobs: %w", err)
	}
	defer func() { cleanup.Error("close retirement rows", rows.Close()) }()
	var result []string
	for rows.Next() {
		var id sql.NullString
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan replacement blob: %w", err)
		}
		if id.Valid {
			result = append(result, id.String)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replacement blobs: %w", err)
	}
	return result, nil
}

func (records retirementRecords) Owners(ctx context.Context, gameID string) ([]gamecontent.RetirementOwner, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT 'NETPLAY',id,state,version,NULL,finished_at_ms FROM netplay_sessions WHERE game_id=?
UNION ALL SELECT 'ROOM',id,state,version,NULL,ended_at_ms FROM netplay_rooms WHERE selected_game_id=?
UNION ALL SELECT 'LAUNCH',id,state,version,save_state_id,finished_at_ms FROM launch_sessions WHERE game_id=?
UNION ALL SELECT 'PLAY',id,state,version,NULL,ended_at_ms FROM play_sessions WHERE game_id=?
UNION ALL SELECT 'VARIANT',id,status,version,NULL,NULL FROM game_variants WHERE game_id=?
`, gameID, gameID, gameID, gameID, gameID)
	if err != nil {
		return nil, fmt.Errorf("query replacement runtime: %w", err)
	}
	defer func() { cleanup.Error("close retirement rows", rows.Close()) }()
	var result []gamecontent.RetirementOwner
	for rows.Next() {
		var owner gamecontent.RetirementOwner
		if err := rows.Scan(
			&owner.Kind, &owner.ID, &owner.State, &owner.Version, &owner.SaveID, &owner.FinishedAt,
		); err != nil {
			return nil, fmt.Errorf("scan replacement runtime: %w", err)
		}
		result = append(result, owner)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate replacement runtime: %w", err)
	}
	return result, nil
}

func (records retirementRecords) Consumption(ctx context.Context, jobID string) (string, error) {
	var id string
	err := records.executor.QueryRowContext(ctx, `SELECT id FROM upload_consumptions
WHERE consumer_type='GAME_CONTENT_REPLACE_JOB' AND consumer_id=?`, jobID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("query replacement consumption: %w", err)
	}
	return id, nil
}
