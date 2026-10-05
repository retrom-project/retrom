package firmware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	dbapi "retrom/internal/database"
	"retrom/internal/service/firmware"
)

func (store requirementRecords) Get(ctx context.Context, id string) (firmware.Requirement, bool, error) {
	var value firmware.Requirement
	err := dbapi.QueryRowContext(ctx, store.executor, `SELECT q.id,q.source_kind,q.file_kind,q.logical_name,q.size_bytes,
 q.md5,q.sha1,q.sha256,q.version,q.enabled,q.provider_id,q.target_id,q.source_version,
q.catalog_digest,q.archive_members_json
FROM bios_requirements q JOIN runtime_targets target ON target.provider_id=q.provider_id
AND target.target_id=q.target_id
WHERE q.id=?`, id).
		Scan(&value.ID, &value.SourceKind, &value.FileKind, &value.LogicalName, &value.Size,
			&value.MD5, &value.SHA1, &value.SHA256, &value.Version, &value.Enabled, &value.ProviderID, &value.TargetID,
			&value.SourceVersion, &value.CatalogDigest, &value.ArchiveMembersJSON)
	return optionalBIOSRecord(value, err)
}

func (store uploadRecords) Get(ctx context.Context, id string) (firmware.Upload, bool, error) {
	var value firmware.Upload
	err := dbapi.QueryRowContext(ctx, store.executor, `SELECT f.id,f.upload_session_id,f.relative_path,f.state,
 b.value,(((b.value)::jsonb #>> '{size_bytes}'))::bigint,((b.value)::jsonb #>> '{md5}'),((b.value)::jsonb #>>
 '{sha1}'),((b.value)::jsonb #>> '{sha256}') FROM upload_files f JOIN
LATERAL (SELECT f.final_file_record AS value) b ON b.value IS NOT NULL
WHERE f.id=?`, id).
		Scan(
			&value.ID,
			&value.SessionID,
			&value.RelativePath,
			&value.State,
			&value.FileRecord,
			&value.Size,
			&value.MD5,
			&value.SHA1,
			&value.SHA256,
		)
	return optionalBIOSRecord(value, err)
}

func (store installationRecords) Active(
	ctx context.Context,
	id string,
) (firmware.ActiveInstallation, bool, error) {
	var value firmware.ActiveInstallation
	err := dbapi.QueryRowContext(ctx, store.executor, `SELECT id,file_record,original_filename,size_bytes,md5,sha1,sha256,
 status,validated_requirement_version FROM bios_installations WHERE requirement_id=? AND
is_active=1`, id).
		Scan(
			&value.ID,
			&value.FileRecord,
			&value.Filename,
			&value.Size,
			&value.MD5,
			&value.SHA1,
			&value.SHA256,
			&value.Status,
			&value.ValidatedVersion,
		)
	return optionalBIOSRecord(value, err)
}

func optionalBIOSRecord[T any](value T, err error) (T, bool, error) {
	if errors.Is(err, sql.ErrNoRows) {
		var zero T
		return zero, false, nil
	}
	if err != nil {
		var zero T
		return zero, false, fmt.Errorf("read BIOS record: %w", err)
	}
	return value, true, nil
}
