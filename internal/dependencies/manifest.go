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
	"strings"

	"retrom/internal/cleanup"
	runtimecatalog "retrom/internal/runtime/catalog"
)

var ErrInvalid = errors.New("DEPENDENCY_INVALID")

// Manifest is the third-party DAT provenance document. Runtime selection,
// implementation assets and licenses are owned exclusively by Provider bundles.
type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	EmulatorJS    struct {
		Version string `json:"version"`
	} `json:"emulatorjs"`
	Cores []Core `json:"cores"`
}

type Core struct {
	CoreID     string `json:"core_id"`
	CoreSource struct {
		Commit            string `json:"commit"`
		AssociationStatus string `json:"association_status"`
	} `json:"core_source"`
	DAT        *DATArtifact `json:"dat"`
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
}
type DATArtifact struct {
	LocalPath string `json:"local_path"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type Version struct {
	Paired         bool
	Manifest       Manifest
	ManifestSHA256 string
	DATRoot        string
}

type Set struct {
	Versions       map[string]*Version
	Order          []string
	Active         *Version
	MAME           *Version
	RuntimeCatalog runtimecatalog.Catalog
}

// LoadProduction adds the MAME Current DAT pinned to the published Runtime
// Provider. Tests that need only an EmulatorJS fixture continue to use Load.
func LoadProduction(root string, versions []string, active string) (*Set, error) {
	result, err := Load(root, versions, active)
	if err != nil {
		return nil, err
	}
	result.MAME, err = loadMAMEVersion(root)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func loadMAMEVersion(root string) (*Version, error) {
	datRoot := filepath.Join(root, "dat", "mame-current", "v0.59.0")
	contents, err := os.ReadFile(filepath.Join(datRoot, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: MAME DAT manifest unavailable", ErrInvalid)
	}
	manifest, providerTag, err := parseMAMEManifest(contents)
	if err != nil {
		return nil, err
	}
	var release struct {
		Tag string `json:"tag"`
	}
	releaseContents, err := os.ReadFile(filepath.Join(root, "runtime-providers", "release.json"))
	if err != nil || json.Unmarshal(releaseContents, &release) != nil || release.Tag != providerTag {
		return nil, fmt.Errorf("%w: MAME DAT Provider release mismatch", ErrInvalid)
	}
	digest := sha256.Sum256(contents)
	version := &Version{Manifest: manifest, ManifestSHA256: hex.EncodeToString(digest[:]), DATRoot: datRoot}
	if err := loadDATFiles(version); err != nil {
		return nil, err
	}
	return version, nil
}

type mameReleaseIdentity struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Commit     string `json:"commit"`
	ProviderID string `json:"provider_id"`
	TargetID   string `json:"target_id"`
}

func parseMAMEManifest(contents []byte) (Manifest, string, error) {
	var identity struct {
		SchemaVersion int                 `json:"schema_version"`
		Provider      mameReleaseIdentity `json:"provider_release"`
		Core          mameReleaseIdentity `json:"core_release"`
	}
	var manifest Manifest
	if json.Unmarshal(contents, &identity) != nil || json.Unmarshal(contents, &manifest) != nil ||
		identity.SchemaVersion != 1 || !validMAMEProvider(identity.Provider) ||
		!validMAMECore(identity.Core, manifest) {
		return Manifest{}, "", fmt.Errorf("%w: MAME DAT manifest identity", ErrInvalid)
	}
	return manifest, identity.Provider.Tag, nil
}

func validMAMEProvider(provider mameReleaseIdentity) bool {
	return provider.Repository == "https://github.com/retrom-project/retrom-runtime" &&
		provider.Tag == "v0.59.0" && provider.Commit == "3d42edb8994e2f6e39338ddfd226164dd2d953a5" &&
		provider.ProviderID == "retrom-runtime" && provider.TargetID == "mame-arcade"
}

func validMAMECore(core mameReleaseIdentity, manifest Manifest) bool {
	return core.Repository == "https://github.com/retrom-project/mame" &&
		core.Tag == "retrom-core-gf65d5ba9bc42-r2" &&
		core.Commit == "919816e409260a759a82f65b10792f6938001894" &&
		len(manifest.Cores) == 1 && manifest.Cores[0].CoreID == "mame_arcade" &&
		manifest.Cores[0].CoreSource.Commit == core.Commit && manifest.Cores[0].DAT != nil &&
		manifest.Cores[0].DAT.LocalPath == "mame-arcade.xml"
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
	catalogContents, err := os.ReadFile(filepath.Join(root, "runtime-target-bindings", "v1", "catalog.json"))
	if err != nil {
		return nil, fmt.Errorf("%w: runtime target catalog unavailable", ErrInvalid)
	}
	result.RuntimeCatalog, err = runtimecatalog.ParseCatalog(catalogContents)
	if err != nil {
		return nil, fmt.Errorf("%w: runtime target catalog", ErrInvalid)
	}
	return result, nil
}

func loadVersion(root, versionName string) (*Version, error) {
	datRoot := filepath.Join(root, "dat", "emulatorjs", versionName)
	manifest, digest, err := loadManifest(datRoot, versionName)
	if err != nil {
		return nil, err
	}
	version := &Version{Manifest: manifest, ManifestSHA256: digest, DATRoot: datRoot}
	if err := loadDATFiles(version); err != nil {
		return nil, err
	}
	return version, nil
}

func loadManifest(datRoot, versionName string) (Manifest, string, error) {
	contents, err := os.ReadFile(filepath.Join(datRoot, "manifest.json"))
	if err != nil {
		return Manifest{}, "", fmt.Errorf("%w: manifest unavailable", ErrInvalid)
	}
	var manifest Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil ||
		manifest.SchemaVersion != 8 || manifest.EmulatorJS.Version != versionName {
		return Manifest{}, "", fmt.Errorf("%w: manifest schema", ErrInvalid)
	}
	digest := sha256.Sum256(contents)
	return manifest, hex.EncodeToString(digest[:]), nil
}

func loadDATFiles(version *Version) error {
	seen := make(map[string]struct{}, len(version.Manifest.Cores))
	for _, core := range version.Manifest.Cores {
		if core.CoreID == "" {
			return fmt.Errorf("%w: DAT core identity", ErrInvalid)
		}
		if _, duplicate := seen[core.CoreID]; duplicate {
			return fmt.Errorf("%w: duplicate DAT core", ErrInvalid)
		}
		seen[core.CoreID] = struct{}{}
		if core.DAT == nil {
			continue
		}
		if err := checkFile(version.DATRoot, core.DAT.LocalPath, core.DAT.SizeBytes, core.DAT.SHA256); err != nil {
			return err
		}
	}
	return nil
}

func checkFile(root, relative string, expectedSize int64, expectedDigest string) error {
	if !safeRelative(relative) || len(expectedDigest) != 64 || expectedDigest != strings.ToLower(expectedDigest) {
		return fmt.Errorf("%w: file declaration", ErrInvalid)
	}
	file, err := os.Open(filepath.Join(root, filepath.FromSlash(relative)))
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
