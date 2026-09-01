package dependencies

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"retrom/internal/rpgmaker/routing"
)

const rpgMakerManifestVersion = "v1"

var (
	runtimeCompatibilityIdentity = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,118}-v[1-9][0-9]*$`)
	errInvalidPFBOverlay         = errors.New("invalid PFB overlay")
	errInvalidPFBOverlayDigest   = errors.New("invalid PFB overlay digest")
)

type RPGMakerManifest struct {
	SchemaVersion int                    `json:"schema_version"`
	RuntimeID     string                 `json:"runtime_id"`
	Release       RPGMakerRuntimeRelease `json:"release"`
	RuntimeFiles  []RPGMakerRuntimeFile  `json:"runtime_files"`
	Artifacts     []RPGMakerArtifact     `json:"artifacts"`
}

type RPGMakerRuntimeFile struct {
	BundlePath   string `json:"bundle_path"`
	Path         string `json:"path_in_release"`
	Role         string `json:"role"`
	MaxSizeBytes int64  `json:"max_size_bytes"`
	SizeBytes    int64  `json:"-"`
	SHA256       string `json:"-"`
}

type RPGMakerArtifact struct {
	CoreID                 string          `json:"core_id"`
	RuntimeFamily          string          `json:"runtime_family"`
	Generation             string          `json:"generation"`
	RouteKey               string          `json:"route_key"`
	RuntimeAdapterKind     string          `json:"runtime_adapter_kind"`
	RuntimeVersion         string          `json:"runtime_version"`
	AdapterID              string          `json:"adapter_id"`
	AdapterABI             string          `json:"adapter_abi"`
	EntryPath              string          `json:"entry_path"`
	EntrySizeBytes         int64           `json:"-"`
	EntrySHA256            string          `json:"-"`
	ArtifactSetSHA256      string          `json:"-"`
	FilePaths              []string        `json:"file_paths"`
	RequiresThreads        bool            `json:"requires_threads"`
	SavePayloadKind        string          `json:"save_payload_kind"`
	SaveMaxBytes           int64           `json:"save_max_bytes"`
	SelectedForNewBindings bool            `json:"selected_for_new_bindings"`
	AvailableForLaunch     bool            `json:"available_for_launch"`
	Compatibility          json.RawMessage `json:"compatibility"`
}

type RPGMakerVersion struct {
	Manifest       RPGMakerManifest
	ManifestSHA256 string
	RuntimeRoot    string
	Allowlist      map[string]RPGMakerRuntimeFile
	PFBCandidate   bool
}

type rpgMakerPFBCandidate struct {
	SchemaVersion        int                   `json:"schemaVersion"`
	Kind                 string                `json:"kind"`
	PFBID                string                `json:"pfbId"`
	FormalManifestSHA256 string                `json:"formalManifestSha256"`
	Runtime              json.RawMessage       `json:"runtime"`
	Cores                json.RawMessage       `json:"cores"`
	RuntimeFiles         []RPGMakerRuntimeFile `json:"runtimeFiles"`
	Artifacts            []RPGMakerArtifact    `json:"artifacts"`
	FilesSHA256          string                `json:"filesSha256"`
	OverlaySHA256        string                `json:"overlaySha256"`
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
	manifestSHA256 := hex.EncodeToString(digest[:])
	candidate, pfbEnabled, err := loadRPGMakerPFBCandidate(runtimeRoot, manifestSHA256)
	if err != nil {
		return nil, err
	}
	if pfbEnabled {
		manifest.RuntimeFiles = append(manifest.RuntimeFiles, candidate.RuntimeFiles...)
		manifest.Artifacts = append(manifest.Artifacts, candidate.Artifacts...)
	}
	version := &RPGMakerVersion{
		Manifest: manifest, ManifestSHA256: manifestSHA256, RuntimeRoot: runtimeRoot,
		Allowlist:    make(map[string]RPGMakerRuntimeFile, len(manifest.RuntimeFiles)),
		PFBCandidate: pfbEnabled,
	}
	if err := hydrateRPGMakerReleaseFiles(version); err != nil {
		return nil, err
	}
	if err := validateRPGMakerVersion(version); err != nil {
		return nil, err
	}
	return version, nil
}

func loadRPGMakerPFBCandidate(
	runtimeRoot string, formalManifestSHA256 string,
) (rpgMakerPFBCandidate, bool, error) {
	contents, err := os.ReadFile(filepath.Join(runtimeRoot, ".retrom-pfb-candidate.json"))
	if errors.Is(err, os.ErrNotExist) {
		return rpgMakerPFBCandidate{}, false, nil
	}
	if err != nil {
		return rpgMakerPFBCandidate{}, false, fmt.Errorf("%w: PFB_CANDIDATE_FORBIDDEN", ErrInvalid)
	}
	identifier := os.Getenv("RETROM_PFB_ID")
	overlaySHA256, overlayErr := calculateRPGMakerPFBOverlaySHA256(contents)
	marker, decodeErr := decodeRPGMakerPFBCandidate(contents)
	if decodeErr != nil || !validRPGMakerPFBCandidate(
		marker, identifier, formalManifestSHA256, overlaySHA256, overlayErr,
	) {
		return rpgMakerPFBCandidate{}, false, fmt.Errorf("%w: PFB_CANDIDATE_FORBIDDEN", ErrInvalid)
	}
	return marker, true, nil
}

func decodeRPGMakerPFBCandidate(contents []byte) (rpgMakerPFBCandidate, error) {
	var marker rpgMakerPFBCandidate
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return rpgMakerPFBCandidate{}, fmt.Errorf("decode PFB overlay: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return rpgMakerPFBCandidate{}, fmt.Errorf("decode PFB overlay trailing data: %w", err)
	}
	return marker, nil
}

func validRPGMakerPFBCandidate(
	marker rpgMakerPFBCandidate,
	identifier, formalManifestSHA256, overlaySHA256 string,
	overlayErr error,
) bool {
	if identifier == "" || marker.SchemaVersion != 1 || marker.Kind != "RETROM_PFB_CANDIDATE_V1" {
		return false
	}
	if marker.PFBID != identifier || marker.FormalManifestSHA256 != formalManifestSHA256 {
		return false
	}
	if !validSHA256(marker.FilesSHA256) || !validSHA256(marker.OverlaySHA256) {
		return false
	}
	if overlayErr != nil || marker.OverlaySHA256 != overlaySHA256 {
		return false
	}
	return validRPGMakerPFBCandidatePayload(marker)
}

func validRPGMakerPFBCandidatePayload(marker rpgMakerPFBCandidate) bool {
	if len(marker.Runtime) == 0 || len(marker.Cores) == 0 {
		return false
	}
	if marker.RuntimeFiles == nil || marker.Artifacts == nil {
		return false
	}
	return len(marker.RuntimeFiles) <= 64 && len(marker.Artifacts) <= 32
}

func calculateRPGMakerPFBOverlaySHA256(contents []byte) (string, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || requireJSONEOF(decoder) != nil || len(value) != 10 {
		return "", errInvalidPFBOverlay
	}
	if _, ok := value["overlaySha256"].(string); !ok {
		return "", errInvalidPFBOverlayDigest
	}
	delete(value, "overlaySha256")
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", fmt.Errorf("encode PFB overlay: %w", err)
	}
	digest := sha256.Sum256(bytes.TrimSuffix(canonical.Bytes(), []byte("\n")))
	return hex.EncodeToString(digest[:]), nil
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
	minimumFiles := 24
	minimumArtifacts := len(routing.Entries()) + 4
	validCounts := len(manifest.RuntimeFiles) == minimumFiles && len(manifest.Artifacts) == minimumArtifacts
	if version.PFBCandidate {
		validCounts = len(manifest.RuntimeFiles) >= minimumFiles && len(manifest.Artifacts) >= minimumArtifacts
	}
	if manifest.SchemaVersion != 3 || manifest.RuntimeID != "retrom-runtime" || !validCounts {
		return fmt.Errorf("%w: RPG Maker manifest identity", ErrInvalid)
	}
	if err := validateRPGMakerRuntimeFiles(version); err != nil {
		return err
	}
	if err := hydrateRPGMakerArtifacts(manifest.Artifacts, version.Allowlist); err != nil {
		return err
	}
	version.Manifest.Artifacts = manifest.Artifacts
	return validateRPGMakerArtifacts(manifest.Artifacts, version.Allowlist)
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
	validRole := file.Role == "runtime_js" || file.Role == "runtime_wasm" ||
		file.Role == "adapter_bridge" || file.Role == "runtime_asset" || file.Role == "license"
	return safeRelative(file.BundlePath) && safeRelative(file.Path) && file.MaxSizeBytes > 0 &&
		file.SizeBytes > 0 && file.SizeBytes <= file.MaxSizeBytes && validSHA256(file.SHA256) && validRole
}

func validateRPGMakerArtifacts(artifacts []RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
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

func validateRPGMakerArtifact(artifact RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
	if artifact.RuntimeFamily == "ONS" {
		return validateONSArtifact(artifact, files)
	}
	if artifact.RuntimeFamily == "KIRIKIRI" {
		return validateKiriKiriArtifact(artifact, files)
	}
	if artifact.RuntimeFamily == "BUTTERSCOTCH" {
		return validateButterscotchArtifact(artifact, files)
	}
	if artifact.RuntimeFamily == "TYRANOSCRIPT" {
		return validateTyranoScriptArtifact(artifact, files)
	}
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
		rpgArtifactSetDigest(entries) != artifact.ArtifactSetSHA256 || !validRPGCompatibility(artifact, route) {
		return fmt.Errorf("%w: RPG Maker artifact bytes", ErrInvalid)
	}
	return nil
}

func validateKiriKiriArtifact(artifact RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
	if !validKiriKiriIdentity(artifact) {
		return fmt.Errorf("%w: KiriKiri artifact identity", ErrInvalid)
	}
	entries, err := rpgArtifactSetEntries(artifact, files)
	if err != nil {
		return err
	}
	entry, exists := files[path.Join(artifact.RuntimeVersion, artifact.EntryPath)]
	if !exists || entry.SizeBytes != artifact.EntrySizeBytes || entry.SHA256 != artifact.EntrySHA256 ||
		rpgArtifactSetDigest(entries) != artifact.ArtifactSetSHA256 || !validKiriKiriCompatibility(artifact) {
		return fmt.Errorf("%w: KiriKiri artifact bytes", ErrInvalid)
	}
	return nil
}

func validKiriKiriIdentity(artifact RPGMakerArtifact) bool {
	return artifact.CoreID == "kirikiri2" && artifact.Generation == "KIRIKIRI2" &&
		artifact.RouteKey == "KIRIKIRI2_KAG" && artifact.RuntimeAdapterKind == "KIRIKIRI2_WEB" &&
		artifact.AdapterID == "kirikiri2-web" && artifact.AdapterABI == "kirikiri-kag-bookmark" &&
		artifact.EntryPath == "index.js" && artifact.RequiresThreads &&
		artifact.SavePayloadKind == "KIRIKIRI_SAVE_BUNDLE_V1" && artifact.SaveMaxBytes == 64<<20 &&
		artifact.SelectedForNewBindings && artifact.AvailableForLaunch
}

func validKiriKiriCompatibility(artifact RPGMakerArtifact) bool {
	var value map[string]any
	return json.Unmarshal(artifact.Compatibility, &value) == nil && len(value) == 9 &&
		value["adapterAbi"] == "kirikiri-kag-bookmark" && value["checkpointSlot"] == float64(1999) &&
		value["jsPath"] == "index.js" && value["wasmPath"] == "index.wasm" &&
		value["vlfsPath"] == "vlfs.js" && value["assetsPath"] == "assets.zip" &&
		validRuntimeCompatibilityContract(value)
}

func validateButterscotchArtifact(artifact RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
	if !validButterscotchIdentity(artifact) {
		return fmt.Errorf("%w: Butterscotch artifact identity", ErrInvalid)
	}
	entries, err := rpgArtifactSetEntries(artifact, files)
	if err != nil {
		return err
	}
	entry, exists := files[path.Join(artifact.RuntimeVersion, artifact.EntryPath)]
	if !exists || entry.SizeBytes != artifact.EntrySizeBytes || entry.SHA256 != artifact.EntrySHA256 ||
		rpgArtifactSetDigest(entries) != artifact.ArtifactSetSHA256 || !validButterscotchCompatibility(artifact) {
		return fmt.Errorf("%w: Butterscotch artifact bytes", ErrInvalid)
	}
	return nil
}

func validButterscotchIdentity(artifact RPGMakerArtifact) bool {
	return artifact.CoreID == "butterscotch" && artifact.Generation == "BUTTERSCOTCH" &&
		artifact.RouteKey == "BUTTERSCOTCH_GAMEMAKER" && artifact.RuntimeAdapterKind == "BUTTERSCOTCH_WEB" &&
		artifact.AdapterID == "butterscotch-web" && artifact.AdapterABI == "butterscotch-checkpoint-v2" &&
		artifact.EntryPath == "butterscotch.mjs" && artifact.RequiresThreads &&
		artifact.SavePayloadKind == "RUNTIME_STATE" && artifact.SaveMaxBytes == 16<<20 &&
		artifact.SelectedForNewBindings && artifact.AvailableForLaunch
}

func validButterscotchCompatibility(artifact RPGMakerArtifact) bool {
	var value map[string]any
	return json.Unmarshal(artifact.Compatibility, &value) == nil && len(value) == 7 &&
		value["adapterAbi"] == "butterscotch-checkpoint-v2" &&
		value["jsPath"] == "butterscotch.mjs" && value["wasmPath"] == "butterscotch.wasm" &&
		value["workerPath"] == "butterscotch-worker.mjs" && validRuntimeCompatibilityContract(value)
}

func validateTyranoScriptArtifact(artifact RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
	if !validTyranoScriptIdentity(artifact) {
		return fmt.Errorf("%w: TyranoScript artifact identity", ErrInvalid)
	}
	entries, err := rpgArtifactSetEntries(artifact, files)
	if err != nil {
		return err
	}
	entry, exists := files[path.Join(artifact.RuntimeVersion, artifact.EntryPath)]
	if !exists || entry.SizeBytes != artifact.EntrySizeBytes || entry.SHA256 != artifact.EntrySHA256 ||
		rpgArtifactSetDigest(entries) != artifact.ArtifactSetSHA256 || !validTyranoScriptCompatibility(artifact) {
		return fmt.Errorf("%w: TyranoScript artifact bytes", ErrInvalid)
	}
	return nil
}

func validTyranoScriptIdentity(artifact RPGMakerArtifact) bool {
	return artifact.CoreID == "tyranoscript" && artifact.Generation == "TYRANOSCRIPT" &&
		artifact.RouteKey == "TYRANOSCRIPT_WEB" && artifact.RuntimeAdapterKind == "TYRANOSCRIPT_WEB" &&
		artifact.AdapterID == "tyranoscript-web" && artifact.AdapterABI == "tyranoscript-snapshot-v1" &&
		artifact.EntryPath == "tyranoscript-bridge.js" && !artifact.RequiresThreads &&
		artifact.SavePayloadKind == "RUNTIME_STATE" && artifact.SaveMaxBytes == 32<<20 &&
		artifact.SelectedForNewBindings && artifact.AvailableForLaunch
}

func validTyranoScriptCompatibility(artifact RPGMakerArtifact) bool {
	var value map[string]any
	return json.Unmarshal(artifact.Compatibility, &value) == nil && len(value) == 5 &&
		value["adapterAbi"] == "tyranoscript-snapshot-v1" &&
		value["bridgePath"] == "tyranoscript-bridge.js" && validRuntimeCompatibilityContract(value)
}

func artifactMatchesRoute(artifact RPGMakerArtifact, route routing.Entry) bool {
	return artifact.RuntimeFamily == routing.FamilyRPGMaker && artifact.Generation == string(route.Generation) &&
		artifact.RuntimeAdapterKind == string(route.AdapterKind) &&
		artifact.RuntimeVersion == route.RuntimeVersion && artifact.AdapterID == route.AdapterID &&
		artifact.AdapterABI == route.AdapterABI && artifact.RequiresThreads == route.RequiresThreads &&
		artifact.SavePayloadKind == route.SavePayloadKind && artifact.SaveMaxBytes == route.SaveMaxBytes &&
		artifact.SelectedForNewBindings == route.SelectedForNewBindings && artifact.AvailableForLaunch &&
		safeRelative(artifact.EntryPath) && validSHA256(artifact.EntrySHA256) &&
		validSHA256(artifact.ArtifactSetSHA256) && len(artifact.FilePaths) > 0
}

func validateONSArtifact(artifact RPGMakerArtifact, files map[string]RPGMakerRuntimeFile) error {
	if !validONSIdentity(artifact) {
		return fmt.Errorf("%w: ONS artifact identity", ErrInvalid)
	}
	entries, err := rpgArtifactSetEntries(artifact, files)
	if err != nil {
		return err
	}
	entry, exists := files[path.Join(artifact.RuntimeVersion, artifact.EntryPath)]
	if !exists || entry.SizeBytes != artifact.EntrySizeBytes || entry.SHA256 != artifact.EntrySHA256 ||
		rpgArtifactSetDigest(entries) != artifact.ArtifactSetSHA256 || !validONSCompatibility(artifact) {
		return fmt.Errorf("%w: ONS artifact bytes", ErrInvalid)
	}
	return nil
}

func validONSIdentity(artifact RPGMakerArtifact) bool {
	return artifact.CoreID == "onscripter_yuri" && artifact.Generation == "ONS" &&
		artifact.RouteKey == "ONS_YURI" && artifact.RuntimeAdapterKind == "ONS_YURI_WEB" &&
		artifact.AdapterID == "ons-yuri-web" && artifact.AdapterABI == "ons-save" &&
		artifact.EntryPath == "onsyuri.js" && !artifact.RequiresThreads &&
		artifact.SavePayloadKind == "ONS_SAVE_BUNDLE_V1" && artifact.SaveMaxBytes == 64<<20 &&
		artifact.SelectedForNewBindings && artifact.AvailableForLaunch
}

func validONSCompatibility(artifact RPGMakerArtifact) bool {
	var value map[string]any
	return json.Unmarshal(artifact.Compatibility, &value) == nil && len(value) == 7 &&
		value["adapterAbi"] == "ons-save" && value["checkpointSlot"] == float64(999) &&
		value["jsPath"] == "onsyuri.js" && value["wasmPath"] == "onsyuri.wasm" &&
		validRuntimeCompatibilityContract(value)
}

func rpgArtifactSetEntries(
	artifact RPGMakerArtifact,
	files map[string]RPGMakerRuntimeFile,
) ([]artifactSetEntry, error) {
	entries := make([]artifactSetEntry, 0, len(artifact.FilePaths))
	paths := append([]string(nil), artifact.FilePaths...)
	sort.Strings(paths)
	for index, filePath := range paths {
		if index > 0 && paths[index-1] == filePath || !strings.HasPrefix(filePath, artifact.RuntimeVersion+"/") {
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

func validRPGCompatibility(artifact RPGMakerArtifact, route routing.Entry) bool {
	var value map[string]any
	if json.Unmarshal(artifact.Compatibility, &value) != nil || value["adapterAbi"] != artifact.AdapterABI ||
		!validRuntimeCompatibilityContract(value) {
		return false
	}
	switch artifact.RuntimeAdapterKind {
	case "EASYRPG_WEB":
		return validEasyRPGCompatibility(value, route)
	case "MKXP_LIBRETRO_WEB":
		return validMKXPCompatibility(value, route)
	case "NATIVE_WEB":
		return validNativeCompatibility(value, artifact.Generation)
	default:
		return false
	}
}

func validEasyRPGCompatibility(value map[string]any, route routing.Entry) bool {
	return len(value) == 7 && value["engineMode"] == route.EngineMode &&
		value["jsPath"] == "easyrpg-player.js" && value["wasmPath"] == "easyrpg-player.wasm"
}

func validMKXPCompatibility(value map[string]any, route routing.Entry) bool {
	return len(value) == 8 && value["rgssVersion"] == float64(route.RGSSVersion) &&
		value["jsPath"] == "mkxp-z_libretro.js" && value["wasmPath"] == "mkxp-z_libretro.wasm" &&
		value["bridgePath"] == "position_bridge.rb"
}

func validNativeCompatibility(value map[string]any, generation string) bool {
	return len(value) == 6 && value["bridgePath"] == "native-bridge.js" &&
		(value["bridgeProfile"] == "RPGMV" || value["bridgeProfile"] == "RPGMZ") &&
		value["bridgeProfile"] == generation
}

func validRuntimeCompatibilityContract(value map[string]any) bool {
	gameLine, gameOK := value["gameCompatibilityLine"].(string)
	saveABI, saveOK := value["saveAbi"].(string)
	readable, readableOK := value["readableSaveAbis"].([]any)
	if !gameOK || !saveOK || !readableOK || !runtimeCompatibilityIdentity.MatchString(gameLine) ||
		!runtimeCompatibilityIdentity.MatchString(saveABI) || len(readable) < 1 || len(readable) > 16 {
		return false
	}
	seen := make(map[string]struct{}, len(readable))
	for _, candidate := range readable {
		name, ok := candidate.(string)
		if !ok || !runtimeCompatibilityIdentity.MatchString(name) {
			return false
		}
		seen[name] = struct{}{}
	}
	_, writesReadableABI := seen[saveABI]
	return writesReadableABI && len(seen) == len(readable)
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func (set *Set) RetromRuntimeFile(runtimeVersion, runtimePath string) (string, RPGMakerRuntimeFile, bool) {
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
