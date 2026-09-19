package corevalidation

import (
	"encoding/json"

	"retrom/internal/capability/content/corevalidation"
)

// ValidateBIOSRequest rejects incomplete identities before BIOS facts are read.
func ValidateBIOSRequest(providerID, targetID, contentLogicalName string) error {
	if providerID == "" || targetID == "" || contentLogicalName == "" {
		return corevalidation.ErrInvalidSnapshot
	}
	return nil
}

// ResolveBIOSRecords evaluates a frozen catalog/installation snapshot without storage access.
func ResolveBIOSRecords(
	records []BIOSRecord,
	contentLogicalName string,
) (corevalidation.Snapshot, string, string, error) {
	if contentLogicalName == "" {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", corevalidation.ErrInvalidSnapshot
	}
	snapshot := corevalidation.Snapshot{
		SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindStatic,
		BIOS: make([]corevalidation.BIOSDependency, 0, len(records)),
	}
	status, code := "READY", "READY"
	for _, record := range records {
		dependency := record.Dependency
		if dependency.ConditionCode != nil && !corevalidation.BIOSApplies(*dependency.ConditionCode, contentLogicalName) {
			continue
		}
		dependency.ActivationOptions = map[string]string{}
		if record.ActivationOptions != nil {
			if err := json.Unmarshal([]byte(*record.ActivationOptions), &dependency.ActivationOptions); err != nil {
				return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", corevalidation.ErrInvalidSnapshot
			}
		}
		valid := dependency.InstallationStatus != nil && dependency.BlobID != nil &&
			corevalidation.BIOSInstallationUsable(*dependency.InstallationStatus)
		if !valid && (dependency.RequirementMode != "OPTIONAL" || dependency.InstallationStatus != nil) {
			status, code = "BLOCKED", "LAUNCH_BIOS_MISSING"
		}
		snapshot.BIOS = append(snapshot.BIOS, dependency)
	}
	return snapshot, status, code, nil
}
