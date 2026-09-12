package runtimeprovider

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
)

func installedFileDigest(reader io.Reader) (string, error) {
	digest := sha256.New()
	// Docker bind mounts make many 32 KiB reads expensive. Keep memory bounded
	// and hide WriterTo so it cannot replace this buffer with its own small one.
	if _, err := io.CopyBuffer(digest, struct{ io.Reader }{reader}, make([]byte, 1024*1024)); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
