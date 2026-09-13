package corevalidation

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/foundation/cleanup"
	"retrom/internal/repo/dbexec"
	service "retrom/internal/service/corevalidation"
)

type Repository struct{ executor dbexec.Executor }

func New(executor dbexec.Executor) *Repository { return &Repository{executor: executor} }
func (repository *Repository) Catalog(
	ctx context.Context,
	providerID, targetID string,
) ([]corevalidation.BIOSCatalogEntry, error) {
	rows, err := repository.executor.QueryContext(ctx, `
SELECT id,version,catalog_digest,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path
FROM bios_requirements
WHERE provider_id=? AND target_id=? AND source_kind='STATIC' AND enabled=1
ORDER BY logical_name,id
`, providerID, targetID)
	if err != nil {
		return nil, fmt.Errorf("corevalidation/catalog: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	entries := make([]corevalidation.BIOSCatalogEntry, 0)
	for rows.Next() {
		var entry corevalidation.BIOSCatalogEntry
		var condition, emulatorPath sql.NullString
		if err := rows.Scan(
			&entry.RequirementID,
			&entry.RequirementVersion,
			&entry.CatalogDigest,
			&entry.LogicalName,
			&entry.RequirementMode,
			&condition,
			&entry.DeliveryKind,
			&emulatorPath,
		); err != nil {
			return nil, fmt.Errorf("corevalidation/catalog: %w", err)
		}
		entry.ConditionCode = nullableString(condition)
		entry.EmulatorPath = nullableString(emulatorPath)
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("corevalidation/catalog: %w", err)
	}
	return entries, nil
}

func (repository *Repository) BIOS(ctx context.Context, providerID, targetID string) ([]service.BIOSRecord, error) {
	rows, err := repository.executor.QueryContext(ctx, `
SELECT q.id,q.version,q.catalog_digest,q.logical_name,q.requirement_mode,q.condition_code,
       q.delivery_kind,q.emulator_path,q.activation_options_json,
       i.id,i.version,i.blob_id,i.status
FROM bios_requirements q
LEFT JOIN bios_installations i ON i.requirement_id=q.id AND i.is_active=1
WHERE q.provider_id=? AND q.target_id=? AND q.source_kind='STATIC' AND q.enabled=1
ORDER BY q.logical_name,q.id
`, providerID, targetID)
	if err != nil {
		return nil, fmt.Errorf("corevalidation/bios: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()

	records := make([]service.BIOSRecord, 0)
	for rows.Next() {
		record, err := scanBIOSDependency(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("corevalidation/read BIOS: %w", err)
	}
	return records, nil
}

func scanBIOSDependency(rows dbexec.Scanner) (service.BIOSRecord, error) {
	var dependency corevalidation.BIOSDependency
	var condition, emulatorPath, optionsJSON sql.NullString
	var installationID, blobID, installationStatus sql.NullString
	var installationVersion sql.NullInt64
	if err := rows.Scan(
		&dependency.RequirementID, &dependency.RequirementVersion,
		&dependency.CatalogDigest, &dependency.LogicalName, &dependency.RequirementMode,
		&condition, &dependency.DeliveryKind, &emulatorPath, &optionsJSON,
		&installationID, &installationVersion, &blobID, &installationStatus,
	); err != nil {
		return service.BIOSRecord{}, fmt.Errorf("corevalidation/bios: %w", err)
	}

	dependency.ConditionCode = nullableString(condition)
	dependency.EmulatorPath = nullableString(emulatorPath)
	dependency.InstallationID = nullableString(installationID)
	dependency.InstallationVersion = nullableInt64(installationVersion)
	dependency.BlobID = nullableString(blobID)
	dependency.InstallationStatus = nullableString(installationStatus)
	return service.BIOSRecord{Dependency: dependency, ActivationOptions: nullableString(optionsJSON)}, nil
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copyValue := value.String
	return &copyValue
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	copyValue := value.Int64
	return &copyValue
}
