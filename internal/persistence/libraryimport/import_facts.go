package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/dbexec"
	"retrom/internal/persistence/contentquery"
	application "retrom/internal/service/libraryimport"
)

type ImportFacts struct{ executor dbexec.Executor }

func BindImportFacts(executor dbexec.Executor) ImportFacts { return ImportFacts{executor: executor} }

func (records ImportFacts) Upload(ctx context.Context, id string) (application.ImportUpload, bool, error) {
	var result application.ImportUpload
	err := records.executor.QueryRowContext(ctx, `
SELECT id,purpose,source_type,state,version,manifest_digest,total_files
FROM upload_sessions WHERE id=?`, id).Scan(&result.ID, &result.Purpose, &result.SourceType,
		&result.State, &result.Version, &result.ManifestDigest, &result.FileCount)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ImportUpload{}, false, nil
	}
	if err != nil {
		return application.ImportUpload{}, false, fmt.Errorf("query import upload: %w", err)
	}
	return result, true, nil
}

func (records ImportFacts) Target(ctx context.Context, id string) (application.ImportTarget, bool, error) {
	var result application.ImportTarget
	err := records.executor.QueryRowContext(ctx, `
SELECT pi.id,pi.platform_id,pi.default_core_id,pi.version
FROM platform_instances pi WHERE pi.id=? AND pi.enabled=1 AND pi.deleted_at_ms IS NULL`, id).
		Scan(&result.ID, &result.PlatformID, &result.DefaultCoreID, &result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return application.ImportTarget{}, false, nil
	}
	if err != nil {
		return application.ImportTarget{}, false, fmt.Errorf("query import platform instance: %w", err)
	}
	return result, true, nil
}

func (records ImportFacts) Files(ctx context.Context, uploadID string) ([]application.ImportFile, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT f.id,f.relative_path,f.blob_id,b.sha256,b.size_bytes
FROM import_files f JOIN blobs b ON b.id=f.blob_id
WHERE f.upload_session_id=? AND f.released_at_ms IS NULL ORDER BY f.relative_path,f.id`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("query import source files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []application.ImportFile{}
	for rows.Next() {
		var file application.ImportFile
		if err := rows.Scan(&file.ID, &file.Path, &file.BlobID, &file.SHA256, &file.Size); err != nil {
			return nil, fmt.Errorf("scan import source file: %w", err)
		}
		result = append(result, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import source files: %w", err)
	}
	return result, nil
}

func (records ImportFacts) Bindings(
	ctx context.Context, filter application.ImportBindingQuery,
) ([]application.ImportBinding, error) {
	query := `
SELECT binding.binding_id,binding.core_id,binding.provider_id,binding.target_id,
 binding.delivery_profile,binding.detector_profile,` + contentquery.BindingPolicySQL + `
FROM runtime_target_bindings binding
JOIN runtime_binding_platforms platform ON platform.binding_id=binding.binding_id AND platform.platform_id=?
JOIN runtime_targets target ON target.provider_id=binding.provider_id AND target.target_id=binding.target_id
WHERE binding.core_id=? AND binding.launch_policy!='DISABLED'`
	arguments := []any{filter.PlatformID, filter.CoreID}
	if filter.DetectorProfile != "" {
		query += ` AND binding.detector_profile=?`
		arguments = append(arguments, filter.DetectorProfile)
	}
	query += ` ORDER BY binding.detector_profile,binding.provider_id,binding.target_id,binding.binding_id`
	rows, err := records.executor.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query import runtime bindings: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []application.ImportBinding{}
	for rows.Next() {
		var binding application.ImportBinding
		var profile sql.NullString
		if err := rows.Scan(&binding.BindingID, &binding.CoreID, &binding.ProviderID, &binding.TargetID,
			&binding.DeliveryProfile, &profile, contentquery.ScanPolicy(&binding.Policy)); err != nil {
			return nil, fmt.Errorf("scan import runtime binding: %w", err)
		}
		binding.DetectorProfile = profile.String
		result = append(result, binding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import runtime bindings: %w", err)
	}
	return result, nil
}
