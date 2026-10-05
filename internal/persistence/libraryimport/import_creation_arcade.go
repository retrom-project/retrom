package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	corevalidation "retrom/internal/core/validation"
	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
)

type creationArcadeRecords struct{ executor dbapi.Executor }

func BindCreationArcade(executor dbapi.Executor) libraryservice.CreationArcadeReader {
	return creationArcadeRecords{executor: executor}
}

func (records creationArcadeRecords) BIOS(
	ctx context.Context,
	providerID, targetID, logicalName string,
) (corevalidation.BIOSDependency, bool, error) {
	var resolved corevalidation.BIOSDependency
	var condition, emulatorPath, installationID, fileRecord, installationStatus sql.NullString
	var installationVersion sql.NullInt64
	err := dbapi.QueryRowContext(ctx, records.executor, `
SELECT q.id,
q.version,
q.catalog_digest,
q.logical_name,
q.requirement_mode,
q.condition_code,
q.delivery_kind,
q.emulator_path,
i.id,
i.version,
i.file_record,
i.status
FROM bios_requirements q
JOIN bios_installations i ON i.requirement_id=q.id
AND i.is_active=1
AND i.validated_requirement_version=q.version
AND i.status IN ('MATCHED','UNVERIFIED','HASH_WARNING','MISSING_ENTRY')
WHERE q.provider_id=? AND q.target_id=?
AND q.source_kind='DAT_MACHINE'
AND q.enabled=1
AND q.logical_name=?
`, providerID, targetID, logicalName).Scan(
		&resolved.RequirementID,
		&resolved.RequirementVersion,
		&resolved.CatalogDigest,
		&resolved.LogicalName,
		&resolved.RequirementMode,
		&condition,
		&resolved.DeliveryKind,
		&emulatorPath,
		&installationID,
		&installationVersion,
		&fileRecord,
		&installationStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return corevalidation.BIOSDependency{}, false, nil
	}
	if err != nil {
		return corevalidation.BIOSDependency{}, false, fmt.Errorf("libraryimport/review: resolve arcade BIOS: %w", err)
	}
	resolved.ConditionCode = dbapi.StringPointer(condition)
	resolved.EmulatorPath = dbapi.StringPointer(emulatorPath)
	resolved.ActivationOptions = map[string]string{}
	resolved.InstallationID = dbapi.StringPointer(installationID)
	if installationVersion.Valid {
		resolved.InstallationVersion = &installationVersion.Int64
	}
	resolved.FileRecord = dbapi.StringPointer(fileRecord)
	resolved.InstallationStatus = dbapi.StringPointer(installationStatus)
	return resolved, true, nil
}
