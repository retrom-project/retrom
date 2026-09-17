package libraryimport

import (
	"context"
	"fmt"
	"path"
	"strings"

	model "retrom/internal/model/libraryimport"
)

func readReviewMedia(ctx context.Context, scope model.ReviewReadScope, head model.ReviewHead, result *model.ReviewDetail) error {
	var err error
	result.UploadedAssets, err = scope.Media.UploadedAssets(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read uploaded review assets: %w", err)
	}
	media, found, err := scope.Media.SourceMedia(ctx, head.ItemID)
	if err != nil {
		return fmt.Errorf("read review source media: %w", err)
	}
	if found {
		result.SourceMedia = &media
		if result.SourceMedia.Label != nil && *result.SourceMedia.Label == "" {
			result.SourceMedia.Label = nil
		}
		if !result.SourceMedia.HasCover {
			result.SourceMedia.CoverWidthPX = nil
			result.SourceMedia.CoverHeightPX = nil
		}
	}
	result.SourceFiles, err = readReviewSourceFiles(ctx, scope.Sources, head.SnapshotID, head.ContentKind)
	return err
}

func readReviewSourceFiles(
	ctx context.Context,
	reader model.ReviewSourceReader,
	snapshotID, contentKind string,
) ([]model.ReviewSourceFile, error) {
	records, err := reader.Files(ctx, snapshotID)
	if err != nil {
		return nil, fmt.Errorf("read review source files: %w", err)
	}
	files := make([]model.ReviewSourceFile, 0, len(records))
	for _, record := range records {
		file := model.ReviewSourceFile{
			ID: record.ID, Name: record.Name, SizeBytes: record.SizeBytes, SHA256: record.SHA256, MD5: record.MD5,
			CRC32: record.CRC32, Archive: record.Archive, ArchiveEntries: []model.ReviewArchiveEntry{},
		}
		if record.ArchiveBlobID != nil {
			archive, err := reader.ArchiveEntries(ctx, *record.ArchiveBlobID)
			if err != nil {
				return nil, fmt.Errorf("read review archive entries: %w", err)
			}
			file.ArchiveEntries = archive.Entries
			file.ArchiveFormat = ProjectReviewArchiveFormat(contentKind, record.Name, archive.Format)
		}
		files = append(files, file)
	}
	return files, nil
}

func ProjectReviewArchiveFormat(contentKind, name string, format *string) *string {
	if contentKind == "TYRANOSCRIPT_PROJECT" &&
		format != nil &&
		*format == "ZIP" &&
		strings.EqualFold(path.Ext(name),
			".exe") {
		value := "NWJS_EXECUTABLE"
		return &value
	}
	return format
}
