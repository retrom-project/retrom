package launch

import (
	"context"
	"database/sql"
	"fmt"

	application "retrom/internal/model/launch"
	"retrom/internal/repo/recordstore"
	"retrom/internal/repo/sessionstore"
)

func (records netplayCreationRecords) Create(ctx context.Context, plan application.NetplayCreationPlan) error {
	source, request := plan.Source, plan.Request
	result, err := sessionstore.CreateLaunch(ctx, records.transaction, `INSERT INTO launch_sessions(
 id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,dependency_snapshot_json,
 compatibility_code,save_state_id,dos_entry_path,initial_disc_index,return_to,credential_sha256,state,
 bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms,
 netplay_session_id,netplay_player_no,save_access)
VALUES(?,?,?,?,?,?,?,?,?,'',NULL,NULL,0,?,?,'CREATED',?,?,?,?,?,?,'NETPLAY_DISABLED')`,
		plan.ID, request.ProfileID, source.GameID, source.CoreID, source.ProviderID, source.TargetID, source.BundleSHA256,
		source.ContentKind, source.DependencySnapshot, request.ReturnTo, plan.CredentialHash,
		plan.BootstrapEnd, plan.HardEnd, plan.NowMS, plan.NowMS, request.SessionID, request.PlayerNo)
	if err := requireNetplayCreationChange(result, err); err != nil {
		return err
	}
	for _, file := range plan.Content.Files {
		if _, err := recordstore.CreateLaunchContentFiles(ctx, records.executor, `INSERT INTO launch_content_files(
launch_session_id,logical_name,blob_id,format_version,created_at_ms)VALUES(?,?,?,?,?)`,
			plan.ID, file.LogicalName, file.BlobID, file.Format, plan.NowMS); err != nil {
			return fmt.Errorf("freeze netplay content: %w", err)
		}
	}
	if err := NewProductExternals(records.executor).Store(ctx, plan.ID, plan.External, plan.NowMS); err != nil {
		return err
	}
	result, err = recordstore.UpdateNetplaySessionParticipants(ctx, records.executor, recordstore.Update{
		Set: `launch_session_id=?,credential_sha256=?,credential_generation=?,
state='LAUNCH_READY',version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `netplay_session_id=? AND profile_id=? AND player_no=? AND room_member_id=?
AND version=? AND state='LOCKED' AND credential_generation=0`,
			Args: []any{
				request.SessionID,
				request.ProfileID,
				request.PlayerNo,
				plan.Before.MemberID,
				plan.Before.ParticipantVersion,
			},
		},
		Values: []any{plan.ID, request.NetplayCredentialSHA256, request.CredentialGeneration, plan.NowMS},
	})
	return requireNetplayCreationChange(result, err)
}

func requireNetplayCreationChange(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write netplay creation: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count netplay creation: %w", err)
	}
	if count != 1 {
		return application.ErrBlocked
	}
	return nil
}
