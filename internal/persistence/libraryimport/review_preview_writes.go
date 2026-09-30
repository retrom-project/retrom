package libraryimport

import (
	"context"
	"crypto/sha256"
	"fmt"

	"retrom/internal/persistence/recordstore"
	application "retrom/internal/service/launch"
)

func (records reviewPreviewRecords) Create(ctx context.Context, plan application.PreviewCreatePlan) error {
	source, request, content := plan.Source, plan.Request, plan.Content
	var emulatorGameID *int64
	if source.DeliveryProfile == "EMULATORJS_CONTENT" || source.DeliveryProfile == "ROM_BLOB" {
		id := max(plan.NowMS, 1)
		emulatorGameID = &id
	}
	_, err := recordstore.CreateRuntimePreviewSessions(ctx, records.executor, `
INSERT INTO runtime_preview_sessions(id,scope_id,content_revision,return_to,
 target_platform_instance_id,provider_id,target_id,bundle_sha256,actor_user_id,idempotency_key,title,
content_kind,
 content_file_record,content_logical_name,content_format,dependency_snapshot_json,default_dos_entry,
 restore_from_preview_id,restore_payload_file_record,restore_checkpoint_format,
 emulator_game_id,credential_sha256,state,bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,
updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'CREATED',?,?,?,?)`,
		plan.ID, previewScope(request.ImportItemID), previewScope(source.SourceSnapshotID),
		"/admin/reviews/"+request.ImportItemID, source.PlatformInstanceID,
		source.ProviderID, source.TargetID, source.BundleSHA256, request.ActorUserID, request.IdempotencyKey,
		source.Title, source.ContentKind, content.FileRecord, content.LogicalName, content.Format, source.DependencySnapshot,
		source.DefaultDOSEntry, request.RestoreFromPreviewID, plan.RestoreFileRecord, plan.RestoreFormat, emulatorGameID,
		plan.CredentialHash, plan.BootstrapEnd, plan.HardEnd, plan.NowMS, plan.NowMS)
	if err != nil {
		return fmt.Errorf("insert preview session: %w", err)
	}
	if _, err := records.executor.ExecContext(ctx, `INSERT INTO review_preview_bindings
(preview_session_id,import_item_id,source_snapshot_id) VALUES(?,?,?)`,
		plan.ID, request.ImportItemID, source.SourceSnapshotID); err != nil {
		return fmt.Errorf("bind review preview: %w", err)
	}
	for _, file := range content.Files {
		if err := validateReviewPreviewFile(ctx, records.executor, plan, file); err != nil {
			return err
		}
		_, err := recordstore.CreateRuntimePreviewFiles(ctx, records.executor, `
INSERT INTO runtime_preview_files(preview_session_id,role,logical_name,virtual_path,file_record,
sort_order,created_at_ms)
VALUES(?,?,?,?,?,?,?)
`, plan.ID, file.Role, file.LogicalName, file.VirtualPath, file.FileRecord, file.SortOrder, plan.NowMS)
		if err != nil {
			return fmt.Errorf("insert preview file: %w", err)
		}
	}
	if plan.Isolation != nil {
		return records.isolate(ctx, plan)
	}
	return nil
}

func previewScope(id string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(id))) }

func (records reviewPreviewRecords) isolate(ctx context.Context, plan application.PreviewCreatePlan) error {
	_, err := records.executor.ExecContext(ctx, `
INSERT INTO isolated_runtime_bootstrap_tickets(
 ticket_sha256,preview_id,profile_id,expected_origin,expires_at_ms,consumed_at_ms)
VALUES(?,?,?,?,?,NULL)`, plan.Isolation.Hash[:], plan.ID, plan.ProfileID, plan.Isolation.Origin, plan.NowMS+60_000)
	if err != nil {
		return fmt.Errorf("insert preview isolation: %w", err)
	}
	return nil
}
