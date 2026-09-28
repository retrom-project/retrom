package libraryimport

import (
	"context"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	libraryservice "retrom/internal/service/libraryimport"
)

type creationTarget = libraryservice.ImportTarget

type creationOptions struct {
	sourceCreation *ownedSourceCreation
}

func normalizeTargetCreateRequest(
	request CreateRequest, contentMode, purpose, sourceType string, files []importSourceFile, target creationTarget,
) (CreateRequest, string, error) {
	normalized, mode, err := libraryservice.NormalizeTargetImport(
		request, contentMode, purpose, sourceType, importFileFacts(files), importTargetFacts(target),
	)
	if err != nil {
		return CreateRequest{}, "", fmt.Errorf("normalize import target: %w", err)
	}
	return normalized, mode, nil
}

func normalizeTargetContentMode(platformID, contentMode string) string {
	return libraryservice.NormalizeTargetImportMode(platformID, contentMode)
}

func (service *Service) loadCreationTarget(ctx context.Context, instanceID string) (creationTarget, error) {
	target, err := libraryservice.ReadImportTarget(ctx, repository.BindImportFacts(service.database), instanceID)
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
