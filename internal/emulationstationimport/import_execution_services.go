package emulationstationimport

import (
	"context"
	"fmt"

	"retrom/internal/blobstore"
	persistence "retrom/internal/persistence/emulationstationimport"
	application "retrom/internal/service/emulationstationimport"
)

func (service *Service) itemWork() *application.ItemWork {
	return application.NewItemWork(persistence.NewItemWork(service.database), service.now)
}

func (service *Service) materialization() *application.Materialization {
	return application.NewMaterialization(persistence.NewMaterialization(service.database), service.now)
}

func verifiedBlob(metadata blobstore.Metadata) application.VerifiedBlob {
	return application.VerifiedBlob{
		SHA256: metadata.SHA256,
		MD5:    metadata.MD5,
		SHA1:   metadata.SHA1,
		CRC32:  metadata.CRC32,
		Size:   metadata.Size,
	}
}

func (service *Service) finishItemOutcome(
	ctx context.Context,
	unit work,
	id string,
	outcome application.ItemOutcome,
) error {
	if err := service.itemWork().Finish(ctx, unit, id, outcome); err != nil {
		return fmt.Errorf("finish EmulationStation item outcome: %w", err)
	}
	return nil
}
