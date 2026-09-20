package importing

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"retrom/internal/foundation/cleanup"
)

var ErrElectronASARInvalid = fmt.Errorf("%w: ELECTRON_ASAR_INVALID", ErrArchiveUnsafe)

type electronZIPArchive struct {
	file   *os.File
	reader *zip.Reader
	layout electronZIPLayout
}

// DetectElectronASARZIP recognizes a Windows Electron distribution by its
// sibling executable and resources/app.asar without executing either file.
func DetectElectronASARZIP(pathValue string, limits ArchiveLimits) (bool, error) {
	archive, detected, err := openElectronZIP(pathValue, limits)
	if archive != nil {
		cleanup.Error("close", archive.file.Close())
	}
	return detected, err
}

// ScanElectronASARZIPWithConsumer validates the outer ZIP and inner ASAR, then
// streams each virtual application file exactly once into the shared consumer.
func ScanElectronASARZIPWithConsumer(
	ctx context.Context,
	pathValue string,
	limits ArchiveLimits,
	consumer ArchiveContentConsumer,
) ([]ArchiveEntry, error) {
	if consumer == nil {
		return nil, ErrArchiveUnsafe
	}
	archive, detected, err := openElectronZIP(pathValue, limits)
	if err != nil {
		return nil, err
	}
	if archive != nil {
		defer func() { cleanup.Error("close", archive.file.Close()) }()
	}
	if archive == nil || !detected {
		return nil, ErrElectronASARInvalid
	}
	return archive.scan(ctx, limits, consumer)
}

func openElectronZIP(pathValue string, limits ArchiveLimits) (*electronZIPArchive, bool, error) {
	file, err := os.Open(pathValue)
	if err != nil {
		return nil, false, fmt.Errorf("open Electron ZIP: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		cleanup.Error("close", file.Close())
		return nil, false, ErrArchiveUnsafe
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		cleanup.Error("close", file.Close())
		return nil, false, fmt.Errorf("%w: invalid Electron ZIP", ErrArchiveUnsafe)
	}
	headers, err := electronZIPHeaders(reader, limits)
	if err != nil {
		cleanup.Error("close", file.Close())
		return nil, false, err
	}
	items, err := validateElectronZIPDirectory(headers, limits)
	if err != nil {
		cleanup.Error("close", file.Close())
		return nil, false, err
	}
	layout, detected, err := locateElectronZIPLayout(items)
	if err != nil {
		cleanup.Error("close", file.Close())
		return nil, false, err
	}
	return &electronZIPArchive{file: file, reader: reader, layout: layout}, detected, nil
}

func (archive *electronZIPArchive) scan(
	ctx context.Context,
	limits ArchiveLimits,
	consumer ArchiveContentConsumer,
) ([]ArchiveEntry, error) {
	reader, err := archive.reader.File[archive.layout.appASAR.ordinal].Open()
	if err != nil {
		return nil, fmt.Errorf("%w: open app.asar", ErrElectronASARInvalid)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	appSize, ok := checkedArchiveSize(archive.layout.appASAR.header.UncompressedSize64)
	if !ok {
		return nil, ErrArchiveLimitExceeded
	}
	monitor := &archiveReadMonitor{ctx: ctx, reader: reader, limit: limits.MaxEntryBytes}
	members, dataOffset, err := readASARHeader(monitor, appSize, limits)
	if err != nil {
		return nil, err
	}
	if err := validateUnpackedASARMembers(members, archive.layout); err != nil {
		return nil, err
	}
	entries, position, err := consumePackedASARMembers(
		ctx, monitor, members, archive.layout.appASAR.header.Method, consumer,
	)
	if err != nil {
		return nil, err
	}
	remaining := appSize - dataOffset - position
	if remaining < 0 || copyExact(monitor, remaining) != nil {
		return nil, ErrElectronASARInvalid
	}
	if _, err := io.Copy(io.Discard, monitor); err != nil || monitor.written != appSize {
		return nil, ErrElectronASARInvalid
	}
	if err := archive.consumeUnpackedMembers(ctx, members, consumer, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func consumePackedASARMembers(
	ctx context.Context,
	reader io.Reader,
	members []asarMember,
	method uint16,
	consumer ArchiveContentConsumer,
) ([]ArchiveEntry, int64, error) {
	entries := make([]ArchiveEntry, len(members))
	packed := append([]asarMember(nil), members...)
	sortASARMembersByOffset(packed)
	var position int64
	for _, member := range packed {
		if member.unpacked {
			continue
		}
		if err := copyExact(reader, member.offset-position); err != nil {
			return nil, 0, ErrElectronASARInvalid
		}
		entry, err := consumeASARMember(ctx, reader, member, electronASARCompressionProfile(method), consumer)
		if err != nil {
			return nil, 0, err
		}
		entries[member.ordinal] = entry
		position = member.offset + member.size
	}
	return entries, position, nil
}

func (archive *electronZIPArchive) consumeUnpackedMembers(
	ctx context.Context,
	members []asarMember,
	consumer ArchiveContentConsumer,
	entries []ArchiveEntry,
) error {
	for _, member := range members {
		if !member.unpacked {
			continue
		}
		item := archive.layout.unpacked[ASCIICaseFold(member.path)]
		reader, err := archive.reader.File[item.ordinal].Open()
		if err != nil {
			return ErrElectronASARInvalid
		}
		entry, consumeErr := consumeASARMember(
			ctx, reader, member, electronASARCompressionProfile(item.header.Method), consumer,
		)
		if consumeErr == nil {
			consumeErr = expectEOF(reader)
		}
		closeErr := reader.Close()
		if consumeErr != nil || closeErr != nil {
			return errors.Join(consumeErr, closeErr, ErrElectronASARInvalid)
		}
		if entry.CRC32 != fmt.Sprintf("%08x", item.header.CRC32) {
			return ErrElectronASARInvalid
		}
		entries[member.ordinal] = entry
	}
	return nil
}

func consumeASARMember(
	ctx context.Context,
	reader io.Reader,
	member asarMember,
	compressionProfile string,
	consumer ArchiveContentConsumer,
) (ArchiveEntry, error) {
	limited := io.LimitReader(reader, member.size)
	monitor := &archiveReadMonitor{ctx: ctx, reader: limited, limit: member.size}
	entry := ArchiveEntry{
		Ordinal: member.ordinal, OriginalPath: member.path, NormalizedPath: member.path,
		ASCIICasefoldPath: ASCIICaseFold(member.path), ArchiveFormat: "ELECTRON_ASAR",
		CompressionProfile: compressionProfile,
	}
	content, err := consumer(entry, monitor)
	if err != nil {
		return ArchiveEntry{}, err
	}
	if monitor.written != member.size || content.Size != member.size ||
		content.CRC32 == "" || content.MD5 == "" || content.SHA1 == "" || content.SHA256 == "" {
		return ArchiveEntry{}, ErrElectronASARInvalid
	}
	if err := validateASARIntegrity(member, content); err != nil {
		return ArchiveEntry{}, err
	}
	entry.Size = content.Size
	entry.CRC32, entry.MD5 = content.CRC32, content.MD5
	entry.SHA1, entry.SHA256 = content.SHA1, content.SHA256
	entry.NestedArchive = DetectNestedArchive(
		member.path, monitor.prefix[:min(int64(len(monitor.prefix)), monitor.written)],
	)
	return entry, nil
}

func sortASARMembersByOffset(members []asarMember) {
	sort.Slice(members, func(left, right int) bool {
		if members[left].unpacked != members[right].unpacked {
			return !members[left].unpacked
		}
		if members[left].offset != members[right].offset {
			return members[left].offset < members[right].offset
		}
		return members[left].path < members[right].path
	})
}

func electronASARCompressionProfile(method uint16) string {
	if method == zip.Store {
		return "ELECTRON_ASAR_STORE"
	}
	return "ELECTRON_ASAR_DEFLATE"
}

func copyExact(reader io.Reader, size int64) error {
	if size < 0 {
		return ErrElectronASARInvalid
	}
	written, err := io.CopyN(io.Discard, reader, size)
	if err != nil || written != size {
		return ErrElectronASARInvalid
	}
	return nil
}

func expectEOF(reader io.Reader) error {
	var single [1]byte
	count, err := reader.Read(single[:])
	if count != 0 || !errors.Is(err, io.EOF) {
		return ErrElectronASARInvalid
	}
	return nil
}

func invalidElectronASAR(reason string) error {
	return fmt.Errorf("%w: %s", ErrElectronASARInvalid, reason)
}

func electronZIPHeaders(reader *zip.Reader, limits ArchiveLimits) ([]zipHeaderFacts, error) {
	if err := validateZIPEntryCount(len(reader.File), limits); err != nil {
		return nil, err
	}
	headers := make([]zipHeaderFacts, len(reader.File))
	for ordinal, item := range reader.File {
		headers[ordinal] = zipHeaderFactsFromFile(item)
	}
	return headers, nil
}
