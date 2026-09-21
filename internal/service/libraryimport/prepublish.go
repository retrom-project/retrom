package libraryimport

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
)

type PrepublishDigestInput struct {
	SchemaVersion            int             `json:"schemaVersion"`
	SourceSnapshotID         string          `json:"sourceSnapshotId"`
	SourceManifestDigest     string          `json:"sourceManifestDigest"`
	ContentKind              string          `json:"contentKind"`
	TargetPlatformInstanceID string          `json:"targetPlatformInstanceId"`
	ProviderID               string          `json:"providerId"`
	TargetID                 string          `json:"targetId"`
	ContentPolicyDigest      string          `json:"contentPolicyDigest"`
	DATVersionID             *string         `json:"datVersionId"`
	DefaultDOSEntry          *string         `json:"defaultDosEntry"`
	DependencySnapshot       json.RawMessage `json:"dependencySnapshot"`
	Status                   string          `json:"status"`
	CompatibilityCode        string          `json:"compatibilityCode"`
}

func PrepublishDigest(input PrepublishDigestInput) string {
	canonical, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func PrepublishDigestMatches(digest string, input PrepublishDigestInput) bool {
	return len(digest) == 64 && subtle.ConstantTimeCompare([]byte(digest), []byte(PrepublishDigest(input))) == 1
}
