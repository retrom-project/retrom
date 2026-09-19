package libraryimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"retrom/internal/capability/content/multidisc"
)

// ComputeContentIdentity computes the deterministic content identity digest
// from ordered identity parts. It uses the RETROM_CONTENT_IDENTITY_V1 prefix
// format with NUL-separated role, SHA256, and count fields.
func ComputeContentIdentity(parts []ContentIdentityPart) (string, error) {
	if len(parts) == 0 {
		return "", ErrInvalid
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte("RETROM_CONTENT_IDENTITY_V1\x00"))
	for _, part := range parts {
		if part.Role == "" || len(part.SHA256) != 64 || part.Count < 1 {
			return "", ErrInvalid
		}
		_, _ = fmt.Fprintf(digest, "%s\x00%s\x00%d\x00", part.Role, part.SHA256, part.Count)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// ComputeDiscIdentity computes the deterministic multi-disc content identity
// from ordered disc records. Returns ErrMultiDiscIncomplete if any disc is
// not in PRESENT state or has an empty hash.
func ComputeDiscIdentity(discs []ContentIdentityDisc) (string, error) {
	hashes := make([]string, 0, len(discs))
	for _, disc := range discs {
		if disc.State != "PRESENT" || disc.SHA256 == "" {
			return "", ErrMultiDiscIncomplete
		}
		hashes = append(hashes, disc.SHA256)
	}
	return multidisc.ContentIdentity(hashes)
}

// ComputeSnapshotIdentity dispatches to the correct identity algorithm based
// on the content kind. It accepts pre-loaded fact values and performs no I/O.
func ComputeSnapshotIdentity(kind string, parts []ContentIdentityPart, discs []ContentIdentityDisc) (string, error) {
	if kind == multidisc.ContentKind {
		return ComputeDiscIdentity(discs)
	}
	return ComputeContentIdentity(parts)
}
