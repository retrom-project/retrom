package testsupport

import (
	"crypto/sha256"
	"encoding/hex"
	"hash/crc32"

	"retrom/internal/filestore"
	"retrom/internal/legacychecksum"

	"github.com/google/uuid"
)

// FileMetadata supplies a domain file value for persistence/policy fixtures.
// Tests which exercise IO must create bytes with filestore.Store instead.
func FileMetadata(seed string) filestore.Metadata {
	data := []byte(seed)
	digest := sha256.Sum256(data)
	legacy := legacychecksum.New()
	_, _ = legacy.MD5.Write(data)
	_, _ = legacy.SHA1.Write(data)
	id := uuid.NewSHA1(uuid.NameSpaceOID, data)
	record := filestore.Record{
		Path:   "staging/writes/" + id.String(),
		SHA256: hex.EncodeToString(digest[:]), MD5: hex.EncodeToString(legacy.MD5.Sum(nil)),
		SHA1: hex.EncodeToString(legacy.SHA1.Sum(nil)), CRC32: crcHex(crc32.ChecksumIEEE(data)),
		Size: int64(len(data)), MediaType: "application/octet-stream",
	}
	value, err := record.Encode()
	if err != nil {
		panic(err)
	}
	return filestore.Metadata{
		Record: value, SHA256: record.SHA256, MD5: record.MD5,
		SHA1: record.SHA1, CRC32: record.CRC32, Size: record.Size,
	}
}

func crcHex(value uint32) string {
	return hex.EncodeToString([]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}
