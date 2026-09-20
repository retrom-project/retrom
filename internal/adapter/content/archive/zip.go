package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"

	"retrom/internal/capability/format/importing"
	"retrom/internal/model/diagnostics"
)

type zipArchive struct {
	file    io.Closer
	reader  *zip.Reader
	members []importing.ZIPMember
}

func (factory *Factory) loadZIP(ctx context.Context, path string, limits importing.ArchiveLimits) (*zipArchive, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		closeResource(ctx, factory.reporter, file)
		return nil, fmt.Errorf("stat zip: %w", err)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		closeResource(ctx, factory.reporter, file)
		return nil, fmt.Errorf("%w: invalid zip", importing.ErrArchiveUnsafe)
	}
	members, err := validateZIPDirectory(ctx, reader, limits, true)
	if err != nil {
		closeResource(ctx, factory.reporter, file)
		return nil, err
	}
	return &zipArchive{file: file, reader: reader, members: members}, nil
}

func validateZIPDirectory(
	ctx context.Context, reader *zip.Reader, limits importing.ArchiveLimits, checkContext bool,
) ([]importing.ZIPMember, error) {
	directory, err := importing.NewZIPDirectory(len(reader.File), limits)
	if err != nil {
		return nil, importing.ErrArchiveLimitExceeded
	}
	members := make([]importing.ZIPMember, 0, len(reader.File))
	for ordinal, item := range reader.File {
		if checkContext {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("importing/archive: %w", err)
			}
		}
		member, isDirectory, err := directory.Add(ordinal, zipHeaderFactsFromFile(item))
		if err != nil {
			return nil, err
		}
		if !isDirectory {
			members = append(members, member)
		}
	}
	return members, nil
}

func zipHeaderFactsFromFile(item *zip.File) importing.ZIPHeaderFacts {
	return importing.ZIPHeaderFacts{
		Name: item.Name, NonUTF8: item.NonUTF8, Mode: item.Mode(), ExternalAttrs: item.ExternalAttrs,
		Flags: item.Flags, Method: item.Method, CompressedSize64: item.CompressedSize64,
		UncompressedSize64: item.UncompressedSize64, CRC32: item.CRC32,
	}
}

func closeZIP(ctx context.Context, reporter diagnostics.ErrorReporter, archive *zipArchive) {
	if archive.file != nil {
		file := archive.file
		archive.file = nil
		closeResource(ctx, reporter, file)
	}
}
