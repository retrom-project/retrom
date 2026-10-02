package prepare

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"

	"retrom/internal/cleanup"
	"retrom/internal/filestore"
	"retrom/internal/importing"
)

func Materialize(ctx context.Context, files Files, archivePath string,
	expected importing.ArchiveEntry,
) (filestore.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return filestore.Metadata{}, fmt.Errorf("materialize content: %w", err)
	}
	var metadata filestore.Metadata
	var putErr, closeErr error
	switch expected.ArchiveFormat {
	case "ZIP":
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return filestore.Metadata{}, importing.ErrArchiveUnsafe
		}
		defer func() { cleanup.Error("close content archive", reader.Close()) }()
		if expected.Ordinal < 0 || expected.Ordinal >= len(reader.File) {
			return filestore.Metadata{}, importing.ErrArchiveUnsafe
		}
		entry, err := reader.File[expected.Ordinal].Open()
		if err != nil {
			return filestore.Metadata{}, importing.ErrArchiveUnsafe
		}
		metadata, putErr = files.Put(io.LimitReader(entry, expected.Size+1))
		closeErr = entry.Close()
	case "SEVEN_Z":
		reader, writer := io.Pipe()
		done := make(chan error, 1)
		go func() {
			extractErr := importing.ExtractSevenZip(ctx, archivePath, expected, writer)
			_ = writer.CloseWithError(extractErr)
			done <- extractErr
		}()
		metadata, putErr = files.Put(io.LimitReader(reader, expected.Size+1))
		closeErr = errors.Join(reader.Close(), <-done)
	default:
		return filestore.Metadata{}, importing.ErrArchiveUnsafe
	}
	if putErr != nil {
		return filestore.Metadata{}, fmt.Errorf("store archive content: %w", putErr)
	}
	if closeErr != nil || metadata.Size != expected.Size || metadata.CRC32 != expected.CRC32 ||
		metadata.MD5 != expected.MD5 || metadata.SHA1 != expected.SHA1 || metadata.SHA256 != expected.SHA256 {
		return filestore.Metadata{}, importing.ErrArchiveUnsafe
	}
	return metadata, nil
}
