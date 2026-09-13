package libraryimport

import (
	"context"
	"fmt"

	composition "retrom/internal/composition/libraryimport"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type creationTarget = application.ImportTarget

type creationOptions struct {
	reviewHandoffKind string
	sourceCreation    *ownedSourceCreation
}

type creationPlan = application.PreparedImport

func (service *Service) importPreparation() *application.ImportPreparation {
	return composition.NewPreparation(service.database, service.creationDependencies())
}

func (service *Service) prepareCreation(ctx context.Context, request CreateRequest) (creationPlan, error) {
	prepared, err := service.importPreparation().Prepare(ctx, request)
	if err != nil {
		return creationPlan{}, fmt.Errorf("prepare import creation: %w", err)
	}
	return prepared, nil
}

func normalizeTargetCreateRequest(
	request CreateRequest, contentMode, purpose, sourceType string, files []importSourceFile, target creationTarget,
) (CreateRequest, string, error) {
	normalized, mode, err := application.NormalizeTargetImport(
		request, contentMode, purpose, sourceType, importFileFacts(files), importTargetFacts(target),
	)
	if err != nil {
		return CreateRequest{}, "", fmt.Errorf("normalize import target: %w", err)
	}
	return normalized, mode, nil
}

func normalizeTargetContentMode(platformID, contentMode string) string {
	return application.NormalizeTargetImportMode(platformID, contentMode)
}

func (service *Service) loadCreationTarget(ctx context.Context, instanceID string) (creationTarget, error) {
	target, err := application.ReadImportTarget(ctx, repository.BindImportFacts(service.database), instanceID)
	if err != nil {
		return creationTarget{}, fmt.Errorf("read creation target: %w", err)
	}
	return legacyCreationTarget(target), nil
}

func legacyPreparationError(err error) error {
	if err != nil {
		return fmt.Errorf("prepare import content: %w", err)
	}
	return nil
}
