package sourceimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Basic projects unorganized files into the same collection/item contract as
// metadata formats. It does not parse metadata or read content during discovery.
func scanBasic(ctx context.Context, source ScannerSource, filter string) (ScanResult, error) {
	files := make([]DiscoveredFile, 0)
	var bytes int64
	err := source.Discover(ctx, func(file DiscoveredFile) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("discover basic file: %w", err)
		}
		if !basicExtensionMatches(file.Path, filter) {
			return nil
		}
		if len(files) >= maxGames || file.Size < 0 || file.Size > (2<<40)-bytes {
			return ErrScanLimit
		}
		bytes += file.Size
		files = append(files, file)
		return nil
	})
	if err != nil {
		return ScanResult{}, fmt.Errorf("discover basic source: %w", err)
	}
	if len(files) == 0 {
		return ScanResult{}, ErrFilesAbsent
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })
	collectionID, err := scannerID()
	if err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{EstimatedBytes: bytes, Collections: []ScanCollection{{
		ID: collectionID, MetadataPath: ".", Name: "所选目录", GameCount: int64(len(files)),
		IgnoredJSON: "[]", WarningJSON: "[]",
	}}}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return ScanResult{}, fmt.Errorf("project basic file: %w", err)
		}
		item, err := basicScanItem(collectionID, file)
		if err != nil {
			return ScanResult{}, err
		}
		result.Items = append(result.Items, item)
	}
	evidence, err := json.Marshal(struct {
		Filter string
		Files  []DiscoveredFile
	}{filter, files})
	if err != nil {
		return ScanResult{}, fmt.Errorf("encode basic scan evidence: %w", err)
	}
	digest := sha256.Sum256(evidence)
	result.SnapshotDigest = hex.EncodeToString(digest[:])
	return result, nil
}

func basicExtensionMatches(filePath, filter string) bool {
	if filter == "" {
		return true
	}
	extension := strings.ToLower(path.Ext(filePath))
	for _, allowed := range strings.Split(filter, ";") {
		if extension == allowed {
			return true
		}
	}
	return false
}

func basicScanItem(collectionID string, file DiscoveredFile) (ScanItem, error) {
	id, err := scannerID()
	if err != nil {
		return ScanItem{}, err
	}
	title := strings.TrimSuffix(path.Base(file.Path), path.Ext(file.Path))
	if title == "" {
		title = path.Base(file.Path)
	}
	title = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return ' '
		}
		return char
	}, title)
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	metadata, err := json.Marshal(map[string]any{"title": title})
	if err != nil {
		return ScanItem{}, fmt.Errorf("encode basic metadata: %w", err)
	}
	manifest, err := json.Marshal(map[string]any{"schemaVersion": 1, "declaredFiles": []string{file.Path}})
	if err != nil {
		return ScanItem{}, fmt.Errorf("encode basic source: %w", err)
	}
	digest := sha256.Sum256(manifest)
	key := hex.EncodeToString(digest[:])
	return ScanItem{
		ID: id, CollectionID: collectionID, MetadataPath: file.Path, SourceKey: key, Title: title,
		DiscoveryState: "READY", MetadataJSON: string(metadata), WarningsJSON: "[]",
		SourceFlagsJSON:    `{"hidden":false,"adult":false,"kidGame":false}`,
		SourceManifestJSON: string(manifest), SourceManifestDigest: key,
		Files: []ScanFile{{Kind: "FILE", Path: file.Path, Facts: file.Facts, Size: file.Size}},
	}, nil
}
