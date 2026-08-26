package dependencies

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"retrom/internal/rpgmaker/routing"
)

const rpgMakerManifestVersion = "v1"

type RPGMakerManifest struct {
	SchemaVersion   int                      `json:"schema_version"`
	RuntimeID       string                   `json:"runtime_id"`
	RuntimeFiles    []RPGMakerRuntimeFile    `json:"runtime_files"`
	RuntimeReleases []RPGMakerRuntimeRelease `json:"runtime_releases"`
	Artifacts       []RPGMakerArtifact       `json:"artifacts"`
	SourceArchives  []RPGMakerSourceArchive  `json:"source_archives"`
	Build           RPGMakerBuild            `json:"build"`
}

type RPGMakerRuntimeFile struct {
	Path          string `json:"path_in_release"`
	SizeBytes     int64  `json:"size_bytes,omitempty"`
	SHA256        string `json:"sha256,omitempty"`
	ReleaseID     string `json:"release_id,omitempty"`
	AssetFilename string `json:"asset_filename,omitempty"`
	Role          string `json:"role"`
}

type RPGMakerArtifact struct {
	CoreID                 string          `json:"core_id"`
	Generation             string          `json:"generation"`
	RouteKey               string          `json:"route_key"`
	RuntimeAdapterKind     string          `json:"runtime_adapter_kind"`
	RuntimeVersion         string          `json:"runtime_version"`
	AdapterID              string          `json:"adapter_id"`
	AdapterABI             string          `json:"adapter_abi"`
	EntryPath              string          `json:"entry_path"`
	EntrySizeBytes         int64           `json:"entry_size_bytes,omitempty"`
	EntrySHA256            string          `json:"entry_sha256,omitempty"`
	ArtifactSetSHA256      string          `json:"artifact_set_sha256,omitempty"`
	FilePaths              []string        `json:"file_paths"`
	RequiresThreads        bool            `json:"requires_threads"`
	SavePayloadKind        string          `json:"save_payload_kind"`
	SaveMaxBytes           int64           `json:"save_max_bytes"`
	SelectedForNewBindings bool            `json:"selected_for_new_bindings"`
	AvailableForLaunch     bool            `json:"available_for_launch"`
	Compatibility          json.RawMessage `json:"compatibility"`
}

type RPGMakerSourceArchive struct {
	ComponentID string `json:"component_id"`
	Repository  string `json:"repository"`
	Commit      string `json:"commit"`
	ArchiveURL  string `json:"archive_url"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	LicensePath string `json:"license_path"`
}

type RPGMakerBuild struct {
	RecipePath               string `json:"recipe_path"`
	EasyRPGEmscriptenVersion string `json:"easyrpg_emscripten_version"`
	MKXPEmscriptenVersion    string `json:"mkxp_emscripten_version"`
	WASISDKVersion           string `json:"wasi_sdk_version"`
	BinaryenVersion          string `json:"binaryen_version"`
	EasyRPGPatchPath         string `json:"easyrpg_patch_path"`
	EasyRPGPatchSHA256       string `json:"easyrpg_patch_sha256"`
	MKXPBridgePath           string `json:"mkxp_bridge_path"`
	MKXPBridgeSHA256         string `json:"mkxp_bridge_sha256"`
	NativeBridgeV3Path       string `json:"native_bridge_v3_path"`
	NativeBridgeV3SHA256     string `json:"native_bridge_v3_sha256"`
}

type RPGMakerVersion struct {
	Manifest       RPGMakerManifest
	ManifestSHA256 string
	DATRoot        string
	RuntimeRoot    string
	Allowlist      map[string]RPGMakerRuntimeFile
}

func loadRPGMaker(root string) (*RPGMakerVersion, error) {
	datRoot := filepath.Join(root, "dat", "rpgmaker", rpgMakerManifestVersion)
	runtimeRoot := filepath.Join(root, "runtime", "rpgmaker", rpgMakerManifestVersion)
	contents, err := os.ReadFile(filepath.Join(datRoot, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: RPG Maker manifest unavailable", ErrInvalid)
	}
	var manifest RPGMakerManifest
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: RPG Maker manifest schema", ErrInvalid)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: RPG Maker manifest trailing data", ErrInvalid)
	}
	digest := sha256.Sum256(contents)
	version := &RPGMakerVersion{
		Manifest: manifest, ManifestSHA256: hex.EncodeToString(digest[:]),
		DATRoot: datRoot, RuntimeRoot: runtimeRoot,
		Allowlist: make(map[string]RPGMakerRuntimeFile, len(manifest.RuntimeFiles)),
	}
	if err := hydrateRPGMakerReleaseFiles(version); err != nil {
		return nil, err
	}
	if err := validateRPGMakerVersion(version); err != nil {
		return nil, err
	}
	return version, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func validateRPGMakerVersion(version *RPGMakerVersion) error {
	manifest := version.Manifest
	if !validRPGMakerManifestIdentity(manifest) {
		return fmt.Errorf("%w: RPG Maker manifest identity", ErrInvalid)
	}
	if err := validateRPGMakerRuntimeFiles(version); err != nil {
		return err
	}
	if err := hydrateRPGMakerArtifacts(manifest.Artifacts, version.Allowlist); err != nil {
		return err
	}
	version.Manifest.Artifacts = manifest.Artifacts
	if err := validateRPGMakerArtifacts(manifest.Artifacts, version.Allowlist); err != nil {
		return err
	}
	if err := validateRPGMakerSources(manifest.SourceArchives); err != nil {
		return err
	}
	return validateRPGMakerBuild(version)
}

func validRPGMakerManifestIdentity(manifest RPGMakerManifest) bool {
	return manifest.SchemaVersion == 1 && manifest.RuntimeID == "rpgmaker-v1" &&
		len(manifest.RuntimeFiles) > 0 && len(manifest.Artifacts) == len(routing.Entries()) &&
		len(manifest.RuntimeReleases) == 3 && len(manifest.SourceArchives) > 0
}

func validateRPGMakerRuntimeFiles(version *RPGMakerVersion) error {
	for _, file := range version.Manifest.RuntimeFiles {
		if !validRPGMakerRuntimeFile(file) {
			return fmt.Errorf("%w: RPG Maker runtime file", ErrInvalid)
		}
		if _, duplicate := version.Allowlist[file.Path]; duplicate {
			return fmt.Errorf("%w: duplicate RPG Maker runtime file", ErrInvalid)
		}
		if err := checkFile(version.RuntimeRoot, file.Path, file.SizeBytes, file.SHA256); err != nil {
			return err
		}
		version.Allowlist[file.Path] = file
	}
	return nil
}

func validRPGMakerRuntimeFile(file RPGMakerRuntimeFile) bool {
	validRole := file.Role == "runtime_js" || file.Role == "runtime_wasm" || file.Role == "adapter_bridge"
	validRelease := file.ReleaseID == "" && file.AssetFilename == "" ||
		file.ReleaseID != "" && file.AssetFilename != ""
	return safeRelative(file.Path) && file.SizeBytes > 0 && validSHA256(file.SHA256) &&
		validRole && validRelease
}

func validateRPGMakerArtifacts(
	artifacts []RPGMakerArtifact,
	files map[string]RPGMakerRuntimeFile,
) error {
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if _, duplicate := seen[artifact.RouteKey]; duplicate {
			return fmt.Errorf("%w: duplicate RPG Maker route", ErrInvalid)
		}
		seen[artifact.RouteKey] = struct{}{}
		if err := validateRPGMakerArtifact(artifact, files); err != nil {
			return err
		}
	}
	return nil
}

func validateRPGMakerArtifact(
	artifact RPGMakerArtifact,
	files map[string]RPGMakerRuntimeFile,
) error {
	route, err := routing.ByRoute(artifact.CoreID, artifact.RouteKey)
	if err != nil || !artifactMatchesRoute(artifact, route) {
		return fmt.Errorf("%w: RPG Maker artifact %s", ErrInvalid, artifact.RouteKey)
	}
	entries, err := rpgArtifactSetEntries(artifact, files)
	if err != nil {
		return err
	}
	entry, exists := files[path.Join(artifact.RuntimeVersion, artifact.EntryPath)]
	if !exists || entry.SizeBytes != artifact.EntrySizeBytes || entry.SHA256 != artifact.EntrySHA256 ||
		rpgArtifactSetDigest(entries) != artifact.ArtifactSetSHA256 || !validRPGCompatibility(artifact) {
		return fmt.Errorf("%w: RPG Maker artifact bytes", ErrInvalid)
	}
	return nil
}

func artifactMatchesRoute(artifact RPGMakerArtifact, route routing.Entry) bool {
	return artifact.Generation == string(route.Generation) &&
		artifact.RuntimeAdapterKind == string(route.AdapterKind) && artifact.RuntimeVersion == route.RuntimeVersion &&
		artifact.AdapterID == route.AdapterID && artifact.AdapterABI == route.AdapterABI &&
		artifact.RequiresThreads == route.RequiresThreads && artifact.SavePayloadKind == route.SavePayloadKind &&
		artifact.SaveMaxBytes == route.SaveMaxBytes &&
		artifact.SelectedForNewBindings == route.SelectedForNewBindings && artifact.AvailableForLaunch &&
		safeRelative(artifact.EntryPath) && validSHA256(artifact.EntrySHA256) &&
		validSHA256(artifact.ArtifactSetSHA256) && len(artifact.FilePaths) > 0
}

func rpgArtifactSetEntries(
	artifact RPGMakerArtifact,
	files map[string]RPGMakerRuntimeFile,
) ([]artifactSetEntry, error) {
	entries := make([]artifactSetEntry, 0, len(artifact.FilePaths))
	paths := append([]string(nil), artifact.FilePaths...)
	sort.Strings(paths)
	for index, filePath := range paths {
		duplicate := index > 0 && paths[index-1] == filePath
		if duplicate || !strings.HasPrefix(filePath, artifact.RuntimeVersion+"/") {
			return nil, fmt.Errorf("%w: RPG Maker artifact file path", ErrInvalid)
		}
		file, exists := files[filePath]
		if !exists {
			return nil, fmt.Errorf("%w: RPG Maker artifact file unavailable", ErrInvalid)
		}
		entries = append(entries, artifactSetEntry{Path: file.Path, SHA256: file.SHA256, SizeBytes: file.SizeBytes})
	}
	return entries, nil
}

func rpgArtifactSetDigest(entries []artifactSetEntry) string {
	canonical, _ := json.Marshal(entries)
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func validRPGCompatibility(artifact RPGMakerArtifact) bool {
	var value map[string]any
	if json.Unmarshal(artifact.Compatibility, &value) != nil || value["adapterAbi"] != artifact.AdapterABI {
		return false
	}
	switch artifact.RuntimeAdapterKind {
	case "EASYRPG_WEB":
		return value["jsPath"] == "easyrpg-player.js" && value["wasmPath"] == "easyrpg-player.wasm"
	case "MKXP_LIBRETRO_WEB":
		return value["jsPath"] == "mkxp-z_libretro.js" && value["wasmPath"] == "mkxp-z_libretro.wasm" &&
			value["bridgePath"] == "retrom_position.rb"
	case "NATIVE_WEB":
		return value["bridgePath"] == "native-bridge.js" &&
			(value["bridgeProfile"] == "mv-v1" || value["bridgeProfile"] == "mz-v1")
	default:
		return false
	}
}

func validateRPGMakerSources(sources []RPGMakerSourceArchive) error {
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if source.ComponentID == "" || source.Repository == "" || len(source.Commit) != 40 ||
			!strings.HasPrefix(source.ArchiveURL, "https://") || source.SizeBytes <= 0 ||
			!validSHA256(source.SHA256) || !safeRelative(source.LicensePath) {
			return fmt.Errorf("%w: RPG Maker source archive", ErrInvalid)
		}
		if _, duplicate := seen[source.ComponentID]; duplicate {
			return fmt.Errorf("%w: duplicate RPG Maker source component", ErrInvalid)
		}
		seen[source.ComponentID] = struct{}{}
	}
	return nil
}

func validateRPGMakerBuild(version *RPGMakerVersion) error {
	build := version.Manifest.Build
	if !validRPGMakerBuildToolchain(build) {
		return fmt.Errorf("%w: RPG Maker build toolchain", ErrInvalid)
	}
	if err := validateRPGMakerBuildRecipe(version, build.RecipePath); err != nil {
		return err
	}
	for _, input := range []struct{ path, digest string }{
		{build.EasyRPGPatchPath, build.EasyRPGPatchSHA256},
		{build.MKXPBridgePath, build.MKXPBridgeSHA256},
		{build.NativeBridgeV3Path, build.NativeBridgeV3SHA256},
	} {
		if err := validateRPGMakerBuildInput(version, input.path, input.digest); err != nil {
			return err
		}
	}
	return validateRPGMakerNativeBridges(version, build)
}

func validRPGMakerBuildToolchain(build RPGMakerBuild) bool {
	return build.EasyRPGEmscriptenVersion == "3.1.74" && build.MKXPEmscriptenVersion == "4.0.8" &&
		build.WASISDKVersion == "30" && build.BinaryenVersion == "126"
}

func validateRPGMakerBuildRecipe(version *RPGMakerVersion, recipePath string) error {
	if !safeRelative(recipePath) {
		return fmt.Errorf("%w: RPG Maker build recipe", ErrInvalid)
	}
	if _, err := os.Stat(filepath.Join(version.DATRoot, recipePath)); err != nil {
		return fmt.Errorf("%w: RPG Maker build recipe unavailable", ErrInvalid)
	}
	return nil
}

func validateRPGMakerBuildInput(version *RPGMakerVersion, inputPath, expectedDigest string) error {
	if !safeRelative(inputPath) || !validSHA256(expectedDigest) {
		return fmt.Errorf("%w: RPG Maker build input", ErrInvalid)
	}
	contents, err := os.ReadFile(filepath.Join(version.DATRoot, inputPath))
	if err != nil {
		return fmt.Errorf("%w: RPG Maker build input unavailable", ErrInvalid)
	}
	digest := sha256.Sum256(contents)
	if hex.EncodeToString(digest[:]) != expectedDigest {
		return fmt.Errorf("%w: RPG Maker build input digest", ErrInvalid)
	}
	return nil
}

func validateRPGMakerNativeBridges(version *RPGMakerVersion, build RPGMakerBuild) error {
	current, currentExists := version.Allowlist["v3/native-bridge.js"]
	if !currentExists || current.SHA256 != build.NativeBridgeV3SHA256 {
		return fmt.Errorf("%w: RPG Maker native bridge versions", ErrInvalid)
	}
	return nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func (set *Set) RPGMakerFile(runtimeVersion, runtimePath string) (string, RPGMakerRuntimeFile, bool) {
	if set == nil || set.RPGMaker == nil || !safeRelative(runtimeVersion) || !safeRelative(runtimePath) ||
		strings.Contains(runtimeVersion, "/") {
		return "", RPGMakerRuntimeFile{}, false
	}
	relative := path.Join(runtimeVersion, runtimePath)
	file, exists := set.RPGMaker.Allowlist[relative]
	if !exists || relative != runtimeVersion+"/"+runtimePath {
		return "", RPGMakerRuntimeFile{}, false
	}
	return filepath.Join(set.RPGMaker.RuntimeRoot, filepath.FromSlash(relative)), file, true
}
