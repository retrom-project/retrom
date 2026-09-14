package libraryimport

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	contentvalidation "retrom/internal/capability/content/corevalidation"
	corevalidationmodel "retrom/internal/model/corevalidation"
	application "retrom/internal/model/libraryimport"
	corevalidationrepo "retrom/internal/repo/corevalidation"
)

// dependencyFactsDigest fingerprints the mutable dependency facts that can
// change a review validation without changing the draft row. It intentionally
// includes the complete target catalog for static and DAT-backed dependencies;
// being conservative here is preferable to accepting a plan after an input
// that might affect it has changed.
func (records *ReviewValidation) dependencyFactsDigest(
	ctx context.Context, itemID, providerID, targetID, contentKind string,
) (string, error) {
	staticRecords, err := corevalidationrepo.New(records.executor).BIOS(ctx, providerID, targetID)
	if err != nil {
		return "", fmt.Errorf("read static validation facts: %w", err)
	}
	arcadeRecords, err := records.biosFacts(ctx, providerID, targetID, "DAT_MACHINE")
	if err != nil {
		return "", fmt.Errorf("read arcade validation facts: %w", err)
	}
	facts := struct {
		SchemaVersion int    `json:"schemaVersion"`
		ContentKind   string `json:"contentKind"`
		StaticDigest  string `json:"staticDigest"`
		ArcadeDigest  string `json:"arcadeDigest"`
		RPGProfile    any    `json:"rpgProfile,omitempty"`
	}{
		SchemaVersion: 1, ContentKind: contentKind,
		StaticDigest: corevalidationmodel.BIOSFactsDigest(staticRecords),
		ArcadeDigest: corevalidationmodel.BIOSFactsDigest(arcadeRecords),
	}
	if contentKind == "RPG_MAKER_PROJECT" {
		profile, err := records.rpgFacts(ctx, itemID)
		if err != nil {
			return "", err
		}
		facts.RPGProfile = profile
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return "", fmt.Errorf("encode validation facts: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type reviewValidationRPGFacts struct {
	Generation             string  `json:"generation"`
	EvidenceFamily         string  `json:"evidenceFamily"`
	EvidenceGeneration     *string `json:"evidenceGeneration"`
	EvidenceConfidence     string  `json:"evidenceConfidence"`
	EngineVersion          *string `json:"engineVersion"`
	EntryHTMLPath          *string `json:"entryHtmlPath"`
	ProjectFingerprint     string  `json:"projectFingerprint"`
	RequirementsSHA256     string  `json:"requirementsSha256"`
	AnalysisJSON           string  `json:"analysisJson"`
	SelfContainedOverride  bool    `json:"selfContainedOverride"`
	ProviderID             string  `json:"providerId"`
	TargetID               string  `json:"targetId"`
	DependencySnapshotHash string  `json:"dependencySnapshotSha256"`
}

func (records *ReviewValidation) rpgFacts(
	ctx context.Context, itemID string,
) (reviewValidationRPGFacts, error) {
	var facts reviewValidationRPGFacts
	var evidenceGeneration, engineVersion, entryHTMLPath sql.NullString
	err := records.executor.QueryRowContext(ctx, `
SELECT profile.generation,profile.evidence_family,profile.evidence_generation,
  profile.evidence_confidence,profile.engine_version,profile.entry_html_path,
  profile.project_fingerprint,profile.requirements_sha256,profile.analysis_json,
  profile.self_contained_override,profile.provider_id,profile.target_id,
  profile.dependency_snapshot_sha256
FROM rpgmaker_review_profiles profile
JOIN review_drafts draft ON draft.id=profile.review_draft_id
WHERE draft.import_item_id=?
`, itemID).Scan(
		&facts.Generation, &facts.EvidenceFamily, &evidenceGeneration,
		&facts.EvidenceConfidence, &engineVersion, &entryHTMLPath,
		&facts.ProjectFingerprint, &facts.RequirementsSHA256, &facts.AnalysisJSON,
		&facts.SelfContainedOverride, &facts.ProviderID, &facts.TargetID,
		&facts.DependencySnapshotHash,
	)
	if err != nil {
		return reviewValidationRPGFacts{}, fmt.Errorf("read RPG validation facts: %w", err)
	}
	facts.EvidenceGeneration = nullableReviewValidationString(evidenceGeneration)
	facts.EngineVersion = nullableReviewValidationString(engineVersion)
	facts.EntryHTMLPath = nullableReviewValidationString(entryHTMLPath)
	return facts, nil
}

func (records *ReviewValidation) biosFacts(
	ctx context.Context, providerID, targetID, sourceKind string,
) ([]corevalidationmodel.BIOSRecord, error) {
	rows, err := records.executor.QueryContext(ctx, `
SELECT q.id,q.version,q.catalog_digest,q.logical_name,q.requirement_mode,q.condition_code,
  q.delivery_kind,q.emulator_path,q.activation_options_json,
  i.id,i.version,i.blob_id,i.status
FROM bios_requirements q
LEFT JOIN bios_installations i ON i.requirement_id=q.id AND i.is_active=1
WHERE q.provider_id=? AND q.target_id=? AND q.source_kind=? AND q.enabled=1
ORDER BY q.logical_name,q.id
`, providerID, targetID, sourceKind)
	if err != nil {
		return nil, fmt.Errorf("query BIOS validation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := make([]corevalidationmodel.BIOSRecord, 0)
	for rows.Next() {
		var dependency contentvalidation.BIOSDependency
		var condition, emulatorPath, optionsJSON sql.NullString
		var installationID, blobID, installationStatus sql.NullString
		var installationVersion sql.NullInt64
		if err := rows.Scan(
			&dependency.RequirementID, &dependency.RequirementVersion, &dependency.CatalogDigest,
			&dependency.LogicalName, &dependency.RequirementMode, &condition,
			&dependency.DeliveryKind, &emulatorPath, &optionsJSON,
			&installationID, &installationVersion, &blobID, &installationStatus,
		); err != nil {
			return nil, fmt.Errorf("scan BIOS validation facts: %w", err)
		}
		dependency.ConditionCode = nullableReviewValidationString(condition)
		dependency.EmulatorPath = nullableReviewValidationString(emulatorPath)
		dependency.InstallationID = nullableReviewValidationString(installationID)
		dependency.InstallationVersion = nullableReviewValidationInt64(installationVersion)
		dependency.BlobID = nullableReviewValidationString(blobID)
		dependency.InstallationStatus = nullableReviewValidationString(installationStatus)
		result = append(result, corevalidationmodel.BIOSRecord{
			Dependency: dependency, ActivationOptions: nullableReviewValidationString(optionsJSON),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read BIOS validation facts: %w", err)
	}
	return result, nil
}

func nullableReviewValidationInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func reviewValidationGuardMatches(
	guard application.ReviewValidationGuard,
	inputs application.ReviewValidationRefreshInputs,
	targetID string,
	dosEntry *string,
) bool {
	return guard.Valid() && guard.TargetPlatformInstanceID == targetID &&
		guard.SourceSnapshotID == inputs.EffectiveSnapshotID &&
		sameNullable(guard.DefaultDOSEntry, dosEntry) &&
		guard.SourceManifestDigest == inputs.EffectiveManifestDigest &&
		guard.ContentKind == inputs.ContentKind && guard.PlatformID == inputs.PlatformID &&
		guard.PlatformInstanceVersion == inputs.PlatformVersion && guard.CoreID == inputs.CoreID &&
		guard.ProviderID == inputs.ProviderID && guard.TargetID == inputs.RuntimeTargetID &&
		sameNullable(inputs.DATVersionID, guard.DATVersionID) &&
		guard.ContentPolicyDigest == inputs.ContentPolicy.DigestFor(inputs.ContentKind) &&
		guard.DependencyFactsDigest == inputs.DependencyFactsDigest
}
