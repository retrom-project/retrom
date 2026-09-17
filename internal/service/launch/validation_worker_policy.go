package launch

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	model "retrom/internal/model/launch"

	"retrom/internal/capability/content/contentprofile"
	"retrom/internal/capability/content/corevalidation"
	"retrom/internal/capability/engine/scummvm"
	"retrom/internal/capability/format/arcadedat"
)

// EvaluateValidation is shared by the asynchronous attempt and its final
// authority check. Facts contain no database handle or provider callback.
func EvaluateValidation(inputs model.ValidationInputs, facts model.ValidationFacts) (model.ValidationOutcome, error) {
	source := facts.Content.Source
	if !validationSourceMatches(inputs, facts) {
		return model.ValidationOutcome{}, ErrValidationGameChanged
	}

	bios, status, code, err := ResolveProductBIOS(source, facts.Content.ValidationBIOS, source.ValidationLogicalName)
	if err != nil {
		return model.ValidationOutcome{}, fmt.Errorf("resolve validation BIOS: %w", err)
	}
	digest, biosDigest, evidence, err := ProductValidationEvidence(facts.Content, inputs.GameVariantID, bios)
	if err != nil {
		return model.ValidationOutcome{}, fmt.Errorf("resolve validation evidence: %w", err)
	}
	if BindCurrentGameStateDigest(
		digest,
		source.GameVersion,
		source.SourceManifestDigest,
	) != inputs.ValidationInputDigest || biosDigest != inputs.BIOSDependencyDigest {
		return model.ValidationOutcome{}, ErrValidationGameChanged
	}
	if source.ContentKind == "SCUMMVM_PROJECT" && source.ProviderID == "retrom-runtime" && source.TargetID == "scummvm" {
		return validationScummVMOutcome(inputs, facts, bios)
	}

	encoded, err := evidence.JSON()
	if err != nil {
		return model.ValidationOutcome{}, fmt.Errorf("encode validation dependencies: %w", err)
	}
	dependency := string(encoded)
	if inputs.DATVersionID != nil {
		if err := ValidateLockedArcadeSnapshot(
			source.DependencySnapshot,
			source.ValidationLogicalName,
			*inputs.DATVersionID,
		); err != nil {
			return model.ValidationOutcome{}, err
		}
		dependency = source.DependencySnapshot
	}
	if status == "READY" {
		status, code = validationContentStatus(facts)
	}
	return model.ValidationOutcome{Status: status, Code: code, DependencyJSON: dependency, BIOS: evidence}, nil
}

func validateFinalValidation(inputs model.ValidationInputs, before, after model.ValidationFacts) error {
	_, err := EvaluateValidation(inputs, after)
	if err != nil {
		return err
	}
	// Evidence already binds current BIOS, game version/manifest and ordered discs.
	// Selection, target support and locked native/Arcade dependencies also fence the result.
	if before.VariantVersion != after.VariantVersion || before.BindingFound != after.BindingFound ||
		before.RelationshipEnabled != after.RelationshipEnabled ||
		before.Classification != after.Classification || before.Content.Source.CoreID != after.Content.Source.CoreID ||
		before.Content.Source.PlatformID != after.Content.Source.PlatformID ||
		before.Content.Source.DependencySnapshot != after.Content.Source.DependencySnapshot ||
		before.Content.Source.ValidationLogicalName != after.Content.Source.ValidationLogicalName {
		return ErrValidationGameChanged
	}
	return nil
}

func validationContentStatus(facts model.ValidationFacts) (string, string) {
	source := facts.Content.Source
	if !facts.BindingFound {
		return "BLOCKED", validationUnavailable
	}
	if !facts.RelationshipEnabled {
		return "INCOMPATIBLE", "CORE_PLATFORM_UNSUPPORTED"
	}
	if status, code := ValidateContentProfile(
		source.PlatformID,
		source.ValidationLogicalName,
		source.ContentKind,
	); status != "READY" {
		return status, code
	}
	if arcadedat.SupportsCore(source.CoreID) {
		if source.DATVersionID == nil || !strings.EqualFold(filepath.Ext(source.ValidationLogicalName), ".zip") {
			return "INCOMPATIBLE", "ARCADE_CONTENT_NOT_ROMSET"
		}
		if facts.Classification != "NORMAL" {
			return "INCOMPATIBLE", "ARCADE_MACHINE_NOT_FOUND"
		}
	}
	return "READY", "READY"
}

func ValidateContentProfile(platformID, logicalName, contentKind string) (string, string) {
	if contentKind == corevalidation.MultiDiscContentKind && !contentprofile.AllowsContentKind(
		platformID,
		contentprofile.ContentKindMultiDisc,
	) {
		return "INCOMPATIBLE", "CORE_CONTENT_FORMAT_UNSUPPORTED"
	}
	if _, exists := contentprofile.ByPlatform(platformID); exists {
		if !contentprofile.AcceptsRaw(platformID, logicalName) {
			return "INCOMPATIBLE", "CORE_CONTENT_FORMAT_UNSUPPORTED"
		}
		return "READY", "READY"
	}
	if platformID != "arcade" && platformID != "dos" {
		return "BLOCKED", "CORE_CONTENT_PROFILE_MISSING"
	}
	return "READY", "READY"
}

func ValidateLockedArcadeSnapshot(raw, logicalName, datID string) error {
	var identity struct {
		SchemaVersion int    `json:"schemaVersion"`
		Kind          string `json:"kind"`
		Machine       string `json:"machine"`
		DATVersionID  string `json:"datVersionId"`
	}
	if err := json.Unmarshal([]byte(raw), &identity); err != nil {
		return corevalidation.ErrInvalidSnapshot
	}
	machine := strings.TrimSuffix(filepath.Base(logicalName), filepath.Ext(logicalName))
	if identity.SchemaVersion != corevalidation.SnapshotSchemaVersion ||
		identity.Kind != corevalidation.SnapshotKindArcade ||
		identity.Machine != machine ||
		identity.DATVersionID != datID {
		return corevalidation.ErrInvalidSnapshot
	}
	if _, err := corevalidation.ParseRuntimeBIOSDependencies(raw); err != nil {
		return fmt.Errorf("parse Arcade runtime dependencies: %w", err)
	}
	return nil
}

func validationSourceMatches(inputs model.ValidationInputs, facts model.ValidationFacts) bool {
	source := facts.Content.Source
	if !facts.Found ||
		source.GameVersion != inputs.GameVersion ||
		source.SourceManifestDigest != inputs.SourceManifestDigest ||
		source.VariantID != inputs.GameVariantID ||
		source.ProviderID != inputs.ProviderID ||
		source.TargetID != inputs.TargetID ||
		!reflect.DeepEqual(
			source.ContentPolicy,
			inputs.ContentPolicy,
		) || !reflect.DeepEqual(
		source.DATVersionID,
		inputs.DATVersionID,
	) {
		return false
	}
	return true
}

func validationScummVMOutcome(
	inputs model.ValidationInputs,
	facts model.ValidationFacts,
	bios corevalidation.Snapshot,
) (model.ValidationOutcome, error) {
	source := facts.Content.Source
	snapshot, err := scummvm.ParseSnapshot(source.DependencySnapshot)
	if err != nil || snapshot.Detection.SourceDigest != inputs.SourceManifestDigest {
		return model.ValidationOutcome{}, ErrValidationGameChanged
	}
	status, code := snapshot.Status()
	if !facts.BindingFound {
		status, code = "BLOCKED", validationUnavailable
	} else if !facts.RelationshipEnabled {
		status, code = "INCOMPATIBLE", "CORE_PLATFORM_UNSUPPORTED"
	}
	return model.ValidationOutcome{Status: status, Code: code, DependencyJSON: source.DependencySnapshot, BIOS: bios}, nil
}
