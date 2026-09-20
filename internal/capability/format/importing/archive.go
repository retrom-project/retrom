package importing

import (
	"errors"
	"path/filepath"
	"strings"
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

type NestedArchiveFormat string

const (
	NestedArchiveNone     NestedArchiveFormat = ""
	NestedArchiveZIP      NestedArchiveFormat = "ZIP"
	NestedArchiveSevenZip NestedArchiveFormat = "SEVEN_Z"
	NestedArchiveRAR      NestedArchiveFormat = "RAR"
	NestedArchiveTAR      NestedArchiveFormat = "TAR"
	NestedArchiveGZIP     NestedArchiveFormat = "GZIP"
)

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

// RPGMakerArchiveLimits keeps embedded archive bytes opaque so the RPG Maker
// project normalizer can exclude its one narrowly recognized deployment
// sidecar. Every other embedded archive is rejected by that normalizer.
func RPGMakerArchiveLimits() ArchiveLimits {
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
	NestedArchive      NestedArchiveFormat
}

type ArchiveContent struct {
	Size   int64
	CRC32  string
	MD5    string
	SHA1   string
	SHA256 string
}

func DetectNestedArchive(name string, prefix []byte) NestedArchiveFormat {
	if hasArchiveMagic(prefix, []byte{'P', 'K', 3, 4}) {
		return NestedArchiveZIP
	}
	if hasArchiveMagic(prefix, []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c}) {
		return NestedArchiveSevenZip
	}
	if hasArchiveMagic(prefix, []byte{'R', 'a', 'r', '!'}) {
		return NestedArchiveRAR
	}
	if hasArchiveMagic(prefix, []byte{0x1f, 0x8b}) {
		return NestedArchiveGZIP
	}
	if len(prefix) >= 262 && string(prefix[257:262]) == "ustar" {
		return NestedArchiveTAR
	}
	extension := strings.ToLower(filepath.Ext(name))
	switch extension {
	case ".zip":
		return NestedArchiveZIP
	case ".7z":
		return NestedArchiveSevenZip
	case ".rar":
		return NestedArchiveRAR
	case ".tar":
		return NestedArchiveTAR
	case ".gz":
		return NestedArchiveGZIP
	}
	return NestedArchiveNone
}

func hasArchiveMagic(prefix, magic []byte) bool {
	return len(prefix) >= len(magic) && string(prefix[:len(magic)]) == string(magic)
}
