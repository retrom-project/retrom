package libraryimport

import (
	"context"
	"fmt"

	libraryservice "retrom/internal/service/libraryimport"
)

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
	intent := libraryservice.ImportCreationOptions{}
	if reconfiguration != nil {
		intent.Reconfiguration = &libraryservice.ImportReconfiguration{
			ImportID: reconfiguration.sourceImportJobID,
			Version:  reconfiguration.sourceVersion,
			FileIDs:  reconfiguration.sourceFileIDs,
		}
	}
	var binding *ownedSourceCreation
	if len(options) == 1 {
		binding = options[0].sourceCreation
		if binding != nil {
			intent.Source = &libraryservice.OwnedImportCreation{Intent: binding.intent, Before: binding.before}
		}
	}
	result, err := service.creations.Create(ctx, request, intent)
	if err != nil {
		return Created{}, fmt.Errorf("create library import: %w", err)
	}
	if binding != nil {
		binding.result = result.Owned
	}
	return result.Created, nil
}
