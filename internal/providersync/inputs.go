// Package providersync installs the pinned runtime Providers from content-addressed chunks.
package providersync

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

var (
	// ErrInputsInvalid reports a malformed runtime-inputs.json or chunk manifest.
	ErrInputsInvalid = errors.New("PROVIDER_INPUTS_INVALID")
	// ErrDowngrade reports an image pinning a Provider older than the installed one.
	ErrDowngrade = errors.New("RUNTIME_PROVIDER_DOWNGRADE_FORBIDDEN")
	// ErrRebuilt reports a different bundle published under an installed version.
	ErrRebuilt = errors.New("RUNTIME_PROVIDER_VERSION_REBUILT")
	// ErrChunkInvalid reports a downloaded chunk that does not match its manifest.
	ErrChunkInvalid = errors.New("PROVIDER_CHUNK_INVALID")
	// ErrBundleInvalid reports an assembled bundle that does not match its record.
	ErrBundleInvalid = errors.New("PROVIDER_BUNDLE_INVALID")
	// ErrSpace reports insufficient free space before any download starts.
	ErrSpace = errors.New("PROVIDER_DISK_SPACE_INSUFFICIENT")
	// ErrSourceRequired reports a candidate pin without an explicit chunk source.
	ErrSourceRequired = errors.New("PROVIDER_SOURCE_REQUIRED")
)

var (
	lowerDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)
	providerID  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)
	semver      = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z.-]+))?$`)
	assetName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)
)

// Release identifies the immutable runtime release the inputs were published by.
type Release struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
}

// Asset names one flat release asset and its exact transport identity.
type Asset struct {
	Asset     string `json:"asset"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"sizeBytes"`
}

// ProviderRecord is one pinned Provider bundle in runtime-inputs.json.
type ProviderRecord struct {
	BundleSHA256      string `json:"bundleSha256"`
	ChunkManifest     Asset  `json:"chunkManifest"`
	FileCount         int64  `json:"fileCount"`
	ManifestSHA256    string `json:"manifestSha256"`
	ModuleSHA256      string `json:"moduleSha256"`
	ProviderID        string `json:"providerId"`
	ProviderVersion   string `json:"providerVersion"`
	UnpackedSizeBytes int64  `json:"unpackedSizeBytes"`
}

// Inputs is the pinned runtime-inputs.json descriptor.
type Inputs struct {
	SchemaVersion    int              `json:"schemaVersion"`
	Repository       string           `json:"repository"`
	SourceTreeSHA256 string           `json:"sourceTreeSha256"`
	Release          *Release         `json:"release"`
	Tool             json.RawMessage  `json:"tool"`
	Providers        []ProviderRecord `json:"providers"`
	// Digest is the SHA-256 of the exact descriptor bytes; it keys the completion marker.
	Digest string `json:"-"`
}

// ChunkFile is one bundle file carried by a chunk.
type ChunkFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"sizeBytes"`
}

// Chunk is one content-addressed archive of bundle files.
type Chunk struct {
	Archive           string      `json:"archive"`
	ArchiveSHA256     string      `json:"archiveSha256"`
	ArchiveSizeBytes  int64       `json:"archiveSizeBytes"`
	ChunkID           string      `json:"chunkId"`
	Files             []ChunkFile `json:"files"`
	UnpackedSizeBytes int64       `json:"unpackedSizeBytes"`
}

// ChunkManifest lists every chunk of one Provider bundle.
type ChunkManifest struct {
	SchemaVersion   int     `json:"schemaVersion"`
	ProviderID      string  `json:"providerId"`
	ProviderVersion string  `json:"providerVersion"`
	BundleSHA256    string  `json:"bundleSha256"`
	Chunks          []Chunk `json:"chunks"`
}

// LoadInputs reads and validates a runtime-inputs.json descriptor.
func LoadInputs(path string) (*Inputs, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read runtime inputs: %w", err)
	}
	var inputs Inputs
	if err = strictDecode(raw, &inputs); err != nil {
		return nil, err
	}
	if err = inputs.validate(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	inputs.Digest = hex.EncodeToString(sum[:])
	return &inputs, nil
}

func (inputs *Inputs) validate() error {
	if inputs.SchemaVersion != 1 || !lowerDigest.MatchString(inputs.SourceTreeSHA256) || len(inputs.Providers) == 0 {
		return ErrInputsInvalid
	}
	if inputs.Release != nil && (!strings.HasPrefix(inputs.Release.Repository, "https://") ||
		inputs.Release.Tag != "v"+inputs.Providers[0].ProviderVersion) {
		return ErrInputsInvalid
	}
	seen := make(map[string]bool, len(inputs.Providers))
	for _, provider := range inputs.Providers {
		if seen[provider.ProviderID] || !provider.valid() {
			return ErrInputsInvalid
		}
		seen[provider.ProviderID] = true
	}
	return nil
}

func (record ProviderRecord) valid() bool {
	return providerID.MatchString(record.ProviderID) && semver.MatchString(record.ProviderVersion) &&
		lowerDigest.MatchString(record.BundleSHA256) && lowerDigest.MatchString(record.ManifestSHA256) &&
		lowerDigest.MatchString(record.ModuleSHA256) && record.FileCount > 0 && record.UnpackedSizeBytes > 0 &&
		record.ChunkManifest.valid() &&
		record.ChunkManifest.Asset == record.ProviderID+"-chunks-"+record.ProviderVersion+".json"
}

func (asset Asset) valid() bool {
	return assetName.MatchString(asset.Asset) && lowerDigest.MatchString(asset.SHA256) && asset.SizeBytes > 0
}

// parseManifest validates a chunk manifest against its pinned Provider record.
func parseManifest(raw []byte, record ProviderRecord) (*ChunkManifest, error) {
	var manifest ChunkManifest
	if err := strictDecode(raw, &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != 1 || manifest.ProviderID != record.ProviderID ||
		manifest.ProviderVersion != record.ProviderVersion || manifest.BundleSHA256 != record.BundleSHA256 ||
		len(manifest.Chunks) == 0 {
		return nil, ErrInputsInvalid
	}
	paths := make(map[string]bool)
	var files, unpacked int64
	for _, chunk := range manifest.Chunks {
		if err := validateChunk(chunk, record.ProviderID, paths); err != nil {
			return nil, err
		}
		files += int64(len(chunk.Files))
		unpacked += chunk.UnpackedSizeBytes
	}
	if files != record.FileCount || unpacked != record.UnpackedSizeBytes {
		return nil, ErrInputsInvalid
	}
	return &manifest, nil
}

func validateChunk(chunk Chunk, provider string, paths map[string]bool) error {
	if !lowerDigest.MatchString(chunk.ChunkID) || !lowerDigest.MatchString(chunk.ArchiveSHA256) ||
		chunk.Archive != provider+"-chunk-"+chunk.ChunkID+".tar.gz" || chunk.ArchiveSizeBytes <= 0 ||
		len(chunk.Files) == 0 {
		return ErrInputsInvalid
	}
	var total int64
	for index, file := range chunk.Files {
		if !safeRelative(file.Path) || paths[file.Path] || !lowerDigest.MatchString(file.SHA256) || file.SizeBytes < 0 ||
			(index > 0 && chunk.Files[index-1].Path >= file.Path) {
			return ErrInputsInvalid
		}
		paths[file.Path] = true
		total += file.SizeBytes
	}
	if total != chunk.UnpackedSizeBytes || chunkID(chunk.Files) != chunk.ChunkID {
		return ErrInputsInvalid
	}
	return nil
}

// chunkID reproduces the runtime identity: SHA-256 of the compact JSON list of
// [path, sizeBytes, sha256] in byte order, exactly as JSON.stringify emits it.
func chunkID(files []ChunkFile) string {
	identity := make([][]any, 0, len(files))
	for _, file := range files {
		identity = append(identity, []any{file.Path, file.SizeBytes, file.SHA256})
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(identity); err != nil {
		return ""
	}
	sum := sha256.Sum256(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
	return hex.EncodeToString(sum[:])
}

func strictDecode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || decoder.More() {
		return ErrInputsInvalid
	}
	return nil
}

func safeRelative(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\\\x00") {
		return false
	}
	return !slices.ContainsFunc(strings.Split(path, "/"), func(part string) bool {
		return part == "" || part == "." || part == ".."
	})
}
