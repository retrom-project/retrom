package sourceimport

import (
	"context"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/mediaasset"
	"retrom/internal/serversource"
	application "retrom/internal/service/sourceimport"
)

func (source scanSource) Asset(
	ctx context.Context,
	file application.DiscoveredFile,
	kind string,
) (application.ScanAssetInspection, error) {
	release, err := source.acquire(ctx)
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("acquire Source media reader: %w", err)
	}
	defer release()
	handle, before, err := serversource.OpenRelativeFile(source.root.path, source.selectedPath, file.Path)
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("%w: %w", ErrSourceChanged, err)
	}
	defer func() { cleanup.Error("close Source media source", handle.Close()) }()
	if before.Size() != file.Size || serversource.FactsDigest(before) != file.Facts {
		return application.ScanAssetInspection{}, ErrSourceChanged
	}
	result := application.ScanAssetInspection{}
	reader := scanReadSeeker{contextReader: contextReader{ctx: ctx, reader: handle}, seeker: handle}
	if kind == "COVER" {
		value, inspectErr := mediaasset.InspectImage(reader, file.Size)
		err = inspectErr
		result.MediaType = value.MediaType
		result.Width, result.Height = &value.WidthPX, &value.HeightPX
	} else {
		result.MediaType, err = mediaasset.InspectVideo(reader, file.Size)
	}
	if ctx.Err() != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("inspect Source media: %w", ctx.Err())
	}
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("inspect Source media: %w", err)
	}
	after, err := handle.Stat()
	if err != nil {
		return application.ScanAssetInspection{}, fmt.Errorf("stat Source media: %w", err)
	}
	if !serversource.SameFileFacts(before, after) {
		return application.ScanAssetInspection{}, ErrSourceChanged
	}
	return result, nil
}

type scanReadSeeker struct {
	contextReader
	seeker io.Seeker
}

func (reader scanReadSeeker) Seek(offset int64, whence int) (int64, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, fmt.Errorf("seek cancelled Source media: %w", err)
	}
	position, err := reader.seeker.Seek(offset, whence)
	if err != nil {
		return 0, fmt.Errorf("seek Source media: %w", err)
	}
	return position, nil
}
