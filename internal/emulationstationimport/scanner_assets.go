package emulationstationimport

import (
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
	application "retrom/internal/service/emulationstationimport"
)

func (source scannerSource) Asset(
	ctx context.Context,
	file application.DiscoveredFile,
	kind string,
) (application.ScanAssetInspection, error) {
	if err := ctx.Err(); err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("inspect EmulationStation scan media: %w", err)
	}
	release, err := serversource.AcquireReader(ctx)
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf(
			"acquire EmulationStation media reader: %w: %w", application.ErrScanReadFailed, err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(source.root.path, source.selectedPath, file.Path)
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf(
			"open EmulationStation scan media: %w: %w", application.ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close", handle.Close()) }()
	if before.Size() != file.Size || serversource.FactsDigest(before) != file.Facts {
		return application.ScanAssetInspection{}, application.ErrSourceChanged
	}
	result, inspectErr := inspectScanAsset(ctx, handle, file.Size, kind)
	after, statErr := handle.Stat()
	if err := ctx.Err(); err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf(
			"stop EmulationStation media inspection: %w", errors.Join(err, inspectErr, statErr))
	}
	if inspectErr != nil || statErr != nil {
		return application.ScanAssetInspection{}, fmt.Errorf(
			"inspect EmulationStation frozen media: %w", errors.Join(inspectErr, statErr))
	}
	if !serversource.SameFileFacts(before, after) {
		return application.ScanAssetInspection{Changed: true}, nil
	}
	return result, nil
}

func inspectScanAsset(
	ctx context.Context,
	handle io.ReadSeeker,
	size int64,
	kind string,
) (application.ScanAssetInspection, error) {
	reader := scanReadSeeker{contextReader: contextReader{ctx: ctx, reader: handle}, seeker: handle}
	if kind == "COVER" {
		image, err := mediaasset.InspectImage(&reader, size)
		if err != nil {
			return application.ScanAssetInspection{}, fmt.Errorf("inspect EmulationStation cover: %w", err)
		}
		return application.ScanAssetInspection{
			MediaType: image.MediaType, Width: &image.WidthPX, Height: &image.HeightPX,
		}, nil
	}
	mediaType, err := mediaasset.InspectVideo(&reader, size)
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("inspect EmulationStation video: %w", err)
	}
	return application.ScanAssetInspection{MediaType: mediaType}, nil
}

type scanReadSeeker struct {
	contextReader
	seeker io.Seeker
}

func (reader *scanReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, fmt.Errorf("seek cancelled EmulationStation media: %w", err)
	}
	position, err := reader.seeker.Seek(offset, whence)
	if err != nil {
		return 0, fmt.Errorf("seek EmulationStation media: %w", err)
	}
	return position, nil
}
