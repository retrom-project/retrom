package packs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/payloadrelease"
)

func (service *Service) List(ctx context.Context) (ListView, error) {
	definitions, err := service.listDefinitions(ctx)
	if err != nil {
		return ListView{}, err
	}
	installations, err := service.listInstallations(ctx)
	if err != nil {
		return ListView{}, err
	}
	return ListView{Definitions: definitions, Installations: installations}, nil
}

func (service *Service) listDefinitions(ctx context.Context) ([]DefinitionView, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT id,kind,generation,declared_name,normalized_declared_name,display_name,
 required_layout_version,origin,enabled
FROM runtime_asset_pack_definitions
ORDER BY CASE generation
 WHEN 'RPG2000' THEN 0 WHEN 'RPG2003' THEN 1 WHEN 'RPGXP' THEN 2
 WHEN 'RPGVX' THEN 3 ELSE 4 END,normalized_declared_name,id
`)
	if err != nil {
		return nil, fmt.Errorf("runtime pack definitions: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack definitions", rows.Close()) }()
	result := make([]DefinitionView, 0)
	for rows.Next() {
		var item DefinitionView
		if err := rows.Scan(
			&item.DefinitionID, &item.Kind, &item.Generation, &item.DeclaredName,
			&item.NormalizedDeclaredName, &item.DisplayName, &item.RequiredLayout,
			&item.Origin, &item.Enabled,
		); err != nil {
			return nil, fmt.Errorf("runtime pack definition: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtime pack definition rows: %w", err)
	}
	return result, nil
}

func (service *Service) listInstallations(ctx context.Context) ([]InstallationView, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT installation.id,installation.definition_id,installation.files_digest,
 installation.file_count,installation.total_bytes,installation.bundle_sha256,
 installation.status,installation.diagnostic_json,installation.source_note,
 installation.version,installation.created_at_ms,installation.validated_at_ms,
 installation.deleted_at_ms,
 (SELECT count(*) FROM game_variant_runtime_packs reference
  WHERE reference.installation_id=installation.id),
 (SELECT count(*) FROM save_states save
  JOIN launch_content_files locked ON locked.launch_session_id=save.source_launch_session_id
  WHERE locked.blob_id=installation.bundle_blob_id
    AND locked.logical_name LIKE '__retrom__/pack-%.zip' AND save.deleted_at_ms IS NULL)
FROM runtime_asset_pack_installations installation
JOIN runtime_asset_pack_definitions definition ON definition.id=installation.definition_id
ORDER BY CASE definition.generation
 WHEN 'RPG2000' THEN 0 WHEN 'RPG2003' THEN 1 WHEN 'RPGXP' THEN 2
 WHEN 'RPGVX' THEN 3 ELSE 4 END,definition.normalized_declared_name,
 installation.created_at_ms DESC,installation.id DESC
`)
	if err != nil {
		return nil, fmt.Errorf("runtime pack installations: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack installations", rows.Close()) }()
	result := make([]InstallationView, 0)
	for rows.Next() {
		item, err := scanInstallation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtime pack installation rows: %w", err)
	}
	return result, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanInstallation(row rowScanner) (InstallationView, error) {
	var item InstallationView
	var bundle, note sql.NullString
	var validated, deleted sql.NullInt64
	var diagnostics string
	err := row.Scan(
		&item.InstallationID, &item.DefinitionID, &item.FilesDigest, &item.FileCount,
		&item.TotalBytes, &bundle, &item.Status, &diagnostics, &note, &item.Version,
		&item.CreatedAtMS, &validated, &deleted, &item.References.GameCount,
		&item.References.CheckpointCount,
	)
	if err != nil {
		return InstallationView{}, fmt.Errorf("runtime pack installation projection: %w", err)
	}
	if jsonErr := json.Unmarshal([]byte(diagnostics), &item.Diagnostics); jsonErr != nil || item.Diagnostics == nil {
		return InstallationView{}, fmt.Errorf("runtime pack installation diagnostics: %w", jsonErr)
	}
	item.BundleSHA256 = nullableStringPointer(bundle)
	item.SourceNote = nullableStringPointer(note)
	item.ValidatedAtMS = nullableIntPointer(validated)
	item.DeletedAtMS = nullableIntPointer(deleted)
	return item, nil
}

func (service *Service) Delete(ctx context.Context, installationID string, expectedVersion int64) error {
	parsed, err := uuid.Parse(installationID)
	if err != nil || parsed.String() != installationID || expectedVersion < 1 {
		return ErrInvalid
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("runtime pack delete: %w", err)
	}
	defer cleanup.Rollback(transaction)
	if err := validateInstallationDeletion(
		ctx, transaction, installationID, expectedVersion,
	); err != nil {
		return err
	}
	references, err := installationReferences(ctx, transaction, installationID)
	if err != nil {
		return err
	}
	if references.GameCount != 0 || references.CheckpointCount != 0 {
		return ErrReferenced
	}
	blobIDs, err := installationBlobIDs(ctx, transaction, installationID)
	if err != nil {
		return err
	}
	if err := deleteInstallationRows(ctx, transaction, installationID, service.now().UnixMilli()); err != nil {
		return err
	}
	if err := service.scheduleDeletedPayload(ctx, transaction, installationID, blobIDs); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("runtime pack delete commit: %w", err)
	}
	if service.releases != nil {
		service.releases.Signal()
	}
	return nil
}

func validateInstallationDeletion(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	expectedVersion int64,
) error {
	var state string
	var version int64
	err := transaction.QueryRowContext(ctx, `
SELECT status,version FROM runtime_asset_pack_installations WHERE id=?
`, installationID).Scan(&state, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("runtime pack delete lookup: %w", err)
	}
	if version != expectedVersion {
		return ErrStale
	}
	if state != "READY" && state != "FAILED" {
		return ErrConflict
	}
	return nil
}

func installationReferences(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
) (ReferenceCounts, error) {
	var result ReferenceCounts
	err := transaction.QueryRowContext(ctx, `
SELECT
 (SELECT count(*) FROM game_variant_runtime_packs reference
  WHERE reference.installation_id=?),
 (SELECT count(*) FROM save_states save
  JOIN launch_content_files locked ON locked.launch_session_id=save.source_launch_session_id
  JOIN runtime_asset_pack_installations installation ON installation.id=?
  WHERE locked.blob_id=installation.bundle_blob_id
    AND locked.logical_name LIKE '__retrom__/pack-%.zip' AND save.deleted_at_ms IS NULL)
`, installationID, installationID).Scan(&result.GameCount, &result.CheckpointCount)
	if err != nil {
		return ReferenceCounts{}, fmt.Errorf("runtime pack references: %w", err)
	}
	return result, nil
}

func installationBlobIDs(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT bundle_blob_id FROM runtime_asset_pack_installations
WHERE id=? AND bundle_blob_id IS NOT NULL
UNION SELECT blob_id FROM runtime_asset_pack_files WHERE installation_id=?
`, installationID, installationID)
	if err != nil {
		return nil, fmt.Errorf("runtime pack payloads: %w", err)
	}
	defer func() { cleanup.Error("close runtime pack payloads", rows.Close()) }()
	var result []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("runtime pack payload: %w", err)
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runtime pack payload rows: %w", err)
	}
	return result, nil
}

func deleteInstallationRows(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	now int64,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE runtime_asset_pack_installations
SET status='DELETE_PENDING',version=version+1 WHERE id=?
`, installationID)
	if err != nil {
		return fmt.Errorf("runtime pack begin delete: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrConflict
	}
	if _, err := transaction.ExecContext(
		ctx, `DELETE FROM runtime_asset_pack_files WHERE installation_id=?`, installationID,
	); err != nil {
		return fmt.Errorf("runtime pack delete files: %w", err)
	}
	result, err = transaction.ExecContext(ctx, `
UPDATE runtime_asset_pack_installations
SET status='DELETED',bundle_blob_id=NULL,bundle_sha256=NULL,deleted_at_ms=?,version=version+1
WHERE id=? AND status='DELETE_PENDING'
`, now, installationID)
	if err != nil {
		return fmt.Errorf("runtime pack finish delete: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrConflict
	}
	return nil
}

func (service *Service) scheduleDeletedPayload(
	ctx context.Context,
	transaction *sql.Tx,
	installationID string,
	blobIDs []string,
) error {
	if service.releases == nil {
		return nil
	}
	if err := service.releases.StageCandidates(ctx, transaction, blobIDs); err != nil {
		return fmt.Errorf("runtime pack stage payload: %w", err)
	}
	var consumptionID string
	err := transaction.QueryRowContext(ctx, `
SELECT id FROM upload_consumptions
WHERE consumer_type='RUNTIME_ASSET_PACK_INSTALLATION' AND consumer_id=?
`, installationID).Scan(&consumptionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("runtime pack consumption: %w", err)
	}
	if _, err := payloadrelease.ScheduleConsumption(
		ctx, transaction, consumptionID, service.now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("runtime pack consumption release: %w", err)
	}
	return nil
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullableIntPointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}
