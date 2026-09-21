package launch

import (
	"context"
	"fmt"

	"retrom/internal/persistence/recordstore"
	"retrom/internal/persistence/sessionstore"
	application "retrom/internal/service/launch"
)

func (records productCreationRecords) Create(ctx context.Context, plan application.ProductCreatePlan) error {
	if err := records.refreshApprovedBIOS(ctx, plan); err != nil {
		return err
	}
	source := plan.Source
	if _, err := sessionstore.CreateLaunch(ctx, records.transaction, `INSERT INTO launch_sessions(
 id,profile_id,game_id,core_id,provider_id,target_id,bundle_sha256,content_kind,dependency_snapshot_json,
 compatibility_code,save_state_id,dos_entry_path,initial_disc_index,return_to,credential_sha256,state,
 bootstrap_expires_at_ms,hard_expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'CREATED',?,?,?,?)`, plan.ID, plan.Command.ProfileID, source.GameID, source.CoreID,
		source.ProviderID, source.TargetID, source.BundleSHA256, source.ContentKind,
		source.DependencySnapshot, source.CompatibilityCode,
		plan.Command.Request.SaveStateID, plan.SelectedDOSEntry, plan.InitialDiscIndex,
		plan.Command.Request.ReturnTo, plan.CredentialHash,
		plan.BootstrapEnd, plan.HardEnd, plan.NowMS, plan.NowMS); err != nil {
		return fmt.Errorf("create product launch: %w", err)
	}
	if plan.Isolation != nil {
		if _, err := records.executor.ExecContext(
			ctx,
			`INSERT INTO isolated_runtime_bootstrap_tickets(
 ticket_sha256,launch_id,profile_id,expected_origin,expires_at_ms,consumed_at_ms)
VALUES(?,?,?,?,?,NULL)`,
			plan.Isolation.Hash[:],
			plan.ID,
			plan.Command.ProfileID,
			plan.Isolation.Origin,
			plan.NowMS+60_000,
		); err != nil {
			return fmt.Errorf("create product isolation ticket: %w", err)
		}
	}
	for _, file := range plan.Content.Files {
		if _, err := recordstore.CreateLaunchContentFiles(
			ctx,
			records.executor,
			`INSERT INTO launch_content_files(
launch_session_id,logical_name,blob_id,format_version,created_at_ms) VALUES(?,?,?,?,?)`,
			plan.ID,
			file.LogicalName,
			file.BlobID,
			file.Format,
			plan.NowMS,
		); err != nil {
			return fmt.Errorf("freeze product content: %w", err)
		}
	}
	for _, file := range plan.External {
		if _, err := recordstore.CreateLaunchExternalFiles(
			ctx,
			records.executor,
			`INSERT INTO launch_external_files(
launch_session_id,virtual_path,logical_name,blob_id,created_at_ms,kind) VALUES(?,?,?,?,?,?)`,
			plan.ID,
			file.VirtualPath,
			file.LogicalName,
			file.BlobID,
			plan.NowMS,
			file.Kind,
		); err != nil {
			return fmt.Errorf("freeze product external file: %w", err)
		}
	}
	return nil
}

func (records productCreationRecords) refreshApprovedBIOS(
	ctx context.Context,
	plan application.ProductCreatePlan,
) error {
	if plan.OverrideBIOS == nil {
		return nil
	}
	if _, err := recordstore.UpdateGameVariants(ctx, records.executor, recordstore.Update{
		Set: `dependency_snapshot_json=?,version=version+1,updated_at_ms=?`,
		Scope: recordstore.Scope{
			Where: `id=? AND dependency_snapshot_json<>?`,
			Args:  []any{plan.Source.VariantID, plan.Source.DependencySnapshot},
		},
		Values: []any{plan.Source.DependencySnapshot, plan.NowMS},
	}); err != nil {
		return fmt.Errorf("refresh approved product BIOS snapshot: %w", err)
	}
	for index, dependency := range plan.OverrideBIOS.BIOS {
		if dependency.DeliveryKind != "BIOS_BUNDLE" || dependency.BlobID == nil {
			continue
		}
		if _, err := recordstore.CreateVariantFiles(
			ctx,
			records.executor,
			`INSERT INTO variant_files(
 game_variant_id,role,logical_name,blob_id,sort_order) VALUES(?,'BIOS_BUNDLE',?,?,?)
ON CONFLICT(game_variant_id,role,logical_name) DO UPDATE SET blob_id=excluded.blob_id,sort_order=excluded.sort_order
WHERE variant_files.blob_id<>excluded.blob_id`,
			plan.Source.VariantID,
			dependency.LogicalName,
			*dependency.BlobID,
			index,
		); err != nil {
			return fmt.Errorf("refresh approved product BIOS files: %w", err)
		}
	}
	return nil
}
