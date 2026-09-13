package libraryimport

import (
	"context"
	"database/sql"
	"fmt"

	repository "retrom/internal/persistence/libraryimport"
	application "retrom/internal/service/libraryimport"
)

type creationTarget = application.ImportTarget

type creationOptions struct {
	reviewHandoffKind string
	sourceCreation    *ownedSourceCreation
}

type creationPlan struct {
	reviewHandoffKind string
	sourceCreation    *ownedSourceCreation
	request           CreateRequest
	contentMode       string
	sourceType        string
	target            creationTarget
	datID             sql.NullString
	files             []importSourceFile
	dispositions      []preparedDisposition
	groups            []preparedGroup
	archives          []preparedArchive
}

func (service *Service) importPreparation() *application.ImportPreparation {
	return application.NewImportPreparation(
		repository.BindImportFacts(service.database), repository.BindPreparationCatalog(service.database),
		service.blobs, application.ImportPreparationOptions{
			MultiDiscEnabled: service.multiDiscImportEnabled, MetadataScraperAvailable: service.scraper != nil,
			ScummVMDetector: service.scummVMDetector,
		})
}

func (service *Service) prepareCreation(ctx context.Context, request CreateRequest) (creationPlan, error) {
	prepared, err := service.importPreparation().Prepare(ctx, request)
	if err != nil {
		return creationPlan{}, fmt.Errorf("prepare import creation: %w", err)
	}
	return creationPlan{
		request: prepared.Request, contentMode: prepared.ContentMode, sourceType: prepared.SourceType,
		reviewHandoffKind: reviewHandoffDirect, target: prepared.Target, files: prepared.Files,
		datID:        sql.NullString{String: prepared.DATVersionID, Valid: prepared.DATVersionID != ""},
		dispositions: prepared.Dispositions, groups: prepared.Groups, archives: prepared.Archives,
	}, nil
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
