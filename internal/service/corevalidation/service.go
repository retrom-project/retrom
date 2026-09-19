package corevalidation

import (
	"context"
	"encoding/json"
	"fmt"

	"retrom/internal/capability/content/corevalidation"
	model "retrom/internal/model/corevalidation"
)

type Service struct{ repository model.Repository }

func New(repository model.Repository) *Service { return &Service{repository: repository} }

func (service *Service) Catalog(
	ctx context.Context,
	providerID, targetID string,
) ([]corevalidation.BIOSCatalogEntry, error) {
	result, err := service.repository.Catalog(ctx, providerID, targetID)
	if err != nil {
		return nil, fmt.Errorf("corevalidation/catalog: %w", err)
	}
	return result, nil
}

func (service *Service) ResolveBIOS(
	ctx context.Context, providerID, targetID, contentLogicalName string,
) (corevalidation.Snapshot, string, string, error) {
	if providerID == "" || targetID == "" || contentLogicalName == "" {
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", corevalidation.ErrInvalidSnapshot
	}
	records, err := service.repository.BIOS(ctx, providerID, targetID)
	if err != nil {
		failure := fmt.Errorf("corevalidation/read BIOS: %w", err)
		return corevalidation.Snapshot{}, "BLOCKED", "LAUNCH_CORE_VALIDATION_UNAVAILABLE", failure
	}
	return ResolveBIOSRecords(records, contentLogicalName)
}

// ResolveBIOSRecords evaluates a frozen catalog/installation snapshot without storage access.
func ResolveBIOSRecords(
	records []model.BIOSRecord,
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
