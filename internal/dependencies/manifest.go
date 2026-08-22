package dependencies

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"retrom/internal/cleanup"
)

var (
	ErrInvalid          = errors.New("DEPENDENCY_INVALID")
	errDATJobNotClaimed = errors.New("DEPENDENCY_DAT_JOB_NOT_CLAIMABLE")
	errDATParseFailed   = errors.New("DEPENDENCY_DAT_PARSE_FAILED")
	errBIOSOptions      = errors.New("DEPENDENCY_BIOS_ACTIVATION_OPTIONS_INVALID")
)

type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	EmulatorJS    struct {
		Version       string `json:"version"`
		PlayerAdapter struct {
			ID              string `json:"id"`
			RuntimeBasePath string `json:"runtime_base_path_in_release"`
			LoaderPath      string `json:"loader_path_in_release"`
		} `json:"player_adapter"`
		RuntimeAllowlist []File         `json:"runtime_allowlist"`
		SelectedCores    []SelectedCore `json:"selected_core_artifacts"`
		AuxiliaryFiles   []struct {
			ComponentID string `json:"component_id"`
			Path        string `json:"path_in_release"`
			SizeBytes   int64  `json:"size_bytes"`
			SHA256      string `json:"sha256"`
		} `json:"auxiliary_files"`
	} `json:"emulatorjs"`
	Cores []struct {
		CoreID     string `json:"core_id"`
		CoreSource struct {
			Commit            string `json:"commit"`
			AssociationStatus string `json:"association_status"`
		} `json:"core_source"`
		DAT *struct {
			LocalPath string `json:"local_path"`
			SizeBytes int64  `json:"size_bytes"`
			SHA256    string `json:"sha256"`
		} `json:"dat"`
		ParseStats struct {
			MachineCount              int64 `json:"machine_count"`
			ROMEntryCount             int64 `json:"rom_entry_count"`
			DiskEntryCount            int64 `json:"disk_entry_count"`
			BIOSSetCount              int64 `json:"bios_set_count"`
			DefaultBIOSSetCount       int64 `json:"default_bios_set_count"`
			ExplicitBIOSMachineCount  int64 `json:"explicit_bios_machine_count"`
			BaseDependencyTargetCount int64 `json:"base_dependency_target_count"`
			UnresolvedCloneofCount    int64 `json:"unresolved_cloneof_target_count"`
			UnresolvedRomofCount      int64 `json:"unresolved_romof_target_count"`
		} `json:"parse_stats"`
		Override *struct {
			BundleVersion string `json:"core_bundle_emulatorjs_version"`
		} `json:"tested_runtime_override"`
	} `json:"cores"`
	Licenses struct {
		NoticePath  string   `json:"third_party_notices_relative_path"`
		NoticeOrder []string `json:"notice_order"`
		Components  []struct {
			ComponentID             string `json:"component_id"`
			SourceCommit            string `json:"source_commit"`
			BinaryAssociationStatus string `json:"binary_association_status"`
			Repository              string `json:"repository"`
			LicenseFiles            []struct {
				OutputPath string `json:"output_relative_path"`
				SizeBytes  int64  `json:"size_bytes"`
				SHA256     string `json:"sha256"`
			} `json:"license_files"`
		} `json:"components"`
	} `json:"license_materialization"`
}

type SelectedCore struct {
	CoreID                    string            `json:"core_id"`
	SourceComponentID         string            `json:"source_component_id"`
	RuntimeCoreID             string            `json:"runtime_core_id"`
	PathInRelease             *string           `json:"path_in_release"`
	LocalPath                 string            `json:"local_path"`
	SizeBytes                 int64             `json:"size_bytes"`
	SHA256                    string            `json:"sha256"`
	Threads                   bool              `json:"requires_threads"`
	BundleVersion             string            `json:"bundle_version"`
	ArtifactFlavor            string            `json:"artifact_flavor"`
	RequestedArtifactBasename string            `json:"requested_artifact_basename"`
	CanvasResizePolicy        string            `json:"canvas_resize_policy"`
	DefaultOptions            map[string]string `json:"default_options"`
	PersistentSaveMode        string            `json:"persistent_save_mode"`
	PersistentSaveKind        *string           `json:"persistent_save_kind"`
	InputMode                 string            `json:"input_mode"`
	StartupActions            []StartupAction   `json:"startup_actions"`
	ReportPath                string            `json:"report_path"`
	SupportedContentKinds     []string          `json:"supported_content_kinds"`
	MultiDisc                 *MultiDisc        `json:"multi_disc"`
}

type MultiDisc struct {
	MaxDiscs      int    `json:"max_discs"`
	MaxTotalBytes int64  `json:"max_total_bytes"`
	Delivery      string `json:"delivery"`
}

type StartupAction struct {
	Event      string `json:"event"`
	Kind       string `json:"kind"`
	DelayMS    int    `json:"delayMs"`
	Player     int    `json:"player"`
	Control    int    `json:"control"`
	DurationMS int    `json:"durationMs"`
}

type File struct {
	Path      string `json:"path_in_release"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
	Role      string `json:"role"`
}

type Version struct {
	Manifest       Manifest
	ManifestSHA256 string
	DATRoot        string
	RuntimeRoot    string
	Allowlist      map[string]File
}

type Set struct {
	Versions map[string]*Version
	Order    []string
	Active   *Version
}

func Load(root string, versions []string, active string) (*Set, error) {
	result := &Set{Versions: make(map[string]*Version, len(versions)), Order: append([]string(nil), versions...)}
	for _, versionName := range versions {
		version, err := loadVersion(root, versionName)
		if err != nil {
			return nil, err
		}
		result.Versions[versionName] = version
	}
	result.Active = result.Versions[active]
	if result.Active == nil {
		return nil, fmt.Errorf("%w: active version", ErrInvalid)
	}
	return result, nil
}

func loadVersion(root, versionName string) (*Version, error) {
	datRoot := filepath.Join(root, "dat", "emulatorjs", versionName)
	runtimeRoot := filepath.Join(root, "runtime", "emulatorjs", versionName)
	manifest, digest, err := loadManifest(datRoot, versionName)
	if err != nil {
		return nil, err
	}
	version := &Version{
		Manifest: manifest, ManifestSHA256: digest,
		DATRoot: datRoot, RuntimeRoot: runtimeRoot, Allowlist: make(map[string]File),
	}
	if err := loadRuntimeFiles(version); err != nil {
		return nil, err
	}
	if err := loadDATFiles(version); err != nil {
		return nil, err
	}
	if err := loadLicenseFiles(version); err != nil {
		return nil, err
	}
	return version, nil
}

func loadManifest(datRoot, versionName string) (Manifest, string, error) {
	manifestPath := filepath.Join(datRoot, "manifest.json")
	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Version and manifest slot are strict allowlist values.
	if err != nil {
		return Manifest{}, "", fmt.Errorf("%w: manifest unavailable", ErrInvalid)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, "", fmt.Errorf("%w: manifest schema", ErrInvalid)
	}
	validSchema := manifest.SchemaVersion == 4 || manifest.SchemaVersion == 5 || manifest.SchemaVersion == 6
	if !validSchema || manifest.EmulatorJS.Version != versionName {
		return Manifest{}, "", fmt.Errorf("%w: manifest version", ErrInvalid)
	}
	manifestDigest := sha256.Sum256(contents)
	return manifest, hex.EncodeToString(manifestDigest[:]), nil
}

func loadRuntimeFiles(version *Version) error {
	manifest := version.Manifest
	for _, file := range manifest.EmulatorJS.RuntimeAllowlist {
		if !safeRelative(file.Path) {
			return fmt.Errorf("%w: runtime path", ErrInvalid)
		}
		if err := checkFile(version.RuntimeRoot, file.Path, file.SizeBytes, file.SHA256); err != nil {
			return err
		}
		version.Allowlist[file.Path] = file
	}
	if err := validateManifestCapabilities(manifest, version.Allowlist); err != nil {
		return err
	}
	for _, core := range manifest.EmulatorJS.SelectedCores {
		path := core.LocalPath
		if core.PathInRelease != nil {
			path = *core.PathInRelease
		}
		if err := checkFile(version.RuntimeRoot, path, core.SizeBytes, core.SHA256); err != nil {
			return err
		}
		version.Allowlist[path] = File{Path: path, SizeBytes: core.SizeBytes, SHA256: core.SHA256, Role: "core"}
	}
	return nil
}

func loadDATFiles(version *Version) error {
	for _, core := range version.Manifest.Cores {
		if core.DAT == nil {
			continue
		}
		if err := checkFile(version.DATRoot, core.DAT.LocalPath, core.DAT.SizeBytes, core.DAT.SHA256); err != nil {
			return err
		}
	}
	return nil
}

func loadLicenseFiles(version *Version) error {
	for _, component := range version.Manifest.Licenses.Components {
		for _, license := range component.LicenseFiles {
			if err := checkFile(version.RuntimeRoot, license.OutputPath, license.SizeBytes, license.SHA256); err != nil {
				return err
			}
		}
	}
	if version.Manifest.Licenses.NoticePath == "" {
		return fmt.Errorf("%w: notice path", ErrInvalid)
	}
	if _, err := os.Stat(filepath.Join(version.RuntimeRoot, version.Manifest.Licenses.NoticePath)); err != nil {
		return fmt.Errorf("%w: notice unavailable", ErrInvalid)
	}
	return nil
}

var runtimeCorePattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

func validateManifestCapabilities(manifest Manifest, allowlist map[string]File) error {
	if err := validateLicenseComponents(manifest); err != nil {
		return err
	}
	coreIDs, err := validateSelectedCores(manifest, allowlist)
	if err != nil {
		return err
	}
	return validateAuxiliaryFiles(manifest, allowlist, coreIDs)
}

func validateLicenseComponents(manifest Manifest) error {
	componentIDs := make(map[string]struct{}, len(manifest.Licenses.Components))
	for index, component := range manifest.Licenses.Components {
		if component.ComponentID == "" || len(component.LicenseFiles) == 0 {
			return fmt.Errorf("%w: license component", ErrInvalid)
		}
		if _, duplicate := componentIDs[component.ComponentID]; duplicate {
			return fmt.Errorf("%w: duplicate license component", ErrInvalid)
		}
		componentIDs[component.ComponentID] = struct{}{}
		if index >= len(manifest.Licenses.NoticeOrder) || manifest.Licenses.NoticeOrder[index] != component.ComponentID {
			return fmt.Errorf("%w: license order", ErrInvalid)
		}
		previous := ""
		for _, license := range component.LicenseFiles {
			if !safeRelative(license.OutputPath) || license.OutputPath <= previous {
				return fmt.Errorf("%w: license path order", ErrInvalid)
			}
			previous = license.OutputPath
		}
	}
	if len(manifest.Licenses.NoticeOrder) != len(manifest.Licenses.Components) ||
		len(manifest.Licenses.NoticeOrder) != len(manifest.EmulatorJS.SelectedCores)+1 ||
		len(manifest.Licenses.NoticeOrder) == 0 || manifest.Licenses.NoticeOrder[0] != "emulatorjs" {
		return fmt.Errorf("%w: license selection", ErrInvalid)
	}
	return nil
}

func validateSelectedCores(manifest Manifest, allowlist map[string]File) (map[string]struct{}, error) {
	coreIDs := make(map[string]struct{}, len(manifest.EmulatorJS.SelectedCores))
	runtimeIDs := make(map[string]struct{}, len(manifest.EmulatorJS.SelectedCores))
	for index, core := range manifest.EmulatorJS.SelectedCores {
		if core.CoreID == "" || core.SourceComponentID != core.CoreID ||
			manifest.Licenses.NoticeOrder[index+1] != core.SourceComponentID {
			return nil, fmt.Errorf("%w: core component", ErrInvalid)
		}
		if _, duplicate := coreIDs[core.CoreID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate core", ErrInvalid)
		}
		if !runtimeCorePattern.MatchString(core.RuntimeCoreID) {
			return nil, fmt.Errorf("%w: runtime core", ErrInvalid)
		}
		if _, duplicate := runtimeIDs[core.RuntimeCoreID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate runtime core", ErrInvalid)
		}
		coreIDs[core.CoreID] = struct{}{}
		runtimeIDs[core.RuntimeCoreID] = struct{}{}
		if err := validateSelectedCore(core, allowlist, manifest.SchemaVersion); err != nil {
			return nil, err
		}
	}
	return coreIDs, nil
}

func validateAuxiliaryFiles(manifest Manifest, allowlist map[string]File, coreIDs map[string]struct{}) error {
	for _, auxiliary := range manifest.EmulatorJS.AuxiliaryFiles {
		if _, exists := coreIDs[auxiliary.ComponentID]; !exists || !safeRelative(auxiliary.Path) {
			return fmt.Errorf("%w: auxiliary declaration", ErrInvalid)
		}
		if file, exists := allowlist[auxiliary.Path]; !exists ||
			file.SizeBytes != auxiliary.SizeBytes || file.SHA256 != auxiliary.SHA256 {
			return fmt.Errorf("%w: auxiliary allowlist", ErrInvalid)
		}
	}
	return nil
}

func validateSelectedCore(core SelectedCore, allowlist map[string]File, manifestSchemaVersion int) error {
	path := core.LocalPath
	if core.PathInRelease != nil {
		path = *core.PathInRelease
	}
	if !validArtifactIdentity(core, path) {
		return fmt.Errorf("%w: artifact basename", ErrInvalid)
	}
	if !validArtifactCapability(core) {
		return fmt.Errorf("%w: artifact capability", ErrInvalid)
	}
	if !validDefaultOptions(core.DefaultOptions) {
		return fmt.Errorf("%w: artifact options", ErrInvalid)
	}
	if err := validateRuntimeCapability(core, manifestSchemaVersion); err != nil {
		return err
	}
	if !safeRelative(core.ReportPath) {
		return fmt.Errorf("%w: core report", ErrInvalid)
	}
	if _, exists := allowlist[core.ReportPath]; !exists {
		return fmt.Errorf("%w: core report allowlist", ErrInvalid)
	}
	return validateContentCapabilities(core, manifestSchemaVersion)
}

func validArtifactIdentity(core SelectedCore, path string) bool {
	basename := core.RequestedArtifactBasename
	return safeRelative(path) && filepath.Base(basename) == basename && !strings.Contains(basename, "..") &&
		strings.HasSuffix(basename, "-wasm.data") && strings.HasSuffix(basename, "-thread-wasm.data") == core.Threads
}

func validArtifactCapability(core SelectedCore) bool {
	expectedFlavor := "WASM"
	if core.Threads {
		expectedFlavor = "THREAD_WASM"
	}
	if core.PathInRelease == nil {
		expectedFlavor = "OVERRIDE"
	}
	return core.BundleVersion != "" && core.ArtifactFlavor == expectedFlavor &&
		(core.CanvasResizePolicy == "NONE" || core.CanvasResizePolicy == "ON_GAME_START_TO_CSS_PIXELS")
}

func validDefaultOptions(options map[string]string) bool {
	if len(options) > 32 {
		return false
	}
	for name, value := range options {
		if name == "__proto__" || name == "constructor" || name == "prototype" ||
			!validASCIIOption(name, 1) || !validASCIIOption(value, 0) {
			return false
		}
	}
	return true
}

func validateRuntimeCapability(core SelectedCore, manifestSchemaVersion int) error {
	if !validPersistentSaveCapability(core, manifestSchemaVersion) ||
		core.InputMode != "STANDARD" && core.InputMode != "POINTER" || len(core.StartupActions) > 4 {
		return fmt.Errorf("%w: artifact runtime capability", ErrInvalid)
	}
	for _, action := range core.StartupActions {
		if !validStartupAction(action) {
			return fmt.Errorf("%w: startup action", ErrInvalid)
		}
	}
	return nil
}

func validPersistentSaveCapability(core SelectedCore, manifestSchemaVersion int) bool {
	expectedKind := map[string]*string{
		"SINGLE_FILE": pointerTo("CORE_SAVE"), "DOS_OVERLAY": pointerTo("DOS_OVERLAY"),
		"FILE_TREE": pointerTo("CORE_SAVE"), "AUTO_STATE": pointerTo("CORE_SAVE"), "NONE": nil,
	}
	kind, exists := expectedKind[core.PersistentSaveMode]
	if !exists || !equalOptionalString(core.PersistentSaveKind, kind) {
		return false
	}
	if manifestSchemaVersion >= 6 {
		return true
	}
	return core.PersistentSaveMode != "AUTO_STATE" &&
		(core.PersistentSaveMode != "FILE_TREE" || core.RuntimeCoreID == "ppsspp")
}

func validStartupAction(action StartupAction) bool {
	return action.Event == "GAME_START" && action.Kind == "PRESS_CONTROL" &&
		action.DelayMS >= 0 && action.DelayMS <= 30_000 && action.Player >= 0 && action.Player <= 3 &&
		action.Control >= 0 && action.Control <= 255 && action.DurationMS >= 1 && action.DurationMS <= 1_000
}

func validateContentCapabilities(core SelectedCore, manifestSchemaVersion int) error {
	if manifestSchemaVersion == 4 {
		if len(core.SupportedContentKinds) != 0 || core.MultiDisc != nil {
			return fmt.Errorf("%w: legacy content capability", ErrInvalid)
		}
		return nil
	}
	if !validSupportedContentKinds(core) {
		return fmt.Errorf("%w: content capability", ErrInvalid)
	}
	if core.MultiDisc == nil {
		if len(core.SupportedContentKinds) != 1 {
			return fmt.Errorf("%w: multi-disc capability", ErrInvalid)
		}
		return nil
	}
	if core.CoreID != "yabause" || len(core.SupportedContentKinds) != 2 || core.MultiDisc.MaxDiscs != 8 ||
		core.MultiDisc.MaxTotalBytes != 1_073_741_824 || core.MultiDisc.Delivery != "EAGER_EXTERNAL_FILES" {
		return fmt.Errorf("%w: multi-disc capability", ErrInvalid)
	}
	return nil
}

func validSupportedContentKinds(core SelectedCore) bool {
	switch len(core.SupportedContentKinds) {
	case 1:
		return core.SupportedContentKinds[0] == expectedContentKind(core.CoreID)
	case 2:
		return core.SupportedContentKinds[0] == expectedContentKind(core.CoreID) &&
			core.SupportedContentKinds[1] == "MULTI_DISC_M3U_V1"
	default:
		return false
	}
}

func expectedContentKind(coreID string) string {
	if coreID == "dosbox_pure" {
		return "DOS_BUNDLE"
	}
	return "SINGLE_FILE"
}

func pointerTo(value string) *string { return &value }

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func checkFile(root, relative string, expectedSize int64, expectedDigest string) error {
	if !safeRelative(relative) || len(expectedDigest) != 64 || expectedDigest != strings.ToLower(expectedDigest) {
		return fmt.Errorf("%w: file declaration", ErrInvalid)
	}
	file, err := os.Open( //nolint:gosec // Manifest-allowlisted safe relative path.
		filepath.Join(root, filepath.FromSlash(relative)),
	)
	if err != nil {
		return fmt.Errorf("%w: payload unavailable", ErrInvalid)
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	digest := sha256.New()
	size, err := io.Copy(digest, file)
	if err != nil || size != expectedSize || hex.EncodeToString(digest.Sum(nil)) != expectedDigest {
		return fmt.Errorf("%w: payload mismatch", ErrInvalid)
	}
	return nil
}

func safeRelative(value string) bool {
	if value == "" || strings.Contains(value, "\\") || strings.ContainsRune(value, 0) ||
		filepath.IsAbs(value) || filepath.Clean(value) != filepath.FromSlash(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
