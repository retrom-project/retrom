package emulationstationimport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"retrom/internal/adapter/files/serversource"
	"retrom/internal/capability/content/multidisc"
	"retrom/internal/capability/format/emulationstationmeta"
	model "retrom/internal/model/emulationstationimport"
)

type contentProjection struct {
	kind, discoveryCode string
	files               []model.ScanItemFile
}

type scanCaches struct {
	discCandidates map[string][]multidisc.File
}

func (service *Scanner) projectGame(
	ctx context.Context,
	gamelistPath, collectionID string,
	game emulationstationmeta.Game,
	files map[string]DiscoveredFile,
	caches *scanCaches,
) (model.ScanItem, error) {
	projection, err := service.projectContent(
		ctx, gamelistPath, game.Path, game.BlockedCode, files, caches,
	)
	if err != nil {
		return model.ScanItem{}, err
	}
	warnings := make([]map[string]any, 0, len(game.Warnings)+2)
	for _, warning := range game.Warnings {
		encoded, _ := json.Marshal(warning)
		var projected map[string]any
		_ = json.Unmarshal(encoded, &projected)
		warnings = append(warnings, projected)
	}
	assets, mediaWarnings, err := service.projectAssets(
		ctx, gamelistPath, game.Assets, files,
	)
	if err != nil {
		return model.ScanItem{}, err
	}
	warnings = append(warnings, mediaWarnings...)
	warnings = BoundedWarnings(warnings)
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
	itemID, err := service.newID()
	if err != nil {
		return model.ScanItem{}, fmt.Errorf("generate EmulationStation item identity: %w", err)
	}
	return model.ScanItem{
		ID: itemID, CollectionID: collectionID, GamelistPath: gamelistPath,
		GameOrdinal: int64(game.Ordinal), SourceKey: hex.EncodeToString(keyDigest[:]),
		Title: game.Metadata.Title, SourceFlagsJSON: sourceFlagsJSON,
		DiscoveryState: discoveryState(projection.discoveryCode),
		DiscoveryCode:  projection.discoveryCode, ContentKind: projection.kind,
		MetadataJSON: metadataJSON, WarningsJSON: warningsJSON,
		SourceManifestJSON:   string(manifestJSON),
		SourceManifestDigest: hex.EncodeToString(manifestDigest[:]),
		Files:                projection.files, Assets: assets,
	}, nil
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

func (service *Scanner) projectContent(
	ctx context.Context,
	gamelistPath, declaredPath, parserCode string,
	files map[string]DiscoveredFile,
	caches *scanCaches,
) (contentProjection, error) {
	projection := contentProjection{kind: "SINGLE_FILE", discoveryCode: parserCode, files: []model.ScanItemFile{}}
	if parserCode != "" || declaredPath == "" {
		return projection, nil
	}
	resolved, valid := resolveGamelistPath(gamelistPath, declaredPath)
	if !valid {
		projection.discoveryCode = emulationstationmeta.CodePathInvalid
		return projection, nil
	}
	entry, exists := files[resolved]
	if !exists {
		projection.discoveryCode = "EMULATIONSTATION_SOURCE_NOT_REGULAR"
		return projection, nil
	}
	primary := model.ScanItemFile{
		Ordinal: 0, Kind: "FILE", Path: resolved, Size: entry.Size, Facts: entry.Facts,
	}
	if !strings.EqualFold(path.Ext(resolved), ".m3u") {
		projection.files = append(projection.files, primary)
		return projection, nil
	}
	primary.Kind = "PLAYLIST"
	projection.kind = multidisc.ContentKind
	playlist, err := service.source.Read(ctx, entry, multidisc.MaxPlaylistBytes)
	if err != nil {
		if stop := scannerStop(ctx, err); stop != nil {
			return contentProjection{}, stop
		}
		projection.discoveryCode = "EMULATIONSTATION_SOURCE_CHANGED"
		return projection, nil
	}
	limits := multidisc.DefaultLimits()
	references, err := multidisc.References(playlist, limits)
	if err != nil {
		projection.discoveryCode = multidiscCode(err)
		return projection, nil
	}
	discs, err := service.scanDiscCandidates(
		ctx, path.Dir(resolved), references, files, caches,
	)
	if err != nil {
		if stop := scannerStop(ctx, err); stop != nil {
			return contentProjection{}, stop
		}
		projection.discoveryCode = "EMULATIONSTATION_SOURCE_CHANGED"
		return projection, nil
	}
	parsed, err := multidisc.Parse(playlist, discs, limits)
	if err != nil {
		projection.discoveryCode = multidiscCode(err)
		return projection, nil
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
		projection.files = append(projection.files, model.ScanItemFile{
			Ordinal: int64(len(projection.files)), Kind: "DISC",
			Path: disc.Path, Size: disc.Size, Facts: disc.Facts,
		})
	}
	return projection, nil
}

func resolveGamelistPath(gamelistPath, declaredPath string) (string, bool) {
	if _, err := emulationstationmeta.NormalizeDeclaredPath(declaredPath); err != nil {
		return "", false
	}
	base := path.Dir(gamelistPath)
	resolved := declaredPath
	if base != "." {
		resolved = path.Join(base, declaredPath)
	}
	if err := serversource.ValidateRelativePath(resolved); err != nil {
		return "", false
	}
	return resolved, true
}

func (service *Scanner) scanDiscCandidates(
	ctx context.Context,
	directory string,
	references []string,
	files map[string]DiscoveredFile,
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
		header, err := service.source.Disc(ctx, entry)
		if err != nil {
			return nil, fmt.Errorf("inspect EmulationStation disc: %w", err)
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
