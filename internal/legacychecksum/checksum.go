// Package legacychecksum isolates the non-security hashes required by ROM and
// firmware catalog protocols. Security identity must continue to use SHA-256.
package legacychecksum

import (
	"crypto/md5"
	"crypto/sha1"
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
		MD5:  md5.New(),
		SHA1: sha1.New(),
	}
}

// Sum returns lowercase legacy catalog digests for a bounded in-memory value.
func Sum(value []byte) (string, string) {
	hashes := New()
	_, _ = hashes.MD5.Write(value)
	_, _ = hashes.SHA1.Write(value)
	return hex.EncodeToString(hashes.MD5.Sum(nil)), hex.EncodeToString(hashes.SHA1.Sum(nil))
}
