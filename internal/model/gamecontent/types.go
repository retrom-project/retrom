package gamecontent

import (
	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/capability/content/contentcapability"
)

type JobSnapshot struct {
	ExecutionID             string                   `json:"executionId"`
	GameID                  string                   `json:"gameId"`
	GameVersion             int64                    `json:"gameVersion"`
	BaseManifestDigest      string                   `json:"baseManifestDigest"`
	UploadSessionID         string                   `json:"uploadSessionId"`
	PlatformID              string                   `json:"platformId"`
	PlatformInstanceID      string                   `json:"platformInstanceId"`
	PlatformInstanceVersion int64                    `json:"platformInstanceVersion"`
	CoreID                  string                   `json:"coreId"`
	ProviderID              string                   `json:"providerId"`
	TargetID                string                   `json:"targetId"`
	ContentPolicy           contentcapability.Policy `json:"contentPolicy"`
	TargetPolicyDigest      string                   `json:"targetPolicyDigest"`
	ContentMode             string                   `json:"contentMode"`
	MaxDiscs                int                      `json:"maxDiscs,omitempty"`
	MaxTotalBytes           int64                    `json:"maxTotalBytes,omitempty"`
	DATVersionID            *string                  `json:"datVersionId"`
	ConfigSnapshotDigest    string                   `json:"configSnapshotDigest"`
	VariantID               string                   `json:"variantId,omitempty"`
	RPGGeneration           string                   `json:"rpgGeneration,omitempty"`
	RPGDependencySHA256     string                   `json:"rpgDependencySha256,omitempty"`
	RPGRequirementsSHA256   string                   `json:"rpgRequirementsSha256,omitempty"`
}

type UploadedFile struct {
	LogicalName, BlobID, SHA256 string
	SizeBytes                   int64
}

type ReplacementFile struct {
	Role, LogicalName, BlobID, SHA256 string
	SizeBytes                         int64
	SortOrder                         int
}

type PreparedReplacement struct {
	ContentKind             string
	Files                   []ReplacementFile
	Manifest                []byte
	ManifestDigest          string
	CanonicalPlaylist       blobstore.Metadata
	OrderedDiscSHA256       []string
	FirstContentLogicalName string
	RPGMaker                *PreparedRPGMakerReplacement
}
