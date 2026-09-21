package libraryimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"retrom/internal/corevalidation"
	"retrom/internal/dbexec"
	application "retrom/internal/service/libraryimport"
)

type creationArcadeRecords struct{ executor dbexec.Executor }

func BindCreationArcade(executor dbexec.Executor) application.CreationArcadeReader {
	return creationArcadeRecords{executor: executor}
}

func (records creationArcadeRecords) BIOS(
	ctx context.Context,
	providerID, targetID, logicalName string,
) (corevalidation.BIOSDependency, bool, error) {
	var resolved corevalidation.BIOSDependency
	var condition, emulatorPath, installationID, blobID, installationStatus sql.NullString
	var installationVersion sql.NullInt64
	err := records.executor.QueryRowContext(ctx, `
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
i.blob_id,
i.status
FROM bios_requirements q
JOIN bios_installations i ON i.requirement_id=q.id
AND i.is_active=1
AND i.validated_requirement_version=q.version
AND i.status IN ('MATCHED','HASH_WARNING','MISSING_ENTRY')
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
		&blobID,
		&installationStatus,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return corevalidation.BIOSDependency{}, false, nil
	}
	if err != nil {
		return corevalidation.BIOSDependency{}, false, fmt.Errorf("libraryimport/review: resolve arcade BIOS: %w", err)
	}
	resolved.ConditionCode = dbexec.StringPointer(condition)
	resolved.EmulatorPath = dbexec.StringPointer(emulatorPath)
	resolved.ActivationOptions = map[string]string{}
	resolved.InstallationID = dbexec.StringPointer(installationID)
	if installationVersion.Valid {
		resolved.InstallationVersion = &installationVersion.Int64
	}
	resolved.BlobID = dbexec.StringPointer(blobID)
	resolved.InstallationStatus = dbexec.StringPointer(installationStatus)
	return resolved, true, nil
}
