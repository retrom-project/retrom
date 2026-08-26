package libraryimport

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/rpgmaker/detector"
	"retrom/internal/rpgmaker/packs"
)

type rpgReviewAnalysis struct {
	SelfContained bool `json:"selfContained"`
	Requirements  struct {
		RTP []struct {
			Slot           int    `json:"slot"`
			DeclaredName   string `json:"declaredName"`
			NormalizedName string `json:"normalizedName"`
		} `json:"rtpDependencies"`
	} `json:"requirements"`
}

func (run *draftPatchRun) applyRPGMakerBinding() error {
	proceed, err := run.validRPGMakerPatchShape()
	if err != nil || !proceed {
		return err
	}
	profile, err := run.loadRPGReviewBinding()
	if err != nil {
		return fmt.Errorf("libraryimport/review resolve RPG packs: %w", err)
	}
	generation := detector.Generation(profile.generation)
	override := *run.patch.RPGSelfContainedOverride
	if generation != detector.RPG2000 && generation != detector.RPG2003 && override {
		return ErrInvalid
	}
	selections := rpgPackSelections(*run.patch.RuntimePackSelections)
	definitions, installations, err := run.loadRPGPackCatalog(generation)
	if err != nil {
		return fmt.Errorf("libraryimport/review resolve RPG packs: %w", err)
	}
	resolution, err := packs.Resolve(
		generation, profile.analysis.SelfContained, override, profile.requirements,
		definitions, installations, selections,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/review resolve selected RPG packs: %w", err)
	}
	bindingsJSON, err := json.Marshal(map[string]any{
		"bindings": resolution.Bindings, "schemaVersion": 1,
	})
	if err != nil {
		return ErrInvalid
	}
	digest := sha256.Sum256(bindingsJSON)
	if resolution.DependencySHA256 != hex.EncodeToString(digest[:]) {
		return ErrInvalid
	}
	changed, err := run.replaceRPGPackSelections(resolution.Bindings, override, resolution.DependencySHA256)
	if err != nil {
		return err
	}
	run.rpgBindingChanged = changed
	return nil
}

func (run *draftPatchRun) validRPGMakerPatchShape() (bool, error) {
	if !run.isRPG {
		if run.patch.RuntimePackSelections != nil || run.patch.RPGSelfContainedOverride != nil {
			return false, ErrInvalid
		}
		return false, nil
	}
	if run.patch.RuntimePackSelections == nil || run.patch.RPGSelfContainedOverride == nil ||
		run.targetOrDOSChanged {
		return false, ErrInvalid
	}
	return true, nil
}

func rpgPackSelections(values []RuntimePackSelectionPatch) []packs.Selection {
	selections := make([]packs.Selection, 0, len(values))
	for _, selection := range values {
		selections = append(selections, packs.Selection{
			Slot: selection.Slot, InstallationID: selection.InstallationID,
		})
	}
	return selections
}

type rpgReviewBinding struct {
	generation       string
	override         bool
	dependencySHA256 string
	analysis         rpgReviewAnalysis
	requirements     []packs.Requirement
}

func (run *draftPatchRun) loadRPGReviewBinding() (rpgReviewBinding, error) {
	var result rpgReviewBinding
	var analysisJSON string
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT generation,self_contained_override,dependency_snapshot_sha256,analysis_json
FROM rpgmaker_review_profiles WHERE review_draft_id=?
`, run.draftID).Scan(
		&result.generation, &result.override, &result.dependencySHA256, &analysisJSON,
	); err != nil || json.Unmarshal([]byte(analysisJSON), &result.analysis) != nil {
		return rpgReviewBinding{}, ErrInvalid
	}
	result.requirements = make([]packs.Requirement, 0, len(result.analysis.Requirements.RTP))
	for _, requirement := range result.analysis.Requirements.RTP {
		result.requirements = append(result.requirements, packs.Requirement{
			Slot: requirement.Slot, DeclaredName: requirement.DeclaredName,
			NormalizedName: requirement.NormalizedName,
		})
	}
	return result, nil
}

func (run *draftPatchRun) loadRPGPackCatalog(
	generation detector.Generation,
) ([]packs.Definition, []packs.Installation, error) {
	rows, err := run.transaction.QueryContext(run.ctx, `
SELECT definition.id,definition.declared_name,definition.normalized_declared_name,definition.enabled,
installation.id,installation.files_digest,installation.status,installation.deleted_at_ms
FROM runtime_asset_pack_definitions definition
LEFT JOIN runtime_asset_pack_installations installation ON installation.definition_id=definition.id
WHERE definition.generation=?
ORDER BY definition.id,installation.id
`, generation)
	if err != nil {
		return nil, nil, fmt.Errorf("libraryimport/review RPG pack catalog: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	definitionByID := make(map[string]packs.Definition)
	var installations []packs.Installation
	for rows.Next() {
		var definition packs.Definition
		var installationID, filesDigest, status sql.NullString
		var deletedAt sql.NullInt64
		if err := rows.Scan(
			&definition.ID, &definition.DeclaredName, &definition.NormalizedDeclaredName,
			&definition.Enabled, &installationID, &filesDigest, &status, &deletedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("libraryimport/review RPG pack catalog: %w", err)
		}
		definition.Generation = generation
		definitionByID[definition.ID] = definition
		if installationID.Valid {
			installations = append(installations, packs.Installation{
				ID: installationID.String, DefinitionID: definition.ID, FilesDigest: filesDigest.String,
				Status: status.String, Deleted: deletedAt.Valid,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("libraryimport/review RPG pack catalog: %w", err)
	}
	definitions := make([]packs.Definition, 0, len(definitionByID))
	for _, definition := range definitionByID {
		definitions = append(definitions, definition)
	}
	return definitions, installations, nil
}

func (run *draftPatchRun) replaceRPGPackSelections(
	bindings []packs.Binding,
	override bool,
	dependencySHA256 string,
) (bool, error) {
	var currentCount int
	var currentOverride bool
	var currentDigest string
	if err := run.transaction.QueryRowContext(run.ctx, `
SELECT profile.self_contained_override,profile.dependency_snapshot_sha256,
  (SELECT count(*) FROM review_draft_runtime_pack_selections selection
   WHERE selection.review_draft_id=profile.review_draft_id)
FROM rpgmaker_review_profiles profile WHERE profile.review_draft_id=?
`, run.draftID).Scan(&currentOverride, &currentDigest, &currentCount); err != nil {
		return false, ErrInvalid
	}
	unchanged := currentOverride == override && currentDigest == dependencySHA256 && currentCount == len(bindings)
	if _, err := run.transaction.ExecContext(run.ctx, `
DELETE FROM review_draft_runtime_pack_selections WHERE review_draft_id=?
`, run.draftID); err != nil {
		return false, fmt.Errorf("libraryimport/review replace RPG packs: %w", err)
	}
	for _, binding := range bindings {
		if _, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO review_draft_runtime_pack_selections(
  review_draft_id,slot,declared_name,normalized_declared_name,definition_id,installation_id,created_at_ms
) VALUES(?,?,?,?,?,?,?)
`, run.draftID, binding.Slot, binding.DeclaredName, binding.NormalizedDeclaredName,
			binding.DefinitionID, binding.InstallationID, run.service.now().UnixMilli()); err != nil {
			return false, fmt.Errorf("libraryimport/review replace RPG packs: %w", err)
		}
	}
	if _, err := run.transaction.ExecContext(run.ctx, `
UPDATE rpgmaker_review_profiles
SET self_contained_override=?,dependency_snapshot_sha256=?,updated_at_ms=?
WHERE review_draft_id=?
`, boolIncrement(override), dependencySHA256, run.service.now().UnixMilli(), run.draftID); err != nil {
		return false, fmt.Errorf("libraryimport/review replace RPG packs: %w", err)
	}
	return !unchanged, nil
}
