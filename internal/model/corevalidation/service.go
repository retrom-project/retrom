package corevalidation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	contentvalidation "retrom/internal/capability/content/corevalidation"
)

// ResolveBIOSRecords evaluates a frozen catalog/installation snapshot without
// storage access. It is a pure function that model, repo, and service layers
// can all call.
func ResolveBIOSRecords(
	records []BIOSRecord,
	contentLogicalName string,
) (contentvalidation.Snapshot, string, string, error) {
	if contentLogicalName == "" {
		return contentvalidation.Snapshot{}, "BLOCKED",
			"LAUNCH_CORE_VALIDATION_UNAVAILABLE", contentvalidation.ErrInvalidSnapshot
	}
	snapshot := contentvalidation.Snapshot{
		SchemaVersion: contentvalidation.SnapshotSchemaVersion,
		Kind:          contentvalidation.SnapshotKindStatic,
		BIOS:          make([]contentvalidation.BIOSDependency, 0, len(records)),
	}
	status, code := "READY", "READY"
	for _, record := range records {
		dependency := record.Dependency
		if dependency.ConditionCode != nil && !contentvalidation.BIOSApplies(*dependency.ConditionCode, contentLogicalName) {
			continue
		}
		dependency.ActivationOptions = map[string]string{}
		if record.ActivationOptions != nil {
			if err := json.Unmarshal([]byte(*record.ActivationOptions), &dependency.ActivationOptions); err != nil {
				return contentvalidation.Snapshot{}, "BLOCKED",
					"LAUNCH_CORE_VALIDATION_UNAVAILABLE", contentvalidation.ErrInvalidSnapshot
			}
		}
		valid := dependency.InstallationStatus != nil && dependency.BlobID != nil &&
			contentvalidation.BIOSInstallationUsable(*dependency.InstallationStatus)
		if !valid && (dependency.RequirementMode != "OPTIONAL" || dependency.InstallationStatus != nil) {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		snapshot.BIOS = append(snapshot.BIOS, dependency)
	}
	return snapshot, status, code, nil
}

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
