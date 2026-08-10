package libraryimport

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
)

const prepublishGeneration = 4

const (
	validatorImportV4 = "import-source-validator-v4"
	validatorReviewV4 = "review-compatible-v4"
	validatorArcadeV4 = "arcade-source-validator-v4"
	validatorMultiV4  = "multi-disc-source-validator-v4"
)

type prepublishDigestInput struct {
	SchemaVersion             int             `json:"schemaVersion"`
	ValidatorVersion          string          `json:"validatorVersion"`
	SourceSnapshotID          string          `json:"sourceSnapshotId"`
	SourceManifestDigest      string          `json:"sourceManifestDigest"`
	ContentKind               string          `json:"contentKind"`
	TargetPlatformInstanceID  string          `json:"targetPlatformInstanceId"`
	PlatformInstanceVersion   int64           `json:"platformInstanceVersion"`
	CoreArtifactID            string          `json:"coreArtifactId"`
	CoreArtifactVersion       int64           `json:"coreArtifactVersion"`
	CompatibilityConfigDigest string          `json:"compatibilityConfigDigest"`
	DATVersionID              *string         `json:"datVersionId"`
	DefaultDOSEntry           *string         `json:"defaultDosEntry"`
	DependencySnapshot        json.RawMessage `json:"dependencySnapshot"`
	Status                    string          `json:"status"`
	CompatibilityCode         string          `json:"compatibilityCode"`
}

func prepublishDigest(input prepublishDigestInput) string {
	canonical, _ := json.Marshal(input)
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func compatibilityConfigDigest(compatibility string) string {
	digest := sha256.Sum256([]byte(compatibility))
	return hex.EncodeToString(digest[:])
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	valueCopy := value.String
	return &valueCopy
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	valueCopy := value
	return &valueCopy
}

func preparedGroupContentKind(group preparedGroup) string {
	for _, source := range group.sources {
		if source.role == "DOS_SOURCE" {
			return "DOS_BUNDLE"
		}
	}
	return "SINGLE_FILE"
}
