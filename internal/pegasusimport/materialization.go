package pegasusimport

import (
	"context"
	"fmt"

	"retrom/internal/blobstore"
	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) materialization() *application.Materialization {
	return application.NewMaterialization(repository.NewMaterialization(service.database), service.now)
}

func (service *Service) recordCopiedFile(
	ctx context.Context,
	unit work,
	itemID string,
	file executionFile,
	metadata blobstore.Metadata,
) (string, error) {
	result, err := service.materialization().Copy(ctx, unit.Identity(), application.MaterialSource{
		Key:  application.MaterialKey{ItemID: itemID, Ordinal: file.Ordinal},
		Path: file.Path, Facts: file.Facts, Size: file.Size,
	}, verifiedMaterial(metadata))
	if err != nil {
		return "", fmt.Errorf("pegasusimport/bind copied file: %w", err)
	}
	return result, nil
}

func materialAsset(itemID string, asset executionAsset) application.MaterialSource {
	return application.MaterialSource{
		Key:   application.MaterialKey{ItemID: itemID, Kind: asset.Kind},
		Path:  asset.Path,
		Facts: asset.Facts,

		Size:      asset.Size,
		MediaType: asset.MediaType,
		Width:     asset.Width,
		Height:    asset.Height,
	}
}

func verifiedMaterial(metadata blobstore.Metadata) application.VerifiedBlob {
	return application.VerifiedBlob{
		SHA256: metadata.SHA256,
		MD5:    metadata.MD5,
		SHA1:   metadata.SHA1,
		CRC32:  metadata.CRC32,
		Size:   metadata.Size,
	}
}

func (service *Service) recordCopiedAsset(
	ctx context.Context,
	unit work,
	itemID string,
	asset executionAsset,
	metadata blobstore.Metadata,
) (string, error) {
	result, err := service.materialization().Copy(
		ctx,
		unit.Identity(),
		materialAsset(itemID, asset),
		verifiedMaterial(metadata),
	)
	if err != nil {
		return "", fmt.Errorf("pegasusimport/bind copied asset: %w", err)
	}
	return result, nil
}

func (service *Service) closeAssetWarning(
	ctx context.Context,
	unit work,
	itemID string,
	asset executionAsset,
	code string,
) error {
	if err := service.materialization().Warning(ctx, unit.Identity(), materialAsset(itemID, asset), code); err != nil {
		return fmt.Errorf("pegasusimport/write asset warning: %w", err)
	}
	return nil
}

func (service *Service) updateExecutionPhase(ctx context.Context, unit work, phase string) error {
	if err := service.materialization().SetPhase(
		ctx,
		unit.Identity(),
		phase,
	); err != nil {
		return fmt.Errorf(
			"pegasusimport/update phase: %w",
			err,
		)
	}
	return nil
}

func (service *Service) importCancelled(ctx context.Context, unit work) (bool, error) {
	cancelled, err := service.materialization().Cancelled(ctx, unit.Identity())
	if err != nil {
		return false, fmt.Errorf("pegasusimport/read cancellation: %w", err)
	}
	return cancelled, nil
}
