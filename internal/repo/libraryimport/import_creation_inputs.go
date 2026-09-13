package libraryimport

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"retrom/internal/repo/contentquery"
	application "retrom/internal/service/libraryimport"
)

func (records creationRecords) Queued(
	ctx context.Context,
	id string,
) (application.CreationQueuedSnapshot, error) {
	result, found, err := (importExecutionRecords{executor: records.transaction}).Current(ctx, id)
	if err != nil {
		return application.CreationQueuedSnapshot{}, fmt.Errorf("read queued creation authority: %w", err)
	}
	if !found {
		return application.CreationQueuedSnapshot{}, fmt.Errorf("read queued creation authority: %w", sql.ErrNoRows)
	}
	return result.Creation, nil
}

func (records creationRecords) FenceInputs(ctx context.Context, plan application.PreparedImport) error {
	upload := plan.Upload
	result, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE upload_sessions SET version=version
WHERE id=? AND state='COMPLETE' AND version=? AND manifest_digest=? AND total_files=? AND
 source_type=? AND purpose=?`,
		upload.ID,
		upload.Version,
		upload.ManifestDigest,
		upload.FileCount,
		upload.SourceType,
		upload.Purpose,
	)
	if err := creationMutation(result, err, "fence prepared upload", 1); err != nil {
		return err
	}
	target := plan.Target
	result, err = records.transaction.ExecContext(ctx, `
UPDATE platform_instances SET version=version
WHERE id=? AND version=? AND platform_id=? AND default_core_id=? AND enabled=1 AND deleted_at_ms IS NULL`,
		target.ID, target.Version, target.PlatformID, target.DefaultCoreID)
	if err := creationMutation(result, err, "fence prepared platform", 1); err != nil {
		return err
	}
	if err := records.fenceBinding(ctx, plan); err != nil {
		return err
	}
	for _, file := range plan.Files {
		result, err = records.transaction.ExecContext(
			ctx,
			`
UPDATE upload_files SET state=state WHERE id=? AND upload_session_id=? AND state='COMPLETE'
AND relative_path=? AND final_blob_id=? AND EXISTS(SELECT 1 FROM blobs WHERE id=? AND sha256=? AND
 size_bytes=?)`,
			file.ID,
			upload.ID,
			file.Path,
			file.BlobID,
			file.BlobID,
			file.SHA256,
			file.Size,
		)
		if err := creationMutation(result, err, "fence prepared file", 1); err != nil {
			return err
		}
	}
	return nil
}

func (records creationRecords) fenceBinding(ctx context.Context, plan application.PreparedImport) error {
	target := plan.Target
	result, err := records.transaction.ExecContext(
		ctx,
		`
UPDATE runtime_target_bindings AS binding SET binding_id=binding_id
WHERE binding_id=? AND core_id=? AND provider_id=? AND target_id=? AND delivery_profile=? AND
 launch_policy!='DISABLED'
AND EXISTS(SELECT 1 FROM runtime_binding_platforms WHERE binding_id=binding.binding_id AND platform_id=?)
AND `+contentquery.BindingPolicySQL+`=?`,
		target.BindingID,
		target.CoreID,
		target.ProviderID,
		target.TargetID,
		target.DeliveryProfile,
		target.PlatformID,
		strings.Join(target.Policy.SupportedContentKinds, ","),
	)
	if err := creationMutation(result, err, "fence prepared binding", 1); err != nil {
		return err
	}
	var active string
	err = records.transaction.QueryRowContext(ctx, `
SELECT COALESCE((SELECT id FROM dat_versions WHERE provider_id=? AND target_id=? AND is_active=1),'')`,
		target.ProviderID, target.TargetID).Scan(&active)
	if err != nil {
		return fmt.Errorf("read prepared DAT fence: %w", err)
	}
	if active != plan.DATVersionID {
		return application.ErrVersionConflict
	}
	return nil
}
