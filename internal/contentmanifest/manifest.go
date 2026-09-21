package contentmanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"retrom/internal/contentprofile"
	"retrom/internal/rpgmaker/fileset"
)

var ErrInvalid = errors.New("CONTENT_MANIFEST_INVALID")

type File = fileset.File

// Build emits the compact canonical V2 manifest and its digest. Exact files stay
// in normalized file tables; the manifest commits to them through FilesDigest.
func Build(contentKind string, files []File) ([]byte, string, error) {
	if !contentprofile.KnownContentKind(contentprofile.ContentKind(contentKind)) {
		return nil, "", ErrInvalid
	}
	filesDigest, totalBytes, err := FilesDigest(files)
	if err != nil {
		return nil, "", err
	}
	manifest := map[string]any{
		"contentKind":   contentKind,
		"fileCount":     len(files),
		"filesDigest":   filesDigest,
		"schemaVersion": 2,
		"totalBytes":    totalBytes,
	}
	contents, err := json.Marshal(manifest)
	if err != nil {
		return nil, "", fmt.Errorf("contentmanifest/manifest: %w", err)
	}
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:]), nil
}

// FilesDigest implements RETROM_FILESET_V1. Entries are ordered by the raw
// UTF-8 bytes of (role,path), and all integers use unsigned big-endian form.
func FilesDigest(files []File) (string, int64, error) {
	digest, totalBytes, err := fileset.Digest(files)
	if err != nil {
		return "", 0, ErrInvalid
	}
	return digest, totalBytes, nil
}
