package launch

import (
	"context"
	"fmt"

	"retrom/internal/foundation/cleanup"
	application "retrom/internal/model/launch"
	validation "retrom/internal/repo/corevalidation"
	"retrom/internal/repo/dbexec"
)

func ProductBIOSFacts(
	ctx context.Context,
	executor dbexec.Executor,
	source application.ProductSource,
	datID *string,
) (application.ProductBIOSFacts, error) {
	records, err := validation.New(executor).BIOS(ctx, source.ProviderID, source.TargetID)
	if err != nil {
		return application.ProductBIOSFacts{}, fmt.Errorf("read product static BIOS: %w", err)
	}
	facts := application.ProductBIOSFacts{Static: records, Arcade: []application.ProductArcadeBIOS{}}
	if datID == nil {
		return facts, nil
	}
	rows, err := executor.QueryContext(
		ctx,
		`SELECT dependency.logical_archive,dependency.state,
requirement.id,requirement.version,requirement.catalog_digest,requirement.requirement_mode,
requirement.condition_code,requirement.delivery_kind,requirement.emulator_path,requirement.activation_options_json,
installation.id,installation.version,installation.blob_id,installation.status
FROM game_variants variant
JOIN variant_dependencies dependency ON dependency.game_variant_id=variant.id AND dependency.kind='BIOS_OR_BASE'
LEFT JOIN bios_requirements requirement ON requirement.provider_id=? AND requirement.target_id=?
AND requirement.source_kind='DAT_MACHINE' AND requirement.source_version=?
AND requirement.logical_name=dependency.logical_archive AND requirement.enabled=1
LEFT JOIN bios_installations installation ON installation.requirement_id=requirement.id
AND installation.is_active=1 AND installation.validated_requirement_version=requirement.version
WHERE variant.id=? AND variant.game_id=? AND variant.dat_version_id IS ?
ORDER BY dependency.logical_archive`,
		source.ProviderID,
		source.TargetID,
		*datID,
		source.VariantID,
		source.GameID,
		*datID,
	)
	if err != nil {
		return application.ProductBIOSFacts{}, fmt.Errorf("query product arcade BIOS: %w", err)
	}
	defer func() { cleanup.Error("close product arcade BIOS", rows.Close()) }()
	for rows.Next() {
		record, err := scanProductArcadeBIOS(rows)
		if err != nil {
			return application.ProductBIOSFacts{}, err
		}
		facts.Arcade = append(facts.Arcade, record)
	}
	if err := rows.Err(); err != nil {
		return application.ProductBIOSFacts{}, fmt.Errorf("iterate product arcade BIOS: %w", err)
	}
	return facts, nil
}

func scanProductArcadeBIOS(scanner dbexec.Scanner) (application.ProductArcadeBIOS, error) {
	var record application.ProductArcadeBIOS
	var requirement, catalog, mode, delivery *string
	var version *int64
	dependency := &record.Dependency
	err := scanner.Scan(&dependency.LogicalName, &record.State, &requirement, &version, &catalog, &mode,
		&dependency.ConditionCode, &delivery, &dependency.EmulatorPath, &record.OptionsJSON,
		&dependency.InstallationID, &dependency.InstallationVersion, &dependency.BlobID, &dependency.InstallationStatus)
	if err != nil {
		return application.ProductArcadeBIOS{}, fmt.Errorf("scan product arcade BIOS: %w", err)
	}
	record.CatalogPresent = requirement != nil && version != nil && catalog != nil && mode != nil && delivery != nil
	if record.CatalogPresent {
		dependency.RequirementID = *requirement
		dependency.RequirementVersion = *version
		dependency.CatalogDigest = *catalog
		dependency.RequirementMode = *mode
		dependency.DeliveryKind = *delivery
	}
	return record, nil
}
