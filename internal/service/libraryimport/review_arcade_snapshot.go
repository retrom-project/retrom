package libraryimport

import (
	"encoding/json"

	"retrom/internal/capability/content/corevalidation"
)

type ArcadeDraftDependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type ArcadeDraftSnapshot struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	Kind              string                  `json:"kind"`
	Machine           string                  `json:"machine"`
	DatVersionID      string                  `json:"datVersionId"`
	Closure           json.RawMessage         `json:"closure"`
	Dependencies      []ArcadeDraftDependency `json:"dependencies"`
	MissingEntries    []string                `json:"missingEntries"`
	MismatchedEntries []string                `json:"mismatchedEntries"`
	Warnings          []string                `json:"warnings"`
}

func ParseArcadeDraftSnapshot(raw string) (ArcadeDraftSnapshot, bool) {
	var snapshot ArcadeDraftSnapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil ||
		snapshot.SchemaVersion != corevalidation.SnapshotSchemaVersion ||
		snapshot.Kind != corevalidation.SnapshotKindArcade ||
		snapshot.Machine == "" ||
		snapshot.DatVersionID == "" || snapshot.Dependencies == nil {
		return ArcadeDraftSnapshot{}, false
	}
	return snapshot, true
}
