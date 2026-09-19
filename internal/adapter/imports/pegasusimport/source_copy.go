package pegasusimport

import (
	"context"
	"fmt"
	"io"

	blobmodel "retrom/internal/model/blob"

	"retrom/internal/adapter/files/mediaasset"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/foundation/cleanup"
)

func (service *Sources) copySource(
	ctx context.Context,
	root Root,
	selectedPath, relativePath string,
	size int64,
	facts string,
) (blobmodel.PreparedBlob, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobmodel.PreparedBlob{}, fmt.Errorf("pegasusimport/acquire source reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, relativePath)
	if err != nil || before.Size() != size || serversource.FactsDigest(before) != facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobmodel.PreparedBlob{}, ErrSourceChanged
	}
	metadata, putErr := service.blobs.Put(contextReader{ctx: ctx, reader: io.LimitReader(handle, size+1)})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobmodel.PreparedBlob{}, fmt.Errorf("pegasusimport/copy source to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != facts {
		return blobmodel.PreparedBlob{}, ErrSourceChanged
	}
	return metadata, nil
}

func (service *Sources) copyAsset(
	ctx context.Context,
	root Root,
	selectedPath string,
	asset executionAsset,
) (blobmodel.PreparedBlob, bool, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobmodel.PreparedBlob{}, false, fmt.Errorf("pegasusimport/acquire asset reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, asset.Path)
	if err != nil || before.Size() != asset.Size || serversource.FactsDigest(before) != asset.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobmodel.PreparedBlob{}, false, ErrSourceChanged
	}
	valid := copiedAssetValid(handle, asset)
	if !valid {
		cleanup.Error("close", handle.Close())
		return blobmodel.PreparedBlob{}, false, nil
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", handle.Close())
		return blobmodel.PreparedBlob{}, false, fmt.Errorf("pegasusimport/rewind asset: %w", err)
	}
	metadata, putErr := service.blobs.Put(contextReader{ctx: ctx, reader: io.LimitReader(handle, asset.Size+1)})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobmodel.PreparedBlob{}, false, fmt.Errorf("pegasusimport/copy asset to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != asset.Size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != asset.Facts {
		return blobmodel.PreparedBlob{}, false, ErrSourceChanged
	}
	return metadata, true, nil
}

func copiedAssetValid(handle io.ReadSeeker, asset executionAsset) bool {
	if asset.Kind == "COVER" {
		image, err := mediaasset.InspectImage(handle, asset.Size)
		return err == nil && image.MediaType == asset.MediaType &&
			asset.Width != nil && asset.Height != nil &&
			image.WidthPX == *asset.Width && image.HeightPX == *asset.Height
	}
	mediaType, err := mediaasset.InspectVideo(handle, asset.Size)
	return err == nil && mediaType == asset.MediaType
}
