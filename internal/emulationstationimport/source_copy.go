package emulationstationimport

import (
	"context"
	"fmt"
	"io"

	application "retrom/internal/service/emulationstationimport"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
)

func (service *Sources) copySource(
	ctx context.Context,
	root Root,
	unit application.Execution,
	selectedPath, relativePath string,
	size int64,
	facts string,
) (blobstore.Metadata, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("emulationstationimport/acquire source reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, relativePath)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf(
			"open frozen EmulationStation source: %w: %w",
			application.ErrSourceChanged,
			err,
		)
	}
	if before.Size() != size || serversource.FactsDigest(before) != facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, application.ErrSourceChanged
	}
	metadata, putErr := service.blobs.Put(&contextReader{
		ctx:    ctx,
		reader: io.LimitReader(handle, size+1),
		check:  func() error { return service.checkSourceExecution(ctx, unit) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, fmt.Errorf("emulationstationimport/copy source to CAS: %w", putErr)
	}
	if statErr != nil {
		return blobstore.Metadata{}, fmt.Errorf(
			"stat frozen EmulationStation source: %w: %w",
			application.ErrSourceChanged,
			statErr,
		)
	}
	if metadata.Size != size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != facts {
		return blobstore.Metadata{}, application.ErrSourceChanged
	}
	return metadata, nil
}

func (service *Sources) copyAsset(
	ctx context.Context,
	root Root,
	unit application.Execution,
	selectedPath string,
	asset application.ExecutionAsset,
) (blobstore.Metadata, bool, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("emulationstationimport/acquire asset reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, asset.Path)
	if err != nil {
		return blobstore.Metadata{}, false, fmt.Errorf(
			"open frozen EmulationStation asset: %w: %w",
			application.ErrSourceChanged,
			err,
		)
	}
	if before.Size() != asset.Size || serversource.FactsDigest(before) != asset.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, false, application.ErrSourceChanged
	}
	valid := copiedAssetValid(handle, asset)
	if !valid {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, nil
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, fmt.Errorf("emulationstationimport/rewind asset: %w", err)
	}
	metadata, putErr := service.blobs.Put(&contextReader{
		ctx:    ctx,
		reader: io.LimitReader(handle, asset.Size+1),
		check:  func() error { return service.checkSourceExecution(ctx, unit) },
	})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("emulationstationimport/copy asset to CAS: %w", putErr)
	}
	if statErr != nil {
		return blobstore.Metadata{}, false, fmt.Errorf(
			"stat frozen EmulationStation asset: %w: %w",
			application.ErrSourceChanged,
			statErr,
		)
	}
	if metadata.Size != asset.Size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != asset.Facts {
		return blobstore.Metadata{}, false, application.ErrSourceChanged
	}
	return metadata, true, nil
}

func copiedAssetValid(handle io.ReadSeeker, asset application.ExecutionAsset) bool {
	if asset.Kind == "COVER" {
		image, err := mediaasset.InspectImage(handle, asset.Size)
		return err == nil && image.MediaType == asset.MediaType &&
			asset.Width != nil && asset.Height != nil &&
			image.WidthPX == *asset.Width && image.HeightPX == *asset.Height
	}
	mediaType, err := mediaasset.InspectVideo(handle, asset.Size)
	return err == nil && mediaType == asset.MediaType
}
