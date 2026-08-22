// Package legacychecksum isolates the non-security hashes required by ROM and
// firmware catalog protocols. Security identity must continue to use SHA-256.
package legacychecksum

import (
	"crypto/md5"  //nolint:gosec // Catalog protocols require MD5; callers also retain authoritative SHA-256 identity.
	"crypto/sha1" //nolint:gosec // Catalog protocols require SHA-1; callers also retain authoritative SHA-256 identity.
	"encoding/hex"
	"hash"
)

// Hashes contains only legacy catalog digest writers.
type Hashes struct {
	MD5  hash.Hash
	SHA1 hash.Hash
}

// New creates isolated legacy catalog digest writers.
func New() Hashes {
	return Hashes{
		MD5:  md5.New(),  //nolint:gosec // Compatibility output is never accepted as a security identity.
		SHA1: sha1.New(), //nolint:gosec // Compatibility output is never accepted as a security identity.
	}
}

// Sum returns lowercase legacy catalog digests for a bounded in-memory value.
func Sum(value []byte) (string, string) {
	hashes := New()
	_, _ = hashes.MD5.Write(value)
	_, _ = hashes.SHA1.Write(value)
	return hex.EncodeToString(hashes.MD5.Sum(nil)), hex.EncodeToString(hashes.SHA1.Sum(nil))
}
