package fileset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"math"
	"sort"
	"unicode/utf8"
)

const domain = "RETROM_FILESET_V1\x00"

var ErrInvalid = errors.New("RPG_FILESET_INVALID")

type File struct {
	Role                      string
	LogicalName               string
	BlobSHA256                string
	SizeBytes                 int64
	SourceArchiveSHA256       *string
	SourceArchiveEntryOrdinal *int
}

// Digest implements the repository-wide RETROM_FILESET_V1 identity used by
// compact content manifests and RPG Maker project fingerprints.
func Digest(files []File) (string, int64, error) {
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Role != ordered[right].Role {
			return ordered[left].Role < ordered[right].Role
		}
		return ordered[left].LogicalName < ordered[right].LogicalName
	})
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(domain))
	var previousRole, previousPath string
	var totalBytes int64
	for index := range ordered {
		file := ordered[index]
		if err := validate(file); err != nil {
			return "", 0, err
		}
		if index > 0 && file.Role == previousRole && file.LogicalName == previousPath {
			return "", 0, ErrInvalid
		}
		if file.SizeBytes > math.MaxInt64-totalBytes {
			return "", 0, ErrInvalid
		}
		writeEntry(hasher, file)
		totalBytes += file.SizeBytes
		previousRole, previousPath = file.Role, file.LogicalName
	}
	return hex.EncodeToString(hasher.Sum(nil)), totalBytes, nil
}

func writeEntry(hasher hash.Hash, file File) {
	blobDigest, _ := hex.DecodeString(file.BlobSHA256)
	writeLengthPrefixed(hasher, file.Role)
	writeLengthPrefixed(hasher, file.LogicalName)
	_, _ = hasher.Write(blobDigest)
	writeUint64(hasher, uint64(file.SizeBytes))
	if file.SourceArchiveSHA256 == nil {
		_, _ = hasher.Write([]byte{0})
		return
	}
	_, _ = hasher.Write([]byte{1})
	archiveDigest, _ := hex.DecodeString(*file.SourceArchiveSHA256)
	_, _ = hasher.Write(archiveDigest)
	writeUint64(hasher, uint64(*file.SourceArchiveEntryOrdinal))
}

func writeLengthPrefixed(writer hash.Hash, value string) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write([]byte(value))
}

func writeUint64(writer hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = writer.Write(encoded[:])
}

func validate(file File) error {
	if !validRole(file.Role) || !utf8.ValidString(file.Role) || file.LogicalName == "" ||
		!utf8.ValidString(file.LogicalName) || uint64(len(file.Role)) > math.MaxUint32 ||
		uint64(len(file.LogicalName)) > math.MaxUint32 || file.SizeBytes < 0 ||
		(file.SourceArchiveSHA256 == nil) != (file.SourceArchiveEntryOrdinal == nil) ||
		!validDigest(file.BlobSHA256) {
		return ErrInvalid
	}
	if file.SourceArchiveSHA256 != nil &&
		(!validDigest(*file.SourceArchiveSHA256) || *file.SourceArchiveEntryOrdinal < 0) {
		return ErrInvalid
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == string(bytes.ToLower([]byte(value)))
}

func validRole(value string) bool {
	switch value {
	case "CONTENT", "DOS_SOURCE", "COMPANION", "PLAYLIST_SOURCE", "DISC", "PROJECT_FILE",
		"RPG_EASYRPG_INDEX", "RPG_MAKER_LAUNCH_BUNDLE":
		return true
	default:
		return false
	}
}
