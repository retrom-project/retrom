package dependencies

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
)

const rpgMakerObservedReleaseFilename = ".release-observed.json"

var (
	rpgMakerReleaseCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)
	rpgMakerReleaseTag    = regexp.MustCompile(
		`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$`,
	)
)

type RPGMakerRuntimeRelease struct {
	Repository    string                  `json:"repository"`
	Tag           string                  `json:"tag"`
	TagCommit     string                  `json:"tag_commit"`
	BundleAsset   RPGMakerReleaseMetadata `json:"bundle_asset"`
	MetadataAsset RPGMakerReleaseMetadata `json:"metadata_asset"`
}

type RPGMakerReleaseMetadata struct {
	Filename     string `json:"filename"`
	URL          string `json:"url"`
	MaxSizeBytes int64  `json:"max_size_bytes"`
}

type rpgMakerObservedRelease struct {
	SchemaVersion  int                              `json:"schema_version"`
	Repository     string                           `json:"repository"`
	Tag            string                           `json:"tag"`
	TagCommit      string                           `json:"tag_commit"`
	BundleFilename string                           `json:"bundle_filename"`
	Files          map[string]rpgMakerObservedAsset `json:"files"`
}

type rpgMakerObservedAsset struct {
	SizeBytes int64  `json:"observed_size_bytes"`
	SHA256    string `json:"observed_sha256"`
}

func hydrateRPGMakerReleaseFiles(version *RPGMakerVersion) error {
	release := version.Manifest.Release
	if !validRPGMakerReleaseIdentity(release) {
		return fmt.Errorf("%w: RPG Maker release identity", ErrInvalid)
	}
	observed, err := loadRPGMakerObservedRelease(version.RuntimeRoot)
	if err != nil {
		return err
	}
	if !observedReleaseMatches(release, observed) || len(observed.Files) != len(version.Manifest.RuntimeFiles) {
		return fmt.Errorf("%w: RPG Maker observed release identity", ErrInvalid)
	}
	seen := make(map[string]struct{}, len(version.Manifest.RuntimeFiles))
	for index := range version.Manifest.RuntimeFiles {
		file := &version.Manifest.RuntimeFiles[index]
		if file.SizeBytes != 0 || file.SHA256 != "" || !safeRelative(file.Path) || !safeRelative(file.BundlePath) {
			return fmt.Errorf("%w: RPG Maker release file declaration", ErrInvalid)
		}
		if _, duplicate := seen[file.Path]; duplicate {
			return fmt.Errorf("%w: duplicate RPG Maker release path", ErrInvalid)
		}
		record, exists := observed.Files[file.Path]
		if !exists || record.SizeBytes < 1 || record.SizeBytes > file.MaxSizeBytes || !validSHA256(record.SHA256) {
			return fmt.Errorf("%w: RPG Maker observed release asset", ErrInvalid)
		}
		file.SizeBytes = record.SizeBytes
		file.SHA256 = record.SHA256
		seen[file.Path] = struct{}{}
	}
	return nil
}

func validRPGMakerReleaseIdentity(release RPGMakerRuntimeRelease) bool {
	return release.Repository == "https://github.com/xxxsen/retrom-runtime" &&
		rpgMakerReleaseTag.MatchString(release.Tag) && rpgMakerReleaseCommit.MatchString(release.TagCommit) &&
		validRPGMakerReleaseAsset(release, release.BundleAsset, "retrom-runtime-"+release.Tag[1:]+".tar.gz", 256<<20) &&
		validRPGMakerReleaseAsset(release, release.MetadataAsset, "retrom-runtime-release.json", 1<<20)
}

func validRPGMakerReleaseAsset(
	release RPGMakerRuntimeRelease,
	asset RPGMakerReleaseMetadata,
	filename string,
	maximum int64,
) bool {
	parsed, err := url.Parse(asset.URL)
	return err == nil && parsed.Scheme == "https" && asset.Filename == filename &&
		asset.MaxSizeBytes == maximum && asset.URL == rpgMakerReleaseURL(release, filename)
}

func rpgMakerReleaseURL(release RPGMakerRuntimeRelease, filename string) string {
	return release.Repository + "/releases/download/" + release.Tag + "/" + filename
}

func observedReleaseMatches(release RPGMakerRuntimeRelease, record rpgMakerObservedRelease) bool {
	return record.SchemaVersion == 1 && record.Repository == release.Repository && record.Tag == release.Tag &&
		record.TagCommit == release.TagCommit && record.BundleFilename == release.BundleAsset.Filename
}

func loadRPGMakerObservedRelease(runtimeRoot string) (rpgMakerObservedRelease, error) {
	contents, err := os.ReadFile(filepath.Join(runtimeRoot, rpgMakerObservedReleaseFilename))
	if err != nil {
		return rpgMakerObservedRelease{}, fmt.Errorf("%w: RPG Maker observed release unavailable", ErrInvalid)
	}
	var observed rpgMakerObservedRelease
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observed); err != nil || requireJSONEOF(decoder) != nil {
		return rpgMakerObservedRelease{}, fmt.Errorf("%w: RPG Maker observed release", ErrInvalid)
	}
	return observed, nil
}

func hydrateRPGMakerArtifacts(artifacts []RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
	for index := range artifacts {
		artifact := &artifacts[index]
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

func releaseProvenance(release RPGMakerRuntimeRelease) map[string]any {
	return map[string]any{
		"repository":  release.Repository,
		"tag":         release.Tag,
		"tagCommit":   release.TagCommit,
		"bundleAsset": release.BundleAsset.Filename,
	}
}
