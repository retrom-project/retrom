package importing

import (
	"archive/zip"
	"context"
	"crypto/md5"  //nolint:gosec // Legacy catalog checksum only.
	"crypto/sha1" //nolint:gosec // Legacy catalog checksum only.
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"retrom/internal/cleanup"
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
}

func DefaultArchiveLimits() ArchiveLimits {
	return ArchiveLimits{
		MaxEntries:          20000,
		MaxEntryBytes:       8 << 30,
		MaxExpandedBytes:    32 << 30,
		MaxCompressionRatio: 200,
	}
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

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func ScanZIP(ctx context.Context, path string, limits ArchiveLimits) ([]ArchiveEntry, error) {
	file, err := os.Open(path) //nolint:gosec // Caller supplies a CAS path, and this scanner never writes beside it.
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
		entryName, err := zipEntryName(item)
		if err != nil {
			return nil, err
		}
		pathValue, directory, err := archivePath(entryName)
		if err != nil {
			return nil, err
		}
		mode := item.Mode()
		if mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !mode.IsDir() {
			return nil, ErrArchiveUnsafe
		}
		if directory {
			if !mode.IsDir() && item.ExternalAttrs != 0 {
				return nil, ErrArchiveUnsafe
			}
			continue
		}
		if item.Flags&0x1 != 0 {
			return nil, ErrArchiveUnsafe
		}
		if item.Method != zip.Store && item.Method != zip.Deflate {
			return nil, ErrArchiveMethodUnsupported
		}
		if limits.MaxEntryBytes < 0 ||
			item.UncompressedSize64 > uint64(
				limits.MaxEntryBytes,
			) {
			return nil, ErrArchiveLimitExceeded
		}
		// The signed ratio limit is proven nonnegative before conversion.
		if item.CompressedSize64 == 0 && item.UncompressedSize64 > 0 ||
			item.CompressedSize64 > 0 && (limits.MaxCompressionRatio < 0 ||
				item.UncompressedSize64/item.CompressedSize64 > uint64(limits.MaxCompressionRatio)) {
			return nil, ErrArchiveLimitExceeded
		}
		if item.UncompressedSize64 > ^uint64(0)>>1 ||
			int64(
				item.UncompressedSize64,
			) > limits.MaxExpandedBytes-total {
			return nil, ErrArchiveLimitExceeded
		}
		total += int64(item.UncompressedSize64)
		folded := ASCIICaseFold(pathValue)
		if _, exists := seenPath[pathValue]; exists {
			return nil, ErrArchiveUnsafe
		}
		if _, exists := seenFold[folded]; exists {
			return nil, ErrArchiveCasefoldCollision
		}
		seenPath[pathValue] = struct{}{}
		seenFold[folded] = struct{}{}
		entry, err := readArchiveEntry(ctx, item, ordinal, pathValue, folded, limits.MaxEntryBytes)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

func zipEntryName(item *zip.File) (string, error) {
	if utf8.ValidString(item.Name) {
		return item.Name, nil
	}
	if !item.NonUTF8 {
		return "", ErrArchiveUnsafe
	}
	decoded, err := simplifiedchinese.GB18030.NewDecoder().String(item.Name)
	if err != nil || !utf8.ValidString(decoded) || strings.ContainsRune(decoded, utf8.RuneError) {
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
) (ArchiveEntry, error) {
	reader, err := item.Open()
	if err != nil {
		return ArchiveEntry{}, fmt.Errorf("%w: open entry", ErrArchiveUnsafe)
	}
	defer func() { cleanup.Error("close", reader.Close()) }()
	sha256Hash := sha256.New()
	md5Hash := md5.New()   //nolint:gosec // Legacy catalog checksum only.
	sha1Hash := sha1.New() //nolint:gosec // Legacy catalog checksum only.
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
			_, _ = io.MultiWriter(sha256Hash, md5Hash, sha1Hash, crc32Hash).Write(buffer[:count])
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
	if nestedArchive(pathValue, prefix[:min(int64(len(prefix)), written)]) {
		return ArchiveEntry{}, ErrNestedArchiveUnsupported
	}
	return ArchiveEntry{
		Ordinal: ordinal, OriginalPath: pathValue, NormalizedPath: pathValue, ASCIICasefoldPath: folded,
		ArchiveFormat: "ZIP", CompressionProfile: zipCompressionProfile(item.Method),
		Size: written, CRC32: hex.EncodeToString(crc32Hash.Sum(nil)),
		MD5: hex.EncodeToString(md5Hash.Sum(nil)), SHA1: hex.EncodeToString(sha1Hash.Sum(nil)),
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
