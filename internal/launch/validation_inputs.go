package launch

import (
	"context"
	"database/sql"
	"fmt"

	"retrom/internal/dbexec"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"

	"retrom/internal/contentcapability"

	"retrom/internal/corevalidation"
)

func (service *Service) resolveVariantBIOS(
	ctx context.Context, database dbexec.Executor,
	variantID, contentID, providerID, targetID, contentLogicalName string, datID sql.NullString,
) (corevalidation.Snapshot, string, string, error) {
	source := application.ProductSource{
		VariantID:    variantID,
		GameID:       contentID,
		ProviderID:   providerID,
		TargetID:     targetID,
		DATVersionID: dbexec.StringPointer(datID),
	}
	var facts application.ProductBIOSFacts
	if providerID != "retrom-runtime" || targetID != "scummvm" {
		var err error
		facts, err = persistence.ProductBIOSFacts(ctx, database, source, source.DATVersionID)
		if err != nil {
			return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", fmt.Errorf(
				"read validation BIOS: %w",
				err,
			)
		}
	}
	snapshot, status, code, err := application.ResolveProductBIOS(source, facts, contentLogicalName)
	if err != nil {
		return snapshot, status, code, fmt.Errorf("resolve variant BIOS: %w", err)
	}
	return snapshot, status, code, nil
}

func (service *Service) validationDigests(
	ctx context.Context, transaction *sql.Tx,
	variantID, contentID, contentLogicalName, contentKind, providerID, targetID string,
	contentPolicy contentcapability.Policy, datID sql.NullString,
) (string, string, error) {
	digest, biosDigest, _, _, _, err := service.variantValidationEvidence(
		ctx,
		transaction,
		variantID,
		contentID,
		contentLogicalName,
		contentKind,
		providerID,
		targetID,
		contentPolicy,
		datID,
	)
	return digest, biosDigest, err
}

func (service *Service) variantValidationEvidence(
	ctx context.Context, database dbexec.Executor,
	variantID, contentID, contentLogicalName, contentKind, providerID, targetID string,
	contentPolicy contentcapability.Policy, datID sql.NullString,
) (string, string, corevalidation.Snapshot, string, string, error) {
	bios, status, code, err := service.resolveVariantBIOS(
		ctx,
		database,
		variantID,
		contentID,
		providerID,
		targetID,
		contentLogicalName,
		datID,
	)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("read validation evidence: %w", err)
	}
	source := application.ProductSource{
		VariantID:          variantID,
		GameID:             contentID,
		ContentKind:        contentKind,
		ProviderID:         providerID,
		TargetID:           targetID,
		ContentPolicy:      contentPolicy,
		ActiveDATVersionID: dbexec.StringPointer(datID),
	}
	snapshot := application.ProductSnapshot{Source: source}
	if contentKind == corevalidation.MultiDiscContentKind {
		snapshot, err = persistence.ProductContentSnapshot(ctx, database, source)
		if err != nil {
			return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("read validation evidence: %w", err)
		}
	}
	digest, biosDigest, evidence, err := application.ProductValidationEvidence(snapshot, variantID, bios)
	if err != nil {
		return "", "", corevalidation.Snapshot{}, "", "", fmt.Errorf("variant validation evidence: %w", err)
	}
	return digest, biosDigest, evidence, status, code, nil
}

func validateLockedArcadeSnapshot(raw, contentLogicalName, datID string) error {
	if err := application.ValidateLockedArcadeSnapshot(raw, contentLogicalName, datID); err != nil {
		return fmt.Errorf("validate locked Arcade snapshot: %w", err)
	}
	return nil
}
