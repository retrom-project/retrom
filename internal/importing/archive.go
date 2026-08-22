package importing

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"retrom/internal/cleanup"
	"retrom/internal/legacychecksum"
	"retrom/internal/zipentry"
)

var (
	ErrArchiveUnsafe             = errors.New("ARCHIVE_UNSAFE")
	ErrArchiveLimitExceeded      = errors.New("ARCHIVE_LIMIT_EXCEEDED")
	ErrArchiveEncrypted          = errors.New("ARCHIVE_ENCRYPTED_UNSUPPORTED")
	ErrArchiveVolumeUnsupported  = errors.New("ARCHIVE_VOLUME_UNSUPPORTED")
	ErrArchiveResourceLimit      = errors.New("ARCHIVE_RESOURCE_LIMIT")
	ErrArchiveSandboxUnavailable = errors.New("ARCHIVE_SANDBOX_UNAVAILABLE")
	ErrNestedArchiveUnsupported  = errors.New("NESTED_ARCHIVE_UNSUPPORTED")
	ErrArchiveMethodUnsupported  = errors.New("ARCHIVE_METHOD_UNSUPPORTED")
	ErrArchiveCasefoldCollision  = errors.New("ARCHIVE_CASEFOLD_COLLISION")
)

type ArchiveLimits struct {
	MaxEntries          int
	MaxEntryBytes       int64
	MaxExpandedBytes    int64
	MaxCompressionRatio int64
	AllowNestedArchives bool
}

func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{
		MaxEntries:          20000,
		MaxEntryBytes:       8 << 30,
		MaxExpandedBytes:    32 << 30,
		MaxCompressionRatio: 200,
	}
}

func DOSArchiveLimits() ArchiveLimits {
	limits := DefaultArchiveLimits()
	limits.AllowNestedArchives = true
	return limits
}

type ArchiveEntry struct {
	Ordinal            int
	OriginalPath       string
	NormalizedPath     string
	ASCIICasefoldPath  string
	ArchiveFormat      string
	CompressionProfile string
	Size               int64
	CRC32              string
	MD5                string
	SHA1               string
	SHA256             string
}

func ScanZIP(ctx context.Context, path string, limits ArchiveLimits) ([]ArchiveEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat zip: %w", err)
	}
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid zip", ErrArchiveUnsafe)
	}
	if len(reader.File) > limits.MaxEntries {
		return nil, ErrArchiveLimitExceeded
	}
	seenPath := make(map[string]struct{}, len(reader.File))
	seenFold := make(map[string]struct{}, len(reader.File))
	result := make([]ArchiveEntry, 0, len(reader.File))
	var total int64
	for ordinal, item := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("importing/archive: %w", err)
		}
		pathValue, directory, err := validateZIPItem(item, limits, total)
		if err != nil {
			return nil, err
		}
		if directory {
			continue
		}
		expanded, ok := checkedArchiveSize(item.UncompressedSize64)
		if !ok {
			return nil, ErrArchiveLimitExceeded
		}
		total += expanded
		folded := ASCIICaseFold(pathValue)
		if err := recordArchivePath(seenPath, seenFold, pathValue, folded); err != nil {
			return nil, err
		}
		entry, err := readArchiveEntry(
			ctx, item, ordinal, pathValue, folded, limits.MaxEntryBytes, limits.AllowNestedArchives,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

func checkedArchiveSize(value uint64) (int64, bool) {
	if value > uint64(math.MaxInt64) {
		return 0, false
	}
	return int64(value), true
}

func validateZIPItem(item *zip.File, limits ArchiveLimits, expanded int64) (string, bool, error) {
	entryName, err := zipEntryName(item)
	if err != nil {
		return "", false, err
	}
	pathValue, directory, err := archivePath(entryName)
	if err != nil {
		return "", false, err
	}
	mode := item.Mode()
	if mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !mode.IsDir() {
		return "", false, ErrArchiveUnsafe
	}
	if directory {
		if !mode.IsDir() && item.ExternalAttrs != 0 {
			return "", false, ErrArchiveUnsafe
		}
		return pathValue, true, nil
	}
	if item.Flags&0x1 != 0 {
		return "", false, ErrArchiveUnsafe
	}
	if item.Method != zip.Store && item.Method != zip.Deflate {
		return "", false, ErrArchiveMethodUnsupported
	}
	if invalidZIPSize(item, limits, expanded) {
		return "", false, ErrArchiveLimitExceeded
	}
	return pathValue, false, nil
}

func invalidZIPSize(item *zip.File, limits ArchiveLimits, expanded int64) bool {
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

// ScanFlatZIP applies the shared ZIP safety and resource limits and then
// tightens the accepted structure to root-level files only. Arcade Parent
// attachments intentionally do not accept directory entries or merged sets.
func ScanFlatZIP(ctx context.Context, path string, limits ArchiveLimits) ([]ArchiveEntry, error) {
	entries, err := ScanZIP(ctx, path, limits)
	if err != nil {
		return nil, err
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid zip", ErrArchiveUnsafe)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	for _, item := range reader.File {
		name, nameErr := zipEntryName(item)
		if nameErr != nil {
			return nil, nameErr
		}
		normalized, directory, pathErr := archivePath(name)
		if pathErr != nil {
			return nil, pathErr
		}
		if directory || strings.Contains(normalized, "/") {
			return nil, ErrNestedArchiveUnsupported
		}
	}
	return entries, nil
}

func zipEntryName(item *zip.File) (string, error) {
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

func readArchiveEntry(
	ctx context.Context,
	item *zip.File,
	ordinal int,
	pathValue string,
	folded string,
	limit int64,
	allowNestedArchives bool,
) (ArchiveEntry, error) {
	reader, err := item.Open()
	if err != nil {
		return ArchiveEntry{}, fmt.Errorf("%w: open entry", ErrArchiveUnsafe)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	sha256Hash := sha256.New()
	legacyHashes := legacychecksum.New()
	crc32Hash := crc32.NewIEEE()
	prefix := make([]byte, 512)
	written := int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return ArchiveEntry{}, fmt.Errorf("importing/archive: %w", err)
		}
		buffer := make([]byte, 1024*1024)
		count, readErr := reader.Read(buffer)
		if count > 0 {
			if written+int64(count) > limit {
				return ArchiveEntry{}, ErrArchiveLimitExceeded
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
			return ArchiveEntry{}, fmt.Errorf("%w: read entry", ErrArchiveUnsafe)
		}
	}
	if item.UncompressedSize64 > ^uint64(0)>>1 ||
		written != int64(
			item.UncompressedSize64,
		) {
		return ArchiveEntry{}, ErrArchiveUnsafe
	}
	if !allowNestedArchives && nestedArchive(pathValue, prefix[:min(int64(len(prefix)), written)]) {
		return ArchiveEntry{}, ErrNestedArchiveUnsupported
	}
	return ArchiveEntry{
		Ordinal: ordinal, OriginalPath: pathValue, NormalizedPath: pathValue, ASCIICasefoldPath: folded,
		ArchiveFormat: "ZIP", CompressionProfile: zipCompressionProfile(item.Method),
		Size: written, CRC32: hex.EncodeToString(crc32Hash.Sum(nil)),
		MD5: hex.EncodeToString(legacyHashes.MD5.Sum(nil)), SHA1: hex.EncodeToString(legacyHashes.SHA1.Sum(nil)),
		SHA256: hex.EncodeToString(sha256Hash.Sum(nil)),
	}, nil
}

func zipCompressionProfile(method uint16) string {
	if method == zip.Store {
		return "STORE"
	}
	return "DEFLATE"
}

func nestedArchive(name string, prefix []byte) bool {
	extension := strings.ToLower(filepath.Ext(name))
	if extension == ".zip" || extension == ".7z" || extension == ".rar" || extension == ".tar" || extension == ".gz" {
		return true
	}
	magics := [][]byte{{'P', 'K', 3, 4}, {'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}, {'R', 'a', 'r', '!'}, {0x1f, 0x8b}}
	for _, magic := range magics {
		if len(prefix) >= len(magic) && string(prefix[:len(magic)]) == string(magic) {
			return true
		}
	}
	return len(prefix) >= 262 && string(prefix[257:262]) == "ustar"
}
