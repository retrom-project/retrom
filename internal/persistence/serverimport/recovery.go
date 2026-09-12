package serverimport

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/service/serverimport"
)

type Recovery struct{ database *sql.DB }

func NewRecovery(database *sql.DB) *Recovery { return &Recovery{database} }
func (repository *Recovery) Items(ctx context.Context, importID string) ([]serverimport.CatalogItem, error) {
	rows, err := repository.database.QueryContext(ctx, `
SELECT requirement_id,requirement_version,core_id,core_name_snapshot,provider_id,target_id,
source_kind,archive_members_json,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path,
catalog_digest,
	activation_options_json,source_version,dat_version_id,dat_machine_name,expected_size_bytes,
	expected_md5,expected_sha1,expected_sha256,
active_installation_id_snapshot,active_installation_version_snapshot,active_blob_sha256_snapshot,
active_status_snapshot,active_validated_requirement_version_snapshot,state
FROM server_bios_import_items WHERE server_import_id=? ORDER BY requirement_id COLLATE BINARY`, importID)
	if err != nil {
		return nil, fmt.Errorf("query server import items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]serverimport.CatalogItem, 0)
	for rows.Next() {
		var item serverimport.CatalogItem
		if err := rows.Scan(
			&item.RequirementID, &item.RequirementVersion, &item.CoreID, &item.CoreName,
			&item.ProviderID, &item.TargetID, &item.SourceKind, &item.ArchiveMembersJSON, &item.LogicalName,
			&item.RequirementMode, &item.ConditionCode, &item.DeliveryKind, &item.EmulatorPath,
			&item.CatalogDigest, &item.ActivationOptionsJSON, &item.SourceVersion, &item.DATVersionID,
			&item.DATMachineName, &item.ExpectedSize, &item.ExpectedMD5, &item.ExpectedSHA1,
			&item.ExpectedSHA256, &item.ActiveInstallationID, &item.ActiveInstallationVersion,
			&item.ActiveBlobSHA256, &item.ActiveStatus, &item.ActiveValidatedVersion, &item.State,
		); err != nil {
			return nil, fmt.Errorf("scan server import item: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server import items: %w", err)
	}
	return result, nil
}

func (repository *Recovery) Phase(ctx context.Context, importID string) (string, error) {
	var phase sql.NullString
	if err := repository.database.QueryRowContext(ctx, `
SELECT phase FROM server_imports WHERE id=?
`, importID).Scan(&phase); err != nil {
		return "", fmt.Errorf("read server import discovery phase: %w", err)
	}
	return phase.String, nil
}
