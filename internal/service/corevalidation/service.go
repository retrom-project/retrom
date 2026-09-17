package corevalidation

import (
	"context"
	"fmt"

	model "retrom/internal/model/corevalidation"

	"retrom/internal/capability/content/corevalidation"
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
	snapshot, status, code, err := model.ResolveBIOSRecords(records, contentLogicalName)
	if err != nil {
		return snapshot, status, code, fmt.Errorf("corevalidation/resolve BIOS: %w", err)
	}
	return snapshot, status, code, nil
}

// ResolveBIOSRecords evaluates a frozen catalog/installation snapshot without
// storage access. It delegates to the model-layer pure function.
func ResolveBIOSRecords(
	records []model.BIOSRecord,
	contentLogicalName string,
) (corevalidation.Snapshot, string, string, error) {
	snapshot, status, code, err := model.ResolveBIOSRecords(records, contentLogicalName)
	if err != nil {
		return snapshot, status, code, fmt.Errorf("corevalidation/resolve records: %w", err)
	}
	return snapshot, status, code, nil
}
