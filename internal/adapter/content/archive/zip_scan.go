package archive

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"

	"retrom/internal/capability/format/importing"
	"retrom/internal/foundation/legacychecksum"
	"retrom/internal/model/diagnostics"
)

func (factory *Factory) ScanZIP(
	ctx context.Context, path string, limits importing.ArchiveLimits,
) ([]importing.ArchiveEntry, error) {
	archive, err := factory.loadZIP(ctx, path, limits)
	if err != nil {
		return nil, err
	}
	defer closeZIP(ctx, factory.reporter, archive)
	entries := make([]importing.ArchiveEntry, 0, len(archive.members))
	for _, member := range archive.members {
		entry, err := readZIPEntry(ctx, archive.reader.File[member.Ordinal], member, factory.reporter,
			limits.MaxEntryBytes, limits.AllowNestedArchives)
		if err != nil {
			return nil, fmt.Errorf("scan archive entry %q: %w", member.Path, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ScanFlatZIP preserves the original two-pass ordering: stream validation first,
// then reopen the archive and reject directory or non-root entries.
func (factory *Factory) ScanFlatZIP(
	ctx context.Context, path string, limits importing.ArchiveLimits,
) ([]importing.ArchiveEntry, error) {
	entries, err := factory.ScanZIP(ctx, path, limits)
	if err != nil {
		return nil, err
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid zip", importing.ErrArchiveUnsafe)
	}
	defer closeResource(ctx, factory.reporter, reader)
	for _, item := range reader.File {
		if err := importing.ValidateFlatZIPHeader(zipHeaderFactsFromFile(item)); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

func readZIPEntry(
	ctx context.Context,
	item *zip.File,
	member importing.ZIPMember,
	reporter diagnostics.ErrorReporter,
	limit int64,
	allowNestedArchives bool,
) (importing.ArchiveEntry, error) {
	reader, err := item.Open()
	if err != nil {
		return importing.ArchiveEntry{}, fmt.Errorf("%w: open entry", importing.ErrArchiveUnsafe)
	}
	defer func() { closeResource(ctx, reporter, reader) }()
	sha256Hash := sha256.New()
	legacyHashes := legacychecksum.New()
	crc32Hash := crc32.NewIEEE()
	prefix := make([]byte, 512)
	written := int64(0)
	buffer := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return importing.ArchiveEntry{}, fmt.Errorf("importing/archive: %w", err)
		}
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if written+int64(count) > limit {
				return importing.ArchiveEntry{}, importing.ErrArchiveLimitExceeded
			}
			if written < int64(len(prefix)) {
				copy(prefix[written:], buffer[:count])
			}
			_, _ = io.MultiWriter(sha256Hash, legacyHashes.MD5, legacyHashes.SHA1, crc32Hash).Write(buffer[:count])
			written += int64(count)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return importing.ArchiveEntry{}, fmt.Errorf("%w: read entry", importing.ErrArchiveUnsafe)
		}
	}
	return member.Scanned(importing.ArchiveContent{
		Size: written, CRC32: hex.EncodeToString(crc32Hash.Sum(nil)),
		MD5: hex.EncodeToString(legacyHashes.MD5.Sum(nil)), SHA1: hex.EncodeToString(legacyHashes.SHA1.Sum(nil)),
		SHA256: hex.EncodeToString(sha256Hash.Sum(nil)),
	}, prefix[:min(int64(len(prefix)), written)], allowNestedArchives)
}
