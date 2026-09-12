package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"retrom/internal/dbexec"
	persistence "retrom/internal/persistence/launch"
	application "retrom/internal/service/launch"

	"retrom/internal/contentcapability"

	"retrom/internal/corevalidation"
)

type validationInputs struct {
	GameID                string                   `json:"gameId"`
	GameVariantID         string                   `json:"gameVariantId"`
	GameVersion           int64                    `json:"gameVersion"`
	SourceManifestDigest  string                   `json:"sourceManifestDigest"`
	ProviderID            string                   `json:"providerId"`
	TargetID              string                   `json:"targetId"`
	ContentPolicy         contentcapability.Policy `json:"contentPolicy"`
	DATVersionID          any                      `json:"datVersionId"`
	ValidationInputDigest string                   `json:"validationInputDigest"`
	BIOSDependencyDigest  string                   `json:"biosDependencyDigest"`
}

type validationSnapshot struct {
	SchemaVersion int              `json:"schemaVersion"`
	Kind          string           `json:"kind"`
	Scope         validationScope  `json:"scope"`
	ExecutionID   string           `json:"executionId"`
	Inputs        validationInputs `json:"inputs"`
}

type validationScope struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

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

func (service *Service) currentValidationEvidence(
	ctx context.Context, variantID, contentID, contentLogicalName, contentKind, providerID, targetID string,
	contentPolicy contentcapability.Policy, datID sql.NullString,
) (string, string, corevalidation.Snapshot, string, string, error) {
	return service.variantValidationEvidence(
		ctx,
		service.database,
		variantID,
		contentID,
		contentLogicalName,
		contentKind,
		providerID,
		targetID,
		contentPolicy,
		datID,
	)
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

type arcadeSnapshotIdentity struct {
	SchemaVersion int    `json:"schemaVersion"`
	Kind          string `json:"kind"`
	Machine       string `json:"machine"`
	DATVersionID  string `json:"datVersionId"`
}

func validateLockedArcadeSnapshot(raw, contentLogicalName, datID string) error {
	var identity arcadeSnapshotIdentity
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return corevalidation.ErrInvalidSnapshot
	}
	machine := strings.TrimSuffix(filepath.Base(contentLogicalName), filepath.Ext(contentLogicalName))
	if identity.SchemaVersion != corevalidation.SnapshotSchemaVersion ||
		identity.Kind != corevalidation.SnapshotKindArcade ||
		identity.Machine != machine || identity.DATVersionID != datID {
		return corevalidation.ErrInvalidSnapshot
	}
	if _, err := corevalidation.ParseRuntimeBIOSDependencies(raw); err != nil {
		return fmt.Errorf("parse Arcade runtime dependency snapshot: %w", err)
	}
	return nil
}

func (service *Service) lockedArcadeDependencySnapshot(
	ctx context.Context,
	variantID, contentID, contentLogicalName, datID string,
) (string, error) {
	var raw string
	if err := service.database.QueryRowContext(ctx, `
SELECT variant.dependency_snapshot_json
FROM game_variants variant
WHERE variant.id=? AND variant.game_id=? AND variant.dat_version_id=?
`, variantID, contentID, datID).Scan(&raw); err != nil {
		return "", fmt.Errorf("load locked Arcade dependency snapshot: %w", err)
	}
	if err := validateLockedArcadeSnapshot(raw, contentLogicalName, datID); err != nil {
		return "", fmt.Errorf("validate locked Arcade dependency snapshot: %w", err)
	}
	return raw, nil
}
