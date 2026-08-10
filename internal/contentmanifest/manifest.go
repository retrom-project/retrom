package contentmanifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrInvalid = errors.New("CONTENT_MANIFEST_INVALID")

type File struct {
	Role                      string
	LogicalName               string
	BlobSHA256                string
	SizeBytes                 int64
	SourceArchiveSHA256       *string
	SourceArchiveEntryOrdinal *int
}

// Build emits the V1 RFC 8785-compatible subset used by content manifests:
// integer/string/null-free objects with lexicographically sorted JSON keys and
// a domain-defined array order. encoding/json sorts map keys by UTF-8 bytes.
func Build(files []File) ([]byte, string, error) {
	ordered := append([]File(nil), files...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Role != ordered[right].Role {
			return ordered[left].Role < ordered[right].Role
		}
		return ordered[left].LogicalName < ordered[right].LogicalName
	})
	items := make([]map[string]any, 0, len(ordered))
	for _, file := range ordered {
		if !validRole(file.Role) || file.LogicalName == "" || len(file.BlobSHA256) != 64 ||
			file.BlobSHA256 != strings.ToLower(file.BlobSHA256) ||
			file.SizeBytes < 0 ||
			(file.SourceArchiveSHA256 == nil) != (file.SourceArchiveEntryOrdinal == nil) {
			return nil, "", ErrInvalid
		}
		item := map[string]any{
			"blobSha256":  file.BlobSHA256,
			"logicalName": file.LogicalName,
			"role":        file.Role,
			"sizeBytes":   file.SizeBytes,
		}
		if file.SourceArchiveSHA256 != nil {
			if len(*file.SourceArchiveSHA256) != 64 ||
				*file.SourceArchiveSHA256 != strings.ToLower(*file.SourceArchiveSHA256) ||
				*file.SourceArchiveEntryOrdinal < 0 {
				return nil, "", ErrInvalid
			}
			item["sourceArchiveEntryOrdinal"] = *file.SourceArchiveEntryOrdinal
			item["sourceArchiveSha256"] = *file.SourceArchiveSHA256
		}
		items = append(items, item)
	}
	contents, err := json.Marshal(items)
	if err != nil {
		return nil, "", fmt.Errorf("contentmanifest/manifest: %w", err)
	}
	digest := sha256.Sum256(contents)
	return contents, hex.EncodeToString(digest[:]), nil
}

func validRole(value string) bool {
	return value == "CONTENT" || value == "DOS_SOURCE" || value == "COMPANION" ||
		value == "PLAYLIST_SOURCE" || value == "DISC"
}
