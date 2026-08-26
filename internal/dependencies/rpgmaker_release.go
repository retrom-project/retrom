package dependencies

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const rpgMakerObservedReleaseFilename = ".release-assets-observed.json"

var (
	rpgMakerReleaseCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)
	rpgMakerReleaseTag    = regexp.MustCompile(`^retrom-web-[0-9A-Za-z.-]+-r[1-9][0-9]*$`)
)

type RPGMakerRuntimeRelease struct {
	ID                string                  `json:"id"`
	Repository        string                  `json:"repository"`
	Tag               string                  `json:"tag"`
	TagCommit         string                  `json:"tag_commit"`
	AdapterABI        string                  `json:"adapter_abi"`
	BinaryAssociation string                  `json:"binary_association"`
	MetadataAsset     RPGMakerReleaseMetadata `json:"metadata_asset"`
	Assets            []RPGMakerReleaseAsset  `json:"assets"`
}

type RPGMakerReleaseMetadata struct {
	Filename     string `json:"filename"`
	URL          string `json:"url"`
	MaxSizeBytes int64  `json:"max_size_bytes"`
}

type RPGMakerReleaseAsset struct {
	Filename     string `json:"filename"`
	URL          string `json:"url"`
	Path         string `json:"path_in_release"`
	Role         string `json:"role"`
	MaxSizeBytes int64  `json:"max_size_bytes"`
}

type rpgMakerObservedReleases struct {
	SchemaVersion int                                `json:"schema_version"`
	Releases      map[string]rpgMakerObservedRelease `json:"releases"`
}

type rpgMakerObservedRelease struct {
	Repository string                           `json:"repository"`
	Tag        string                           `json:"tag"`
	TagCommit  string                           `json:"tag_commit"`
	AdapterABI string                           `json:"adapter_abi"`
	Assets     map[string]rpgMakerObservedAsset `json:"assets"`
}

type rpgMakerObservedAsset struct {
	SizeBytes int64  `json:"observed_size_bytes"`
	SHA256    string `json:"observed_sha256"`
}

func hydrateRPGMakerReleaseFiles(version *RPGMakerVersion) error {
	releases, err := validateRPGMakerReleases(version.Manifest.RuntimeReleases)
	if err != nil {
		return err
	}
	observed, err := loadRPGMakerObservedReleases(version.RuntimeRoot)
	if err != nil {
		return err
	}
	used := make(map[string]struct{}, len(version.Manifest.RuntimeFiles))
	for index := range version.Manifest.RuntimeFiles {
		key, hydrateErr := hydrateRPGMakerReleaseFile(
			&version.Manifest.RuntimeFiles[index], releases, observed.Releases,
		)
		if hydrateErr != nil {
			return hydrateErr
		}
		if key != "" {
			used[key] = struct{}{}
		}
	}
	return requireAllRPGMakerReleaseAssets(releases, used)
}

func hydrateRPGMakerReleaseFile(
	file *RPGMakerRuntimeFile,
	releases map[string]RPGMakerRuntimeRelease,
	observed map[string]rpgMakerObservedRelease,
) (string, error) {
	if file.ReleaseID == "" {
		if file.AssetFilename != "" {
			return "", fmt.Errorf("%w: RPG Maker release file identity", ErrInvalid)
		}
		return "", nil
	}
	if file.SizeBytes != 0 || file.SHA256 != "" || file.AssetFilename == "" {
		return "", fmt.Errorf("%w: RPG Maker release file digest declaration", ErrInvalid)
	}
	release, exists := releases[file.ReleaseID]
	if !exists {
		return "", fmt.Errorf("%w: RPG Maker release unavailable", ErrInvalid)
	}
	asset, exists := releaseAsset(release, file.AssetFilename, file.Path, file.Role)
	if !exists {
		return "", fmt.Errorf("%w: RPG Maker release asset declaration", ErrInvalid)
	}
	record, exists := observed[file.ReleaseID]
	if !exists || !observedReleaseMatches(release, record) {
		return "", fmt.Errorf("%w: RPG Maker observed release identity", ErrInvalid)
	}
	observedAsset, exists := record.Assets[file.AssetFilename]
	if !exists || observedAsset.SizeBytes < 1 || observedAsset.SizeBytes > asset.MaxSizeBytes ||
		!validSHA256(observedAsset.SHA256) {
		return "", fmt.Errorf("%w: RPG Maker observed release asset", ErrInvalid)
	}
	file.SizeBytes = observedAsset.SizeBytes
	file.SHA256 = observedAsset.SHA256
	return file.ReleaseID + "\x00" + file.AssetFilename, nil
}

func requireAllRPGMakerReleaseAssets(
	releases map[string]RPGMakerRuntimeRelease,
	used map[string]struct{},
) error {
	for id, release := range releases {
		for _, asset := range release.Assets {
			if _, exists := used[id+"\x00"+asset.Filename]; !exists {
				return fmt.Errorf("%w: unused RPG Maker release asset", ErrInvalid)
			}
		}
	}
	return nil
}

func validateRPGMakerReleases(
	declarations []RPGMakerRuntimeRelease,
) (map[string]RPGMakerRuntimeRelease, error) {
	if len(declarations) != 3 {
		return nil, fmt.Errorf("%w: RPG Maker release count", ErrInvalid)
	}
	result := make(map[string]RPGMakerRuntimeRelease, len(declarations))
	paths := make(map[string]struct{}, len(declarations)*2)
	for _, release := range declarations {
		if !validRPGMakerReleaseIdentity(release) {
			return nil, fmt.Errorf("%w: RPG Maker release identity", ErrInvalid)
		}
		if _, duplicate := result[release.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate RPG Maker release", ErrInvalid)
		}
		if err := validateRPGMakerReleaseAssets(release, paths); err != nil {
			return nil, err
		}
		result[release.ID] = release
	}
	return result, nil
}

func validRPGMakerReleaseIdentity(release RPGMakerRuntimeRelease) bool {
	repository, abi, known := expectedRPGMakerReleaseIdentity(release.ID)
	return known && release.Repository == repository && release.AdapterABI == abi &&
		release.BinaryAssociation == "TAGGED_RELEASE_COMPATIBLE" &&
		rpgMakerReleaseTag.MatchString(release.Tag) && rpgMakerReleaseCommit.MatchString(release.TagCommit) &&
		validRPGMakerReleaseMetadata(release) && len(release.Assets) == 2
}

func expectedRPGMakerReleaseIdentity(id string) (string, string, bool) {
	switch id {
	case "easyrpg":
		return "https://github.com/xxxsen/Player", "easyrpg-save-v1", true
	case "easyrpg-r3":
		return "https://github.com/xxxsen/Player", "easyrpg-save-v1", true
	case "mkxp":
		return "https://github.com/xxxsen/mkxp-z-libretro-emscripten", "mkxp-state-v1", true
	default:
		return "", "", false
	}
}

func validateRPGMakerReleaseAssets(
	release RPGMakerRuntimeRelease,
	paths map[string]struct{},
) error {
	filenames := make(map[string]struct{}, len(release.Assets))
	for _, asset := range release.Assets {
		if !validRPGMakerReleaseAsset(release, asset) {
			return fmt.Errorf("%w: RPG Maker release asset", ErrInvalid)
		}
		if _, duplicate := filenames[asset.Filename]; duplicate {
			return fmt.Errorf("%w: duplicate RPG Maker release filename", ErrInvalid)
		}
		if _, duplicate := paths[asset.Path]; duplicate {
			return fmt.Errorf("%w: duplicate RPG Maker release path", ErrInvalid)
		}
		filenames[asset.Filename] = struct{}{}
		paths[asset.Path] = struct{}{}
	}
	return nil
}

func validRPGMakerReleaseMetadata(release RPGMakerRuntimeRelease) bool {
	metadata := release.MetadataAsset
	return metadata.Filename == "retrom-runtime-release.json" && metadata.MaxSizeBytes == 65536 &&
		metadata.URL == rpgMakerReleaseURL(release, metadata.Filename)
}

func validRPGMakerReleaseAsset(release RPGMakerRuntimeRelease, asset RPGMakerReleaseAsset) bool {
	parsed, err := url.Parse(asset.URL)
	return err == nil && asset.Filename != "" && filepath.Base(asset.Filename) == asset.Filename &&
		safeRelative(asset.Path) && (asset.Role == "runtime_js" || asset.Role == "runtime_wasm") &&
		asset.MaxSizeBytes > 0 && asset.MaxSizeBytes <= 128<<20 && parsed.Scheme == "https" &&
		asset.URL == rpgMakerReleaseURL(release, asset.Filename)
}

func rpgMakerReleaseURL(release RPGMakerRuntimeRelease, filename string) string {
	return release.Repository + "/releases/download/" + release.Tag + "/" + filename
}

func releaseAsset(
	release RPGMakerRuntimeRelease,
	filename string,
	path string,
	role string,
) (RPGMakerReleaseAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Filename == filename && asset.Path == path && asset.Role == role {
			return asset, true
		}
	}
	return RPGMakerReleaseAsset{}, false
}

func observedReleaseMatches(
	release RPGMakerRuntimeRelease,
	record rpgMakerObservedRelease,
) bool {
	return record.Repository == release.Repository && record.Tag == release.Tag &&
		record.TagCommit == release.TagCommit && record.AdapterABI == release.AdapterABI &&
		len(record.Assets) == len(release.Assets)
}

func loadRPGMakerObservedReleases(runtimeRoot string) (rpgMakerObservedReleases, error) {
	contents, err := os.ReadFile(filepath.Join(runtimeRoot, rpgMakerObservedReleaseFilename))
	if err != nil {
		return rpgMakerObservedReleases{}, fmt.Errorf("%w: RPG Maker observed releases unavailable", ErrInvalid)
	}
	var observed rpgMakerObservedReleases
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil || requireJSONEOF(decoder) != nil ||
		observed.SchemaVersion != 1 || len(observed.Releases) != 3 {
		return rpgMakerObservedReleases{}, fmt.Errorf("%w: RPG Maker observed releases", ErrInvalid)
	}
	return observed, nil
}

func hydrateRPGMakerArtifacts(
	artifacts []RPGMakerArtifact,
	files map[string]RPGMakerRuntimeFile,
) error {
	for index := range artifacts {
		artifact := &artifacts[index]
		if artifact.EntrySizeBytes != 0 || artifact.EntrySHA256 != "" || artifact.ArtifactSetSHA256 != "" {
			return fmt.Errorf("%w: RPG Maker artifact expected digest", ErrInvalid)
		}
		entries, err := rpgArtifactSetEntries(*artifact, files)
		if err != nil {
			return err
		}
		entry, exists := files[artifact.RuntimeVersion+"/"+artifact.EntryPath]
		if !exists {
			return fmt.Errorf("%w: RPG Maker artifact entry", ErrInvalid)
		}
		artifact.EntrySizeBytes = entry.SizeBytes
		artifact.EntrySHA256 = entry.SHA256
		artifact.ArtifactSetSHA256 = rpgArtifactSetDigest(entries)
	}
	return nil
}

func releaseProvenance(releases []RPGMakerRuntimeRelease) map[string]any {
	result := make(map[string]any, len(releases))
	for _, release := range releases {
		assets := make([]string, 0, len(release.Assets))
		for _, asset := range release.Assets {
			assets = append(assets, asset.Filename)
		}
		result[release.ID] = map[string]any{
			"repository":        release.Repository,
			"tag":               release.Tag,
			"tagCommit":         release.TagCommit,
			"adapterAbi":        release.AdapterABI,
			"assets":            assets,
			"binaryAssociation": release.BinaryAssociation,
		}
	}
	return result
}

func releaseForRuntimeVersion(
	releases []RPGMakerRuntimeRelease,
	runtimeVersion string,
) string {
	for _, release := range releases {
		for _, asset := range release.Assets {
			if strings.HasPrefix(asset.Path, runtimeVersion+"/") {
				return release.ID
			}
		}
	}
	return ""
}
