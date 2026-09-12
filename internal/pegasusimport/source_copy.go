package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/libraryimport"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
)

func (service *Service) executionSourceFiles(
	ctx context.Context,
	unit work,
	root Root,
	item executionItem,
) ([]libraryimport.ServerSourceFile, error) {
	files := make([]libraryimport.ServerSourceFile, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(
			files,
			libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: file.BlobID, SizeBytes: file.Size},
		)
	}
	arcadeArchive := item.TargetPlatformKind == "arcade" && len(item.Files) == 1 &&
		strings.EqualFold(path.Ext(item.Files[0].Path), ".zip")
	if !arcadeArchive {
		return files, nil
	}
	companions, err := service.arcadeCompanions(ctx, unit, root, item)
	if err != nil {
		return nil, err
	}
	return append(files, companions...), nil
}

func (service *Service) arcadeCompanions(
	ctx context.Context,
	unit work,
	root Root,
	item executionItem,
) ([]libraryimport.ServerSourceFile, error) {
	companions := application.NewCompanions(repository.NewCompanions(service.database), service.now)
	candidates, err := companions.Find(ctx, unit.Identity(), item.ID)
	if err != nil {
		return nil, fmt.Errorf("pegasusimport/read companions: %w", err)
	}
	result := make([]libraryimport.ServerSourceFile, 0, len(candidates))
	for _, candidate := range candidates {
		file := candidate.File
		metadata, err := service.copySource(ctx, root, unit.RelativePath, file.Path, file.Size, file.Facts)
		if errors.Is(err, ErrSourceChanged) {
			continue
		}
		if err != nil {
			return nil, err
		}
		blobID, err := companions.Record(ctx, unit.Identity(), item.ID, candidate, verifiedMaterial(metadata))
		if err != nil {
			return nil, fmt.Errorf("pegasusimport/register companion: %w", err)
		}
		result = append(result, libraryimport.ServerSourceFile{RelativePath: file.Path, BlobID: blobID, SizeBytes: file.Size})
	}
	return result, nil
}

func terminalForCode(code string) string {
	if code == "PEGASUS_SOURCE_CHANGED" {
		return "SOURCE_CHANGED"
	}
	return "READ_FAILED"
}

func (service *Service) copySource(
	ctx context.Context,
	root Root,
	selectedPath, relativePath string,
	size int64,
	facts string,
) (blobstore.Metadata, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("pegasusimport/acquire source reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, relativePath)
	if err != nil || before.Size() != size || serversource.FactsDigest(before) != facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, ErrSourceChanged
	}
	metadata, putErr := service.blobs.Put(contextReader{ctx: ctx, reader: io.LimitReader(handle, size+1)})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, fmt.Errorf("pegasusimport/copy source to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != facts {
		return blobstore.Metadata{}, ErrSourceChanged
	}
	return metadata, nil
}

func (service *Service) copyAsset(
	ctx context.Context,
	root Root,
	selectedPath string,
	asset executionAsset,
) (blobstore.Metadata, bool, error) {
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("pegasusimport/acquire asset reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, asset.Path)
	if err != nil || before.Size() != asset.Size || serversource.FactsDigest(before) != asset.Facts {
		if handle != nil {
			cleanup.Error("close", handle.Close())
		}
		return blobstore.Metadata{}, false, ErrSourceChanged
	}
	valid := copiedAssetValid(handle, asset)
	if !valid {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, nil
	}
	if _, err := handle.Seek(0, io.SeekStart); err != nil {
		cleanup.Error("close", handle.Close())
		return blobstore.Metadata{}, false, fmt.Errorf("pegasusimport/rewind asset: %w", err)
	}
	metadata, putErr := service.blobs.Put(contextReader{ctx: ctx, reader: io.LimitReader(handle, asset.Size+1)})
	after, statErr := handle.Stat()
	cleanup.Error("close", handle.Close())
	if putErr != nil {
		return blobstore.Metadata{}, false, fmt.Errorf("pegasusimport/copy asset to CAS: %w", putErr)
	}
	if statErr != nil || metadata.Size != asset.Size || !serversource.SameFileFacts(before, after) ||
		serversource.FactsDigest(after) != asset.Facts {
		return blobstore.Metadata{}, false, ErrSourceChanged
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
