package libraryimport

import (
	"context"
	"fmt"

	composition "retrom/internal/bootstrap/composition/libraryimport"
	application "retrom/internal/service/libraryimport"
)

func (service *Service) importCreations() *application.ImportCreations {
	return composition.NewCreations(service.database, service.now, service.creationDependencies())
}

func (service *Service) creationDependencies() composition.CreationOptions {
	return composition.CreationOptions{
		Blobs: service.blobs, Tags: service.tags, Scraper: service.scraper,
		ScummVMDetector: service.scummVMDetector, MultiDiscEnabled: service.multiDiscImportEnabled,
	}
}

func (service *Service) Create(ctx context.Context, request CreateRequest) (Created, error) {
	return service.create(ctx, request, nil)
}

func (service *Service) create(
	ctx context.Context,
	request CreateRequest,
	reconfiguration *reconfigurationInput,
	options ...creationOptions,
) (Created, error) {
	if len(options) > 1 {
		return Created{}, ErrInvalid
	}
	intent := application.ImportCreationOptions{}
	if reconfiguration != nil {
		intent.Reconfiguration = &application.ImportReconfiguration{
			ImportID: reconfiguration.sourceImportJobID,
			Version:  reconfiguration.sourceVersion,
			FileIDs:  reconfiguration.sourceFileIDs,
		}
	}
	var binding *ownedSourceCreation
	if len(options) == 1 {
		intent.ReviewHandoffKind = options[0].reviewHandoffKind
		binding = options[0].sourceCreation
		if binding != nil {
			intent.Source = &application.OwnedImportCreation{Intent: binding.intent, Before: binding.before}
		}
	}
	result, err := service.importCreations().Create(ctx, request, intent)
	if err != nil {
		return Created{}, fmt.Errorf("create library import: %w", err)
	}
	if binding != nil {
		binding.result = result.Owned
	}
	return result.Created, nil
}
