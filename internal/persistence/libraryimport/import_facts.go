package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	"retrom/internal/persistence/contentquery"
	libraryservice "retrom/internal/service/libraryimport"
)

type ImportFacts struct{ executor dbapi.Executor }

func BindImportFacts(executor dbapi.Executor) ImportFacts { return ImportFacts{executor: executor} }

func (records ImportFacts) Upload(ctx context.Context, id string) (libraryservice.ImportUpload, bool, error) {
	var result libraryservice.ImportUpload
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT id,purpose,source_type,state,version,manifest_digest,total_files
FROM upload_sessions WHERE id=?`, id).Scan(&result.ID, &result.Purpose, &result.SourceType,
		&result.State, &result.Version, &result.ManifestDigest, &result.FileCount)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ImportUpload{}, false, nil
	}
	if err != nil {
		return libraryservice.ImportUpload{}, false, fmt.Errorf("query import upload: %w", err)
	}
	return result, true, nil
}

func (records ImportFacts) Target(ctx context.Context, id string) (libraryservice.ImportTarget, bool, error) {
	var result libraryservice.ImportTarget
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT pi.id,pi.platform_id,pi.default_core_id,pi.version
FROM platform_instances pi WHERE pi.id=? AND pi.enabled=1 AND pi.deleted_at_ms IS NULL`, id).
		Scan(&result.ID, &result.PlatformID, &result.DefaultCoreID, &result.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return libraryservice.ImportTarget{}, false, nil
	}
	if err != nil {
		return libraryservice.ImportTarget{}, false, fmt.Errorf("query import platform instance: %w", err)
	}
	return result, true, nil
}

func (records ImportFacts) Files(ctx context.Context, uploadID string) ([]libraryservice.ImportFile, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT f.id,f.relative_path,f.file_record,json_extract(b.value, '$.sha256'),json_extract(b.value,
'$.size_bytes')
FROM import_files f JOIN json_each(json_array(f.file_record)) b ON b.value IS NOT NULL
WHERE f.upload_session_id=? AND f.released_at_ms IS NULL ORDER BY f.relative_path,f.id`, uploadID)
	if err != nil {
		return nil, fmt.Errorf("query import source files: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := []libraryservice.ImportFile{}
	for rows.Next() {
		var file libraryservice.ImportFile
		if err := rows.Scan(&file.ID, &file.Path, &file.FileRecord, &file.SHA256, &file.Size); err != nil {
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
	ctx context.Context, filter libraryservice.ImportBindingQuery,
) ([]libraryservice.ImportBinding, error) {
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
	result := []libraryservice.ImportBinding{}
	for rows.Next() {
		var binding libraryservice.ImportBinding
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
