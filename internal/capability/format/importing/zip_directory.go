package importing

import (
	"fmt"
	"strings"
)

// ZIPMember is one validated regular central-directory entry.
type ZIPMember struct {
	Ordinal    int
	Header     ZIPHeaderFacts
	Path       string
	FoldedPath string
}

// ZIPDirectory incrementally validates metadata without retaining archive resources.
type ZIPDirectory struct {
	limits   ArchiveLimits
	total    int64
	seenPath map[string]struct{}
	seenFold map[string]struct{}
}

func NewZIPDirectory(count int, limits ArchiveLimits) (*ZIPDirectory, error) {
	if err := validateZIPEntryCount(count, limits); err != nil {
		return nil, err
	}
	return &ZIPDirectory{
		limits: limits, seenPath: make(map[string]struct{}, count), seenFold: make(map[string]struct{}, count),
	}, nil
}

func (directory *ZIPDirectory) Add(ordinal int, header ZIPHeaderFacts) (ZIPMember, bool, error) {
	pathValue, isDirectory, err := validateZIPItem(header, directory.limits, directory.total)
	if err != nil {
		return ZIPMember{}, false, err
	}
	if isDirectory {
		return ZIPMember{}, true, nil
	}
	expanded, ok := checkedArchiveSize(header.UncompressedSize64)
	if !ok {
		return ZIPMember{}, false, ErrArchiveLimitExceeded
	}
	directory.total += expanded
	folded := ASCIICaseFold(pathValue)
	if err := recordArchivePath(directory.seenPath, directory.seenFold, pathValue, folded); err != nil {
		return ZIPMember{}, false, err
	}
	return ZIPMember{Ordinal: ordinal, Header: header, Path: pathValue, FoldedPath: folded}, false, nil
}

func (member ZIPMember) Entry() ArchiveEntry {
	return ArchiveEntry{
		Ordinal: member.Ordinal, OriginalPath: member.Path, NormalizedPath: member.Path,
		ASCIICasefoldPath: member.FoldedPath, ArchiveFormat: "ZIP",
		CompressionProfile: zipCompressionProfile(member.Header.Method),
	}
}

// Complete validates the consumer's metadata against the bytes and central directory.
func (member ZIPMember) Complete(
	content ArchiveContent, written int64, prefix []byte, allowNested bool,
) (ArchiveEntry, error) {
	if written != int64(member.Header.UncompressedSize64) || content.Size != written ||
		content.CRC32 != fmt.Sprintf("%08x", member.Header.CRC32) {
		return ArchiveEntry{}, ErrArchiveUnsafe
	}
	return member.complete(content, prefix, allowNested)
}

// Scanned validates the legacy closed-result scanner's byte count. Its ZIP reader
// already verifies CRC; it does not reinterpret consumer-supplied evidence.
func (member ZIPMember) Scanned(content ArchiveContent, prefix []byte, allowNested bool) (ArchiveEntry, error) {
	if member.Header.UncompressedSize64 > ^uint64(0)>>1 || content.Size != int64(member.Header.UncompressedSize64) {
		return ArchiveEntry{}, ErrArchiveUnsafe
	}
	return member.complete(content, prefix, allowNested)
}

func (member ZIPMember) complete(content ArchiveContent, prefix []byte, allowNested bool) (ArchiveEntry, error) {
	nested := DetectNestedArchive(member.Path, prefix)
	if !allowNested && nested != NestedArchiveNone {
		return ArchiveEntry{}, ErrNestedArchiveUnsupported
	}
	entry := member.Entry()
	entry.Size = content.Size
	entry.CRC32, entry.MD5 = content.CRC32, content.MD5
	entry.SHA1, entry.SHA256 = content.SHA1, content.SHA256
	entry.NestedArchive = nested
	return entry, nil
}

// ValidateFlatZIPHeader is applied after the complete legacy ZIP scan.
func ValidateFlatZIPHeader(header ZIPHeaderFacts) error {
	name, err := zipEntryName(header)
	if err != nil {
		return err
	}
	normalized, directory, err := archivePath(name)
	if err != nil {
		return err
	}
	if directory || strings.Contains(normalized, "/") {
		return ErrNestedArchiveUnsupported
	}
	return nil
}
