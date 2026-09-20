package importing

import (
	"archive/zip"
	"io/fs"
	"math"
	"strings"

	"retrom/internal/capability/format/zipentry"
)

// ZIPHeaderFacts contains only the directory metadata used by ZIP rules.
type ZIPHeaderFacts struct {
	Name               string
	NonUTF8            bool
	Mode               fs.FileMode
	ExternalAttrs      uint32
	Flags              uint16
	Method             uint16
	CompressedSize64   uint64
	UncompressedSize64 uint64
	CRC32              uint32
}

func validateZIPEntryCount(count int, limits ArchiveLimits) error {
	if count > limits.MaxEntries {
		return ErrArchiveLimitExceeded
	}
	return nil
}

func checkedArchiveSize(value uint64) (int64, bool) {
	if value > uint64(math.MaxInt64) {
		return 0, false
	}
	return int64(value), true
}

func validateZIPItem(item ZIPHeaderFacts, limits ArchiveLimits, expanded int64) (string, bool, error) {
	entryName, err := zipEntryName(item)
	if err != nil {
		return "", false, err
	}
	pathValue, directory, err := archivePath(entryName)
	if err != nil {
		return "", false, err
	}
	mode := item.Mode
	if mode&fs.ModeSymlink != 0 || mode&fs.ModeType != 0 && !mode.IsDir() {
		return "", false, ErrArchiveUnsafe
	}
	if directory {
		if !mode.IsDir() && item.ExternalAttrs != 0 {
			return "", false, ErrArchiveUnsafe
		}
		return pathValue, true, nil
	}
	if item.Flags&0x1 != 0 {
		return "", false, ErrArchiveEncrypted
	}
	if item.Method != zip.Store && item.Method != zip.Deflate {
		return "", false, ErrArchiveMethodUnsupported
	}
	if invalidZIPSize(item, limits, expanded) {
		return "", false, ErrArchiveLimitExceeded
	}
	return pathValue, false, nil
}

func invalidZIPSize(item ZIPHeaderFacts, limits ArchiveLimits, expanded int64) bool {
	if limits.MaxEntryBytes < 0 || item.UncompressedSize64 > uint64(limits.MaxEntryBytes) {
		return true
	}
	invalidRatio := item.CompressedSize64 == 0 && item.UncompressedSize64 > 0 ||
		item.CompressedSize64 > 0 && (limits.MaxCompressionRatio < 0 ||
			item.UncompressedSize64/item.CompressedSize64 > uint64(limits.MaxCompressionRatio)) &&
			item.UncompressedSize64 > 16<<20
	return invalidRatio || item.UncompressedSize64 > ^uint64(0)>>1 ||
		int64(item.UncompressedSize64) > limits.MaxExpandedBytes-expanded
}

func recordArchivePath(seenPath, seenFold map[string]struct{}, pathValue, folded string) error {
	if _, exists := seenPath[pathValue]; exists {
		return ErrArchiveUnsafe
	}
	if _, exists := seenFold[folded]; exists {
		return ErrArchiveCasefoldCollision
	}
	seenPath[pathValue] = struct{}{}
	seenFold[folded] = struct{}{}
	return nil
}

func zipEntryName(item ZIPHeaderFacts) (string, error) {
	decoded, err := zipentry.DecodeName(item.Name, item.NonUTF8)
	if err != nil {
		return "", ErrArchiveUnsafe
	}
	return decoded, nil
}

func archivePath(value string) (string, bool, error) {
	directory := strings.HasSuffix(value, "/")
	if directory {
		if strings.HasSuffix(value, "//") {
			return "", false, ErrUnsafeLogicalPath
		}
		value = strings.TrimSuffix(value, "/")
	}
	validated, err := ValidateLogicalPath(value)
	if err != nil {
		return "", false, err
	}
	return validated, directory, nil
}

func zipCompressionProfile(method uint16) string {
	if method == zip.Store {
		return "STORE"
	}
	return "DEFLATE"
}
