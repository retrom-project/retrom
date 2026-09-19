package libraryimport

import (
	"context"
	"fmt"

	composition "retrom/internal/bootstrap/composition/libraryimport"
	libraryimportmodel "retrom/internal/model/libraryimport"

	repository "retrom/internal/repo/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type creationTarget = libraryimportmodel.ImportTarget

type creationOptions struct {
	reviewHandoffKind string
	sourceCreation    *ownedSourceCreation
}

func (service *Service) importPreparation() *application.ImportPreparation {
	return composition.NewPreparation(service.database, service.creationDependencies())
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
