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
	"retrom/internal/runtimecatalog"
)

var (
	ErrInvalid          = errors.New("DEPENDENCY_INVALID")
	errDATJobNotClaimed = errors.New("DEPENDENCY_DAT_JOB_NOT_CLAIMABLE")
	errDATParseFailed   = errors.New("DEPENDENCY_DAT_PARSE_FAILED")
	errBIOSOptions      = errors.New("DEPENDENCY_BIOS_ACTIVATION_OPTIONS_INVALID")
)

// Manifest is the third-party DAT provenance document. Runtime implementation
// fields in the source document are intentionally ignored: runtime selection
// and assets are owned exclusively by Provider declarations.
type Manifest struct {
	SchemaVersion int `json:"schema_version"`
	EmulatorJS    struct {
		Version string `json:"version"`
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
}

type Version struct {
	Manifest       Manifest
	ManifestSHA256 string
	DATRoot        string
}

type Set struct {
	Versions       map[string]*Version
	Order          []string
	Active         *Version
	RuntimeCatalog runtimecatalog.Catalog
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
		manifest.SchemaVersion != 7 || manifest.EmulatorJS.Version != versionName {
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
