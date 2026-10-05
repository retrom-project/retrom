package arcade

import (
	"encoding/json"

	"retrom/internal/content/diagnostic"
	corevalidation "retrom/internal/core/validation"
)

type Dependency struct {
	Kind                string   `json:"kind"`
	Machine             string   `json:"machine"`
	RequiredBy          *string  `json:"requiredBy,omitempty"`
	Depth               int      `json:"depth,omitempty"`
	ExpectedLogicalName string   `json:"expectedLogicalName,omitempty"`
	State               string   `json:"state"`
	RequiredEntryCount  int      `json:"requiredEntryCount,omitempty"`
	RequiredEntries     []string `json:"requiredEntries"`
}

type Snapshot struct {
	ContentRejection  *diagnostic.Rejection `json:"contentRejection,omitempty"`
	SchemaVersion     int                   `json:"schemaVersion"`
	Kind              string                `json:"kind"`
	Machine           string                `json:"machine"`
	DatVersionID      string                `json:"datVersionId"`
	Closure           []ClosureNode         `json:"closure"`
	Dependencies      []Dependency          `json:"dependencies"`
	MissingEntries    []string              `json:"missingEntries"`
	MismatchedEntries []string              `json:"mismatchedEntries"`
	Warnings          []string              `json:"warnings"`
}

func ParseSnapshot(raw string) (Snapshot, bool) {
	var snapshot Snapshot
	if json.Unmarshal([]byte(raw), &snapshot) != nil ||
		snapshot.SchemaVersion != corevalidation.SnapshotSchemaVersion ||
		snapshot.Kind != corevalidation.SnapshotKindArcade ||
		snapshot.Machine == "" ||
		snapshot.DatVersionID == "" || snapshot.Dependencies == nil {
		return Snapshot{}, false
	}
	return snapshot, true
}
