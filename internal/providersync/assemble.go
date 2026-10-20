package providersync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type integrityFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"mediaType"`
}

type providerManifest struct {
	ProviderAPIVersion int    `json:"providerApiVersion"`
	ClientModulePath   string `json:"clientModulePath"`
	Targets            []struct {
		ID         string          `json:"id"`
		Checkpoint json.RawMessage `json:"checkpoint"`
	} `json:"targets"`
}

// activeTarget and activeProvider reproduce the existing active.json entry shape.
type activeTarget struct {
	Checkpoint json.RawMessage `json:"checkpoint"`
	ID         string          `json:"id"`
}

type activeProvider struct {
	BundleSHA256       string         `json:"bundleSha256"`
	ClientModulePath   string         `json:"clientModulePath"`
	FileCount          int64          `json:"fileCount"`
	InstallationPath   string         `json:"installationPath"`
	ManifestSHA256     string         `json:"manifestSha256"`
	ModuleSHA256       string         `json:"moduleSha256"`
	ProviderAPIVersion int            `json:"providerApiVersion"`
	ProviderID         string         `json:"providerId"`
	ProviderVersion    string         `json:"providerVersion"`
	Targets            []activeTarget `json:"targets"`
	UnpackedSizeBytes  int64          `json:"unpackedSizeBytes"`
}

func installationPath(record ProviderRecord) string {
	return record.ProviderID + "/" + record.BundleSHA256
}

// assemble publishes installed/<provider>/<bundle>/ from cached chunks by hard links.
func (s *syncer) assemble(record ProviderRecord, manifest *ChunkManifest) (activeProvider, error) {
	final := filepath.Join(s.root, "installed", filepath.FromSlash(installationPath(record)))
	if exists(final) {
		entry, err := describeBundle(final, record, manifest)
		if err == nil {
			return entry, nil
		}
		if err = os.RemoveAll(final); err != nil {
			return activeProvider{}, fmt.Errorf("remove invalid bundle: %w", err)
		}
	}
	staging := filepath.Join(s.root, ".staging", "bundle-"+record.ProviderID+"-"+record.BundleSHA256)
	if err := os.RemoveAll(staging); err != nil {
		return activeProvider{}, fmt.Errorf("reset bundle staging: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	for _, chunk := range manifest.Chunks {
		for _, file := range chunk.Files {
			target := filepath.Join(staging, filepath.FromSlash(file.Path))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return activeProvider{}, fmt.Errorf("create %s: %w", filepath.Dir(target), err)
			}
			if err := linkOrCopy(filepath.Join(s.cache, chunk.ChunkID, filepath.FromSlash(file.Path)), target); err != nil {
				return activeProvider{}, err
			}
		}
	}
	entry, err := describeBundle(staging, record, manifest)
	if err != nil {
		return activeProvider{}, err
	}
	if err = os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return activeProvider{}, fmt.Errorf("create installed root: %w", err)
	}
	if err = os.Rename(staging, final); err != nil {
		return activeProvider{}, fmt.Errorf("publish bundle: %w", err)
	}
	return entry, syncDirectory(filepath.Dir(final))
}

// describeBundle checks an assembled bundle against its pin without rehashing chunk
// files: integrity.json must hash to bundleSha256 and list exactly the chunk files.
func describeBundle(root string, record ProviderRecord, manifest *ChunkManifest) (activeProvider, error) {
	raw, err := os.ReadFile(filepath.Join(root, "integrity.json"))
	if err != nil {
		return activeProvider{}, fmt.Errorf("read integrity: %w", err)
	}
	if digest(raw) != record.BundleSHA256 {
		return activeProvider{}, ErrBundleInvalid
	}
	var integrity struct {
		SchemaVersion int             `json:"schemaVersion"`
		Files         []integrityFile `json:"files"`
	}
	if err = strictDecode(raw, &integrity); err != nil || integrity.SchemaVersion != 1 {
		return activeProvider{}, ErrBundleInvalid
	}
	chunkFiles := make(map[string]ChunkFile)
	for _, chunk := range manifest.Chunks {
		for _, file := range chunk.Files {
			chunkFiles[file.Path] = file
		}
	}
	if err = matchIntegrity(root, integrity.Files, chunkFiles, record); err != nil {
		return activeProvider{}, err
	}
	return activeEntry(root, record, integrity.Files)
}

func matchIntegrity(root string, files []integrityFile, chunkFiles map[string]ChunkFile, record ProviderRecord) error {
	if int64(len(files))+1 != record.FileCount || len(chunkFiles) != len(files)+1 {
		return ErrBundleInvalid
	}
	for _, file := range files {
		chunk, ok := chunkFiles[file.Path]
		if !ok || chunk.SHA256 != file.SHA256 || chunk.SizeBytes != file.SizeBytes {
			return ErrBundleInvalid
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(file.Path)))
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.SizeBytes {
			return ErrBundleInvalid
		}
	}
	return nil
}

func activeEntry(root string, record ProviderRecord, files []integrityFile) (activeProvider, error) {
	raw, err := os.ReadFile(filepath.Join(root, "provider.json"))
	if err != nil {
		return activeProvider{}, fmt.Errorf("read Provider manifest: %w", err)
	}
	var manifest providerManifest
	if digest(raw) != record.ManifestSHA256 || json.Unmarshal(raw, &manifest) != nil {
		return activeProvider{}, ErrBundleInvalid
	}
	module := ""
	for _, file := range files {
		if file.Path == manifest.ClientModulePath {
			module = file.SHA256
		}
	}
	if module != record.ModuleSHA256 || len(manifest.Targets) == 0 {
		return activeProvider{}, ErrBundleInvalid
	}
	targets := make([]activeTarget, 0, len(manifest.Targets))
	for _, target := range manifest.Targets {
		targets = append(targets, activeTarget{Checkpoint: target.Checkpoint, ID: target.ID})
	}
	return activeProvider{
		BundleSHA256: record.BundleSHA256, ClientModulePath: manifest.ClientModulePath, FileCount: record.FileCount,
		InstallationPath: installationPath(record), ManifestSHA256: record.ManifestSHA256, ModuleSHA256: module,
		ProviderAPIVersion: manifest.ProviderAPIVersion, ProviderID: record.ProviderID,
		ProviderVersion: record.ProviderVersion, Targets: targets, UnpackedSizeBytes: record.UnpackedSizeBytes,
	}, nil
}

func digest(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}
