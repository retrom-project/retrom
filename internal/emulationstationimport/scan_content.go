package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"retrom/internal/cleanup"
	"retrom/internal/emulationstationmeta"
	"retrom/internal/multidisc"
	"retrom/internal/serversource"
)

type contentProjection struct {
	kind, discoveryCode string
	files               []scannedItemFile
}

type scanCaches struct {
	discCandidates map[string][]multidisc.File
}

func (service *Service) projectGame(
	ctx context.Context,
	root Root,
	selectedPath, gamelistPath, collectionID string,
	game emulationstationmeta.Game,
	files map[string]discoveredFile,
	caches *scanCaches,
) scannedItem {
	projection := service.projectContent(
		ctx, root, selectedPath, gamelistPath, game.Path, game.BlockedCode, files, caches,
	)
	warnings := make([]map[string]any, 0, len(game.Warnings)+2)
	for _, warning := range game.Warnings {
		encoded, _ := json.Marshal(warning)
		var projected map[string]any
		_ = json.Unmarshal(encoded, &projected)
		warnings = append(warnings, projected)
	}
	assets, mediaWarnings := service.projectAssets(
		ctx, root, selectedPath, gamelistPath, game.Assets, files,
	)
	warnings = append(warnings, mediaWarnings...)
	warnings = boundedWarnings(warnings)
	metadataJSON := string(compactJSON(game.Metadata))
	sourceFlagsJSON := string(compactJSON(game.SourceFlags))
	warningsJSON := string(compactJSON(warnings))
	manifest := sourceManifest{
		SchemaVersion: 1,
		ContentKind:   projection.kind,
		Files:         make([]sourceManifestFile, 0, len(projection.files)),
	}
	for _, file := range projection.files {
		manifest.Files = append(manifest.Files, sourceManifestFile{
			Ordinal: file.Ordinal, DeclaredKind: file.Kind, RelativePath: file.Path,
			SizeBytes: file.Size, SourceFactsDigest: file.Facts,
		})
	}
	manifestJSON := compactJSON(manifest)
	manifestDigest := sha256.Sum256(manifestJSON)
	keyDigest := sha256.Sum256([]byte(
		"retrom:emulationstation:item:v1\x00" + gamelistPath + "\x00" + strconv.Itoa(game.Ordinal),
	))
	itemID, _ := uuid.NewV7()
	return scannedItem{
		ID: itemID.String(), CollectionID: collectionID, GamelistPath: gamelistPath,
		GameOrdinal: int64(game.Ordinal), SourceKey: hex.EncodeToString(keyDigest[:]),
		Title: game.Metadata.Title, SourceFlagsJSON: sourceFlagsJSON,
		DiscoveryState: discoveryState(projection.discoveryCode),
		DiscoveryCode:  projection.discoveryCode, ContentKind: projection.kind,
		MetadataJSON: metadataJSON, WarningsJSON: warningsJSON,
		SourceManifestJSON:   string(manifestJSON),
		SourceManifestDigest: hex.EncodeToString(manifestDigest[:]),
		Files:                projection.files, Assets: assets,
	}
}

func boundedWarnings(values []map[string]any) []map[string]any {
	if len(values) <= emulationstationmeta.MaxWarnings {
		return values
	}
	omitted := 0
	for _, warning := range values[emulationstationmeta.MaxWarnings-1:] {
		if warning["code"] != emulationstationmeta.WarningLimitReached {
			omitted++
			continue
		}
		switch count := warning["omittedCount"].(type) {
		case float64:
			omitted += int(count)
		case int:
			omitted += count
		}
	}
	result := append([]map[string]any(nil), values[:emulationstationmeta.MaxWarnings-1]...)
	return append(result, map[string]any{
		"code": emulationstationmeta.WarningLimitReached, "omittedCount": omitted,
	})
}

type sourceManifest struct {
	SchemaVersion int                  `json:"schemaVersion"`
	ContentKind   string               `json:"contentKind"`
	Files         []sourceManifestFile `json:"files"`
}

type sourceManifestFile struct {
	Ordinal           int64  `json:"ordinal"`
	DeclaredKind      string `json:"declaredKind"`
	RelativePath      string `json:"relativePath"`
	SizeBytes         int64  `json:"sizeBytes"`
	SourceFactsDigest string `json:"sourceFactsDigest"`
}

func (service *Service) projectContent(
	ctx context.Context,
	root Root,
	selectedPath, gamelistPath, declaredPath, parserCode string,
	files map[string]discoveredFile,
	caches *scanCaches,
) contentProjection {
	projection := contentProjection{kind: "SINGLE_FILE", discoveryCode: parserCode, files: []scannedItemFile{}}
	if parserCode != "" || declaredPath == "" {
		return projection
	}
	resolved, err := resolveGamelistPath(gamelistPath, declaredPath)
	if err != nil {
		projection.discoveryCode = emulationstationmeta.CodePathInvalid
		return projection
	}
	entry, exists := files[resolved]
	if !exists {
		projection.discoveryCode = "EMULATIONSTATION_SOURCE_NOT_REGULAR"
		return projection
	}
	primary := scannedItemFile{
		Ordinal: 0, Kind: "FILE", Path: resolved, Size: entry.Size, Facts: entry.Facts,
	}
	if !strings.EqualFold(path.Ext(resolved), ".m3u") {
		projection.files = append(projection.files, primary)
		return projection
	}
	primary.Kind = "PLAYLIST"
	projection.kind = multidisc.ContentKind
	playlist, _, err := readFrozenFile(
		ctx, root, selectedPath, entry, multidisc.MaxPlaylistBytes,
	)
	if err != nil {
		projection.discoveryCode = "EMULATIONSTATION_SOURCE_CHANGED"
		return projection
	}
	limits := multidisc.DefaultLimits()
	references, err := multidisc.References(playlist, limits)
	if err != nil {
		projection.discoveryCode = multidiscCode(err)
		return projection
	}
	discs, err := scanDiscCandidates(
		ctx, root, selectedPath, path.Dir(resolved), references, files, caches,
	)
	if err != nil {
		projection.discoveryCode = "EMULATIONSTATION_SOURCE_CHANGED"
		return projection
	}
	parsed, err := multidisc.Parse(playlist, discs, limits)
	if err != nil {
		projection.discoveryCode = multidiscCode(err)
		return projection
	}
	projection.files = append(projection.files, primary)
	for _, parsedEntry := range parsed.Entries {
		if parsedEntry.File == nil {
			continue
		}
		discPath := path.Join(path.Dir(resolved), parsedEntry.File.Basename)
		if path.Dir(resolved) == "." {
			discPath = parsedEntry.File.Basename
		}
		disc := files[discPath]
		projection.files = append(projection.files, scannedItemFile{
			Ordinal: int64(len(projection.files)), Kind: "DISC",
			Path: disc.Path, Size: disc.Size, Facts: disc.Facts,
		})
	}
	return projection
}

func resolveGamelistPath(gamelistPath, declaredPath string) (string, error) {
	if _, err := emulationstationmeta.NormalizeDeclaredPath(declaredPath); err != nil {
		return "", fmt.Errorf("emulationstationimport/normalize declared path: %w", err)
	}
	base := path.Dir(gamelistPath)
	resolved := declaredPath
	if base != "." {
		resolved = path.Join(base, declaredPath)
	}
	if err := serversource.ValidateRelativePath(resolved); err != nil {
		return "", fmt.Errorf("emulationstationimport/validate resolved path: %w", err)
	}
	return resolved, nil
}

func scanDiscCandidates(
	ctx context.Context,
	root Root,
	selectedPath, directory string,
	references []string,
	files map[string]discoveredFile,
	caches *scanCaches,
) ([]multidisc.File, error) {
	cacheKey := directory + "\x00" + strings.Join(references, "\x00")
	if cached, exists := caches.discCandidates[cacheKey]; exists {
		return cached, nil
	}
	exact := make(map[string]string)
	folded := make(map[string][]string)
	for relativePath := range files {
		candidateDirectory := path.Dir(relativePath)
		if candidateDirectory == directory && strings.EqualFold(path.Ext(relativePath), ".chd") {
			basename := path.Base(relativePath)
			exact[basename] = relativePath
			key := multidisc.ASCIIFold(basename)
			folded[key] = append(folded[key], relativePath)
		}
	}
	paths := referencedDiscPaths(references, exact, folded)
	sort.Strings(paths)
	result := make([]multidisc.File, 0, len(paths))
	for _, relativePath := range paths {
		entry := files[relativePath]
		release, acquireErr := serversource.AcquireReader(ctx)
		if acquireErr != nil {
			return nil, fmt.Errorf("emulationstationimport/acquire CHD reader: %w", acquireErr)
		}
		handle, before, err := serversource.OpenRelativeFile(root.path, selectedPath, relativePath)
		if err != nil || before.Size() != entry.Size || serversource.FactsDigest(before) != entry.Facts {
			if handle != nil {
				cleanup.Error("close", handle.Close())
			}
			release()
			return nil, ErrSourceChanged
		}
		header := make([]byte, 8)
		_, readErr := io.ReadFull(handle, header)
		after, statErr := handle.Stat()
		cleanup.Error("close", handle.Close())
		release()
		if readErr != nil || statErr != nil || !serversource.SameFileFacts(before, after) {
			return nil, ErrSourceChanged
		}
		result = append(result, multidisc.File{
			Basename: path.Base(relativePath), SizeBytes: entry.Size, Header: header,
		})
	}
	caches.discCandidates[cacheKey] = result
	return result, nil
}

func referencedDiscPaths(
	references []string,
	exact map[string]string,
	folded map[string][]string,
) []string {
	selected := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if relativePath := exact[reference]; relativePath != "" {
			selected[relativePath] = struct{}{}
			continue
		}
		for _, relativePath := range folded[multidisc.ASCIIFold(reference)] {
			selected[relativePath] = struct{}{}
		}
	}
	result := make([]string, 0, len(selected))
	for relativePath := range selected {
		result = append(result, relativePath)
	}
	return result
}

func multidiscCode(err error) string {
	for _, code := range []multidisc.ErrorCode{
		multidisc.CodePlaylistInvalid, multidisc.CodeReferenceUnsafe,
		multidisc.CodeCHDInvalid, multidisc.CodeLimitExceeded,
	} {
		if multidisc.ErrorHasCode(err, code) {
			return string(code)
		}
	}
	return string(multidisc.CodePlaylistInvalid)
}

func discoveryState(code string) string {
	if code == "" {
		return "READY"
	}
	if strings.HasPrefix(code, "MULTI_DISC_") {
		return "BLOCKED_CONTENT"
	}
	return "BLOCKED_SOURCE"
}

func (result *scanResult) collectItem(item scannedItem) {
	result.Items = append(result.Items, item)
	var warnings []struct {
		PathKind string `json:"pathKind"`
	}
	if json.Unmarshal([]byte(item.WarningsJSON), &warnings) == nil {
		for _, warning := range warnings {
			if warning.PathKind == "COVER" || warning.PathKind == "VIDEO" {
				result.MediaWarnings++
			}
		}
	}
	for _, file := range item.Files {
		result.addEstimated(file.Size)
	}
	for _, asset := range item.Assets {
		if asset.State != "DISCOVERED" || asset.Size == nil {
			continue
		}
		result.addEstimated(*asset.Size)
		if asset.Kind == "COVER" {
			result.Covers++
		} else {
			result.Videos++
		}
	}
}

func (result *scanResult) addEstimated(value int64) {
	const maximum = int64(2 << 40)
	if value < 0 || result.EstimatedBytes > maximum-value {
		result.EstimatedBytes = maximum + 1
		return
	}
	result.EstimatedBytes += value
}
