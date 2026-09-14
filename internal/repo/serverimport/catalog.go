package serverimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/foundation/cleanup"
	"retrom/internal/model/serverimport"
	"retrom/internal/repo/dbexec"
	"retrom/internal/repo/recordstore"
)

func (repository *Creation) Catalog(ctx context.Context) ([]serverimport.CatalogEntry, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT requirement.id,requirement.version,requirement.core_id,core.name,requirement.provider_id,
requirement.target_id,requirement.source_kind,requirement.archive_members_json,
requirement.logical_name,requirement.requirement_mode,
requirement.condition_code,requirement.activation_options_json,requirement.delivery_kind,requirement.emulator_path,
requirement.source_version,requirement.catalog_digest,
CASE WHEN requirement.source_kind='DAT_MACHINE' THEN dat.id END,requirement.dat_machine_name,
requirement.size_bytes,requirement.md5,requirement.sha1,requirement.sha256,
installation.id,installation.version,blob.sha256,installation.status,installation.validated_requirement_version,
dat.parse_status,dat.is_active
FROM bios_requirements requirement
JOIN cores core ON core.id=requirement.core_id
JOIN runtime_targets target ON target.provider_id=requirement.provider_id AND target.target_id=requirement.target_id
LEFT JOIN dat_versions dat ON dat.id=requirement.source_version AND dat.provider_id=requirement.provider_id
 AND dat.target_id=requirement.target_id
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
 AND installation.is_active=1
LEFT JOIN blobs blob ON blob.id=installation.blob_id
WHERE requirement.enabled=1
ORDER BY requirement.id COLLATE BINARY
`)
	if err != nil {
		return nil, fmt.Errorf("serverimport/query catalog: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	items := make([]serverimport.CatalogEntry, 0)
	for rows.Next() {
		var item serverimport.CatalogItem
		var datStatus sql.NullString
		var datActive sql.NullInt64
		if err := rows.Scan(
			&item.RequirementID, &item.RequirementVersion, &item.CoreID, &item.CoreName, &item.ProviderID,
			&item.TargetID, &item.SourceKind, &item.ArchiveMembersJSON, &item.LogicalName, &item.RequirementMode,
			&item.ConditionCode, &item.ActivationOptionsJSON, &item.DeliveryKind, &item.EmulatorPath,
			&item.SourceVersion, &item.CatalogDigest, &item.DATVersionID,
			&item.DATMachineName, &item.ExpectedSize, &item.ExpectedMD5, &item.ExpectedSHA1, &item.ExpectedSHA256,
			&item.ActiveInstallationID, &item.ActiveInstallationVersion, &item.ActiveBlobSHA256, &item.ActiveStatus,
			&item.ActiveValidatedVersion, &datStatus, &datActive,
		); err != nil {
			return nil, fmt.Errorf("serverimport/scan catalog item: %w", err)
		}
		items = append(
			items,
			serverimport.CatalogEntry{
				Item:     item,
				DATReady: datStatus.Valid && datStatus.String == "READY" && datActive.Valid && datActive.Int64 == 1,
			},
		)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("serverimport/iterate catalog: %w", err)
	}
	return items, nil
}

func insertCatalogItem(
	ctx context.Context,
	transaction dbexec.Executor,
	importID string,
	item serverimport.CatalogItem,
	now int64,
) error {
	_, err := recordstore.CreateServerBiosImportItems(
		ctx, transaction,
		`
INSERT INTO server_bios_import_items(
server_import_id,requirement_id,requirement_version,core_id,core_name_snapshot,provider_id,target_id,
source_kind,archive_members_json,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path,
activation_options_json,source_version,catalog_digest,dat_version_id,dat_machine_name,expected_size_bytes,
expected_md5,expected_sha1,expected_sha256,
active_installation_id_snapshot,active_installation_version_snapshot,active_blob_sha256_snapshot,
active_status_snapshot,active_validated_requirement_version_snapshot,state,created_at_ms,updated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'PENDING',?,?)
`,
		importID,
		item.RequirementID,
		item.RequirementVersion,
		item.CoreID,
		item.CoreName,
		item.ProviderID,
		item.TargetID,
		item.SourceKind,
		item.ArchiveMembersJSON,
		item.LogicalName,
		item.RequirementMode,
		item.ConditionCode,
		item.DeliveryKind,
		item.EmulatorPath,
		item.ActivationOptionsJSON,
		item.SourceVersion,
		item.CatalogDigest,
		item.DATVersionID,
		item.DATMachineName,
		item.ExpectedSize,
		item.ExpectedMD5,
		item.ExpectedSHA1,
		item.ExpectedSHA256,
		item.ActiveInstallationID,
		item.ActiveInstallationVersion,
		item.ActiveBlobSHA256,
		item.ActiveStatus,
		item.ActiveValidatedVersion,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("serverimport/catalog item: %w", err)
	}
	return nil
}
