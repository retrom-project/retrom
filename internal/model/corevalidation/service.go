package corevalidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	contentvalidation "retrom/internal/capability/content/corevalidation"
)

// Repository is the storage-facing contract used by the core-validation
// service. The contract lives in model so repositories and services share the
// same boundary without a dependency from repo back to service.
type Repository interface {
	Catalog(context.Context, string, string) ([]contentvalidation.BIOSCatalogEntry, error)
	BIOS(context.Context, string, string) ([]BIOSRecord, error)
}

type BIOSRecord struct {
	Dependency        contentvalidation.BIOSDependency
	ActivationOptions *string
}

// BIOSFactsDigest returns a stable digest for the complete set of mutable BIOS
// facts used by validation planning. The record set is copied and sorted here
// so callers do not have to rely on a particular SQL ORDER BY clause.
func BIOSFactsDigest(records []BIOSRecord) string {
	ordered := append([]BIOSRecord(nil), records...)
	sort.SliceStable(ordered, func(left, right int) bool {
		leftKey := ordered[left].Dependency.RequirementID + "\x00" + ordered[left].Dependency.LogicalName
		rightKey := ordered[right].Dependency.RequirementID + "\x00" + ordered[right].Dependency.LogicalName
		return leftKey < rightKey
	})
	encoded, err := json.Marshal(struct {
		SchemaVersion int
		Records       []BIOSRecord
	}{SchemaVersion: 1, Records: ordered})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
