package dependencies

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/arcadedat"
	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/datindex"
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

//nolint:gocyclo,gocognit // Manifest fields are independently validated against the signed dependency contract.
func loadVersion(root, versionName string) (*Version, error) {
	datRoot := filepath.Join(root, "dat", "emulatorjs", versionName)
	runtimeRoot := filepath.Join(root, "runtime", "emulatorjs", versionName)
	manifestPath := filepath.Join(datRoot, "manifest.json")
	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Version and manifest slot are strict allowlist values.
	if err != nil {
		return nil, fmt.Errorf("%w: manifest unavailable", ErrInvalid)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest schema", ErrInvalid)
	}
	if manifest.SchemaVersion != 4 && manifest.SchemaVersion != 5 || manifest.EmulatorJS.Version != versionName {
		return nil, fmt.Errorf("%w: manifest version", ErrInvalid)
	}
	manifestDigest := sha256.Sum256(contents)
	version := &Version{
		Manifest: manifest, ManifestSHA256: hex.EncodeToString(manifestDigest[:]),
		DATRoot: datRoot, RuntimeRoot: runtimeRoot, Allowlist: make(map[string]File),
	}
	for _, file := range manifest.EmulatorJS.RuntimeAllowlist {
		if !safeRelative(file.Path) {
			return nil, fmt.Errorf("%w: runtime path", ErrInvalid)
		}
		if err := checkFile(runtimeRoot, file.Path, file.SizeBytes, file.SHA256); err != nil {
			return nil, err
		}
		version.Allowlist[file.Path] = file
	}
	if err := validateManifestCapabilities(manifest, version.Allowlist); err != nil {
		return nil, err
	}
	for _, core := range manifest.EmulatorJS.SelectedCores {
		path := core.LocalPath
		if core.PathInRelease != nil {
			path = *core.PathInRelease
		}
		if err := checkFile(runtimeRoot, path, core.SizeBytes, core.SHA256); err != nil {
			return nil, err
		}
		version.Allowlist[path] = File{Path: path, SizeBytes: core.SizeBytes, SHA256: core.SHA256, Role: "core"}
	}
	for _, core := range manifest.Cores {
		if core.DAT == nil {
			continue
		}
		if err := checkFile(datRoot, core.DAT.LocalPath, core.DAT.SizeBytes, core.DAT.SHA256); err != nil {
			return nil, err
		}
	}
	for _, component := range manifest.Licenses.Components {
		for _, license := range component.LicenseFiles {
			if err := checkFile(runtimeRoot, license.OutputPath, license.SizeBytes, license.SHA256); err != nil {
				return nil, err
			}
		}
	}
	if manifest.Licenses.NoticePath == "" {
		return nil, fmt.Errorf("%w: notice path", ErrInvalid)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, manifest.Licenses.NoticePath)); err != nil {
		return nil, fmt.Errorf("%w: notice unavailable", ErrInvalid)
	}
	return version, nil
}

var runtimeCorePattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

//nolint:gocyclo,gocognit // Each branch enforces one manifest security invariant.
func validateManifestCapabilities(manifest Manifest, allowlist map[string]File) error {
	coreIDs := make(map[string]struct{}, len(manifest.EmulatorJS.SelectedCores))
	runtimeIDs := make(map[string]struct{}, len(manifest.EmulatorJS.SelectedCores))
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
	for index, core := range manifest.EmulatorJS.SelectedCores {
		if core.CoreID == "" || core.SourceComponentID != core.CoreID ||
			manifest.Licenses.NoticeOrder[index+1] != core.SourceComponentID {
			return fmt.Errorf("%w: core component", ErrInvalid)
		}
		if _, duplicate := coreIDs[core.CoreID]; duplicate {
			return fmt.Errorf("%w: duplicate core", ErrInvalid)
		}
		if !runtimeCorePattern.MatchString(core.RuntimeCoreID) {
			return fmt.Errorf("%w: runtime core", ErrInvalid)
		}
		if _, duplicate := runtimeIDs[core.RuntimeCoreID]; duplicate {
			return fmt.Errorf("%w: duplicate runtime core", ErrInvalid)
		}
		coreIDs[core.CoreID] = struct{}{}
		runtimeIDs[core.RuntimeCoreID] = struct{}{}
		if err := validateSelectedCore(core, allowlist, manifest.SchemaVersion); err != nil {
			return err
		}
	}
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

//nolint:gocyclo // Each branch enforces one independent core capability invariant.
func validateSelectedCore(core SelectedCore, allowlist map[string]File, manifestSchemaVersion int) error {
	path := core.LocalPath
	if core.PathInRelease != nil {
		path = *core.PathInRelease
	}
	threadBasename := strings.HasSuffix(core.RequestedArtifactBasename, "-thread-wasm.data")
	if !safeRelative(path) || filepath.Base(core.RequestedArtifactBasename) != core.RequestedArtifactBasename ||
		strings.Contains(core.RequestedArtifactBasename, "..") ||
		!strings.HasSuffix(core.RequestedArtifactBasename, "-wasm.data") || threadBasename != core.Threads {
		return fmt.Errorf("%w: artifact basename", ErrInvalid)
	}
	expectedFlavor := "WASM"
	if core.Threads {
		expectedFlavor = "THREAD_WASM"
	}
	if core.PathInRelease == nil {
		expectedFlavor = "OVERRIDE"
	}
	if core.BundleVersion == "" || core.ArtifactFlavor != expectedFlavor ||
		core.CanvasResizePolicy != "NONE" && core.CanvasResizePolicy != "ON_GAME_START_TO_CSS_PIXELS" {
		return fmt.Errorf("%w: artifact capability", ErrInvalid)
	}
	if len(core.DefaultOptions) > 32 {
		return fmt.Errorf("%w: artifact options", ErrInvalid)
	}
	for name, value := range core.DefaultOptions {
		if name == "__proto__" || name == "constructor" || name == "prototype" ||
			!validASCIIOption(name, 1) || !validASCIIOption(value, 0) {
			return fmt.Errorf("%w: artifact options", ErrInvalid)
		}
	}
	expectedKind := map[string]*string{
		"SINGLE_FILE": pointerTo("CORE_SAVE"), "DOS_OVERLAY": pointerTo("DOS_OVERLAY"),
		"FILE_TREE": pointerTo("CORE_SAVE"), "NONE": nil,
	}
	kind, exists := expectedKind[core.PersistentSaveMode]
	if !exists || !equalOptionalString(core.PersistentSaveKind, kind) ||
		core.PersistentSaveMode == "FILE_TREE" && core.RuntimeCoreID != "ppsspp" ||
		core.InputMode != "STANDARD" && core.InputMode != "POINTER" || len(core.StartupActions) > 4 {
		return fmt.Errorf("%w: artifact runtime capability", ErrInvalid)
	}
	for _, action := range core.StartupActions {
		if action.Event != "GAME_START" || action.Kind != "PRESS_CONTROL" || action.DelayMS < 0 || action.DelayMS > 30_000 ||
			action.Player < 0 || action.Player > 3 || action.Control < 0 || action.Control > 255 ||
			action.DurationMS < 1 || action.DurationMS > 1_000 {
			return fmt.Errorf("%w: startup action", ErrInvalid)
		}
	}
	if !safeRelative(core.ReportPath) {
		return fmt.Errorf("%w: core report", ErrInvalid)
	}
	if _, exists := allowlist[core.ReportPath]; !exists {
		return fmt.Errorf("%w: core report allowlist", ErrInvalid)
	}
	return validateContentCapabilities(core, manifestSchemaVersion)
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

func (set *Set) Bootstrap(ctx context.Context, database *sql.DB, now time.Time) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin dependency bootstrap: %w", err)
	}
	defer cleanup.Rollback(transaction)
	preferredVersions := make(map[string]string)
	for _, versionName := range set.Order {
		for _, core := range set.Versions[versionName].Manifest.EmulatorJS.SelectedCores {
			preferredVersions[core.CoreID] = versionName
		}
	}
	for _, versionName := range set.Order {
		version := set.Versions[versionName]
		licenseComponents := make(map[string]struct {
			Repository, SourceCommit, Association string
		}, len(version.Manifest.Licenses.Components))
		for _, component := range version.Manifest.Licenses.Components {
			licenseComponents[component.ComponentID] = struct {
				Repository, SourceCommit, Association string
			}{component.Repository, component.SourceCommit, component.BinaryAssociationStatus}
		}
		selectedCoreIDs := make(map[string]struct{}, len(version.Manifest.EmulatorJS.SelectedCores))
		for index, core := range version.Manifest.EmulatorJS.SelectedCores {
			selectedCoreIDs[core.CoreID] = struct{}{}
			if err := bootstrapCore(
				ctx,
				transaction,
				versionName,
				version,
				preferredVersions[core.CoreID] == versionName,
				index,
				core,
				licenseComponents[core.SourceComponentID],
				now,
			); err != nil {
				return err
			}
		}
		if err := bootstrapStaticBIOS(ctx, transaction, versionName, selectedCoreIDs, now); err != nil {
			return err
		}
		for _, core := range version.Manifest.Cores {
			if core.DAT == nil {
				continue
			}
			if err := bootstrapDAT(
				ctx,
				transaction,
				versionName,
				core.CoreID,
				core.DAT.LocalPath,
				core.DAT.SHA256,
				core.ParseStats.MachineCount,
				core.ParseStats.ROMEntryCount,
				core.ParseStats.DiskEntryCount,
				core.ParseStats.BIOSSetCount,
				core.ParseStats.DefaultBIOSSetCount,
				core.ParseStats.ExplicitBIOSMachineCount,
				core.ParseStats.BaseDependencyTargetCount,
				core.ParseStats.UnresolvedCloneofCount+core.ParseStats.UnresolvedRomofCount,
				preferredVersions[core.CoreID] == versionName,
				now,
			); err != nil {
				return err
			}
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit dependency bootstrap: %w", err)
	}
	return nil
}

// BootstrapCatalogs materializes the byte-verified built-in DAT indexes. It is
// separate from the lightweight seed operation so focused store tests can seed
// dictionaries without repeatedly parsing the large pinned production inputs.
//
//nolint:funlen,gocognit,gocyclo,nestif // Contract branches stay contiguous for a single auditable decision.
func (set *Set) BootstrapCatalogs(ctx context.Context, database *sql.DB, now time.Time) error {
	versionNames := make([]string, 0, len(set.Versions))
	for versionName := range set.Versions {
		versionNames = append(versionNames, versionName)
	}
	sort.Strings(versionNames)
	var firstFailure error
	for _, versionName := range versionNames {
		version := set.Versions[versionName]
		for _, core := range version.Manifest.Cores {
			if core.DAT == nil {
				continue
			}
			var datID string
			var indexed int64
			var parseStatus string
			err := database.QueryRowContext(ctx, `
SELECT d.id,
d.parse_status,
(SELECT count(*)
FROM dat_machines m
WHERE m.dat_version_id=d.id)
FROM dat_versions d
JOIN core_artifacts a ON a.id=d.core_artifact_id
WHERE d.source='BUILTIN'
AND d.sha256=?
AND d.parser_version='retrom-dat-v1'
AND a.core_id=?
AND a.emulatorjs_version=?
`, core.DAT.SHA256, core.CoreID, versionName).
				Scan(&datID, &parseStatus, &indexed)
			if err != nil {
				if firstFailure == nil {
					firstFailure = fmt.Errorf("find built-in DAT index: %w", err)
				}
				continue
			}
			if parseStatus == "READY" && indexed == core.ParseStats.MachineCount {
				transaction, activateErr := database.BeginTx(ctx, nil)
				if activateErr == nil {
					activateErr = activateBuiltInDAT(ctx, transaction, datID, now)
				}
				if activateErr == nil {
					activateErr = transaction.Commit()
				} else if transaction != nil {
					cleanup.Rollback(transaction)
				}
				if activateErr != nil && firstFailure == nil {
					firstFailure = fmt.Errorf("activate ready built-in DAT: %w", activateErr)
				}
				continue
			}
			if parseStatus == "FAILED" {
				if firstFailure == nil {
					firstFailure = fmt.Errorf("%w: built-in DAT has retained failure evidence", ErrInvalid)
				}
				continue
			}
			jobID, jobErr := ensureBuiltInDATJob(ctx, database, datID, core.DAT.SHA256, "retrom-dat-v1", now)
			if jobErr != nil {
				if firstFailure == nil {
					firstFailure = jobErr
				}
				continue
			}
			startedAtMS := now.UnixMilli()
			claimed, claimErr := database.ExecContext(
				ctx,
				`
UPDATE jobs
SET state='RUNNING',
attempt_count=attempt_count+1,
execution_started_at_ms=COALESCE(execution_started_at_ms,
?),
execution_deadline_at_ms=COALESCE(execution_deadline_at_ms,
?),
leased_until_ms=?,
heartbeat_at_ms=?,
worker_id='builtin-dat-indexer',
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='QUEUED'
`,
				startedAtMS,
				startedAtMS+int64(30*time.Minute/time.Millisecond),
				startedAtMS+60_000,
				startedAtMS,
				startedAtMS,
				jobID,
			)
			if claimErr != nil {
				if firstFailure == nil {
					firstFailure = claimErr
				}
				continue
			}
			if changed, _ := claimed.RowsAffected(); changed != 1 {
				if firstFailure == nil {
					firstFailure = errDATJobNotClaimed
				}
				continue
			}
			_, _ = database.ExecContext(
				ctx,
				`
UPDATE dat_versions
SET parse_status='PARSING',
version=version+1,
updated_at_ms=?
WHERE id=?
AND parse_status IN ('PENDING',
'PARSING')
`,
				startedAtMS,
				datID,
			)
			_, _ = database.ExecContext(
				ctx,
				`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'STARTED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
1),
?)
`,
				jobID,
				datID,
				startedAtMS,
			)
			var catalog arcadedat.Catalog
			if indexed == core.ParseStats.MachineCount {
				catalog.Stats.MachineCount = int(core.ParseStats.MachineCount)
				catalog.Stats.ROMEntryCount = int(core.ParseStats.ROMEntryCount)
				catalog.Stats.DiskEntryCount = int(core.ParseStats.DiskEntryCount)
				catalog.Stats.BIOSSetCount = int(core.ParseStats.BIOSSetCount)
				catalog.Stats.DefaultBIOSSetCount = int(core.ParseStats.DefaultBIOSSetCount)
				catalog.Stats.ExplicitBIOSMachineCount = int(core.ParseStats.ExplicitBIOSMachineCount)
				catalog.Stats.BaseDependencyTargetCount = int(core.ParseStats.BaseDependencyTargetCount)
				catalog.Stats.UnresolvedCloneofTargetCount = int(core.ParseStats.UnresolvedCloneofCount)
				catalog.Stats.UnresolvedRomofTargetCount = int(core.ParseStats.UnresolvedRomofCount)
			} else {
				file, err := os.Open(filepath.Join(version.DATRoot, filepath.FromSlash(core.DAT.LocalPath)))
				if err != nil {
					failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_BLOB_UNAVAILABLE", now)
					if firstFailure == nil {
						firstFailure = fmt.Errorf("open built-in DAT: %w", err)
					}
					continue
				}
				var parseErr error
				catalog, parseErr = arcadedat.ParseCatalog(ctx, file, core.CoreID)
				cleanup.Error("close", file.Close())
				if parseErr != nil || !statsMatch(catalog.Stats, core.ParseStats) {
					failureCode := "DEPENDENCY_DAT_STATISTICS_MISMATCH"
					if parseErr != nil {
						failureCode = "DEPENDENCY_DAT_PARSE_FAILED"
					}
					failBuiltInDAT(ctx, database, datID, jobID, failureCode, now)
					if firstFailure == nil {
						if parseErr != nil {
							firstFailure = fmt.Errorf("parse built-in DAT: %w", parseErr)
						} else {
							firstFailure = fmt.Errorf("%w: built-in DAT statistics mismatch", ErrInvalid)
						}
					}
					continue
				}
			}
			transaction, err := database.BeginTx(ctx, nil)
			if err != nil {
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			if indexed != core.ParseStats.MachineCount {
				if err := datindex.Replace(ctx, transaction, datID, catalog); err != nil {
					cleanup.Rollback(transaction)
					failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
					if firstFailure == nil {
						firstFailure = fmt.Errorf("write built-in DAT index: %w", err)
					}
					continue
				}
			}
			stats := catalog.Stats
			finishedAtMS := now.UnixMilli()
			var artifactID string
			if err := transaction.QueryRowContext(ctx, `
SELECT core_artifact_id
FROM dat_versions
WHERE id=?
`, datID).Scan(&artifactID); err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			_, err = transaction.ExecContext(
				ctx,
				`
UPDATE dat_versions
SET parse_status='READY',
is_active=0,
machine_count=?,
rom_entry_count=?,
disk_entry_count=?,
bios_set_count=?,
default_bios_set_count=?,
explicit_bios_machine_count=?,
base_dependency_target_count=?,
unresolved_relation_count=?,
parsed_at_ms=?,
updated_at_ms=?,
version=version+1
WHERE id=?
`,
				stats.MachineCount,
				stats.ROMEntryCount,
				stats.DiskEntryCount,
				stats.BIOSSetCount,
				stats.DefaultBIOSSetCount,
				stats.ExplicitBIOSMachineCount,
				stats.BaseDependencyTargetCount,
				stats.UnresolvedCloneofTargetCount+stats.UnresolvedRomofTargetCount,
				finishedAtMS,
				finishedAtMS,
				datID,
			)
			if err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = fmt.Errorf("publish built-in DAT index: %w", err)
				}
				continue
			}
			if err := activateBuiltInDAT(ctx, transaction, datID, now); err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = fmt.Errorf("publish built-in DAT requirements: %w", err)
				}
				continue
			}
			if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='SUCCEEDED',
finished_at_ms=?,
leased_until_ms=NULL,
heartbeat_at_ms=?,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
`, finishedAtMS, finishedAtMS, finishedAtMS, jobID); err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'SUCCEEDED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
1,
'machineCount',
?),
?)
`, jobID, datID, stats.MachineCount, finishedAtMS); err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			if err := transaction.Commit(); err != nil {
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = fmt.Errorf("commit built-in DAT index: %w", err)
				}
			}
		}
	}
	return firstFailure
}

func activateBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	datID string,
	now time.Time,
) error {
	var artifactID, source, parseStatus string
	var artifactEnabled, alreadyActive int
	if err := transaction.QueryRowContext(ctx, `
SELECT d.core_artifact_id,a.enabled,d.source,d.parse_status,d.is_active
FROM dat_versions d
JOIN core_artifacts a ON a.id=d.core_artifact_id
WHERE d.id=?
`, datID).Scan(&artifactID, &artifactEnabled, &source, &parseStatus, &alreadyActive); err != nil {
		return fmt.Errorf("inspect selected built-in DAT: %w", err)
	}
	if artifactEnabled == 0 {
		return nil
	}
	if source != "BUILTIN" || parseStatus != "READY" {
		return fmt.Errorf("%w: selected DAT is not a ready built-in version", ErrInvalid)
	}
	if alreadyActive == 1 {
		return nil
	}
	deactivated, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE core_artifact_id=? AND source='BUILTIN' AND is_active=1 AND id<>?
`, now.UnixMilli(), artifactID, datID)
	if err != nil {
		return fmt.Errorf("deactivate superseded built-in DAT: %w", err)
	}
	if changed, _ := deactivated.RowsAffected(); changed > 0 {
		if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, now.UnixMilli(), artifactID); err != nil {
			return fmt.Errorf("advance artifact DAT selection: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=1,activated_at_ms=?,version=version+1,updated_at_ms=?
WHERE id=? AND source='BUILTIN' AND parse_status='READY' AND is_active=0
`, now.UnixMilli(), now.UnixMilli(), datID); err != nil {
		return fmt.Errorf("activate selected built-in DAT: %w", err)
	}
	if err := datindex.SyncRequirements(ctx, transaction, datID, now); err != nil {
		return fmt.Errorf("sync selected built-in DAT requirements: %w", err)
	}
	auditID, _ := uuid.NewV7()
	actor := authn.ActorFromContext(ctx, "release-setup")
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,created_at_ms)
VALUES(?,?,?,?,'BUILTIN_DAT_ACTIVATED','DAT_VERSION',?,
'{"active":false}','{"active":true}',json_object('source','release-manifest'),?)
`, auditID.String(), actor.Kind, actor.UserID, actor.Label, datID, now.UnixMilli()); err != nil {
		return fmt.Errorf("audit selected built-in DAT activation: %w", err)
	}
	return nil
}

//nolint:funlen,nestif // Contract branches stay contiguous for a single auditable decision.
func ensureBuiltInDATJob(
	ctx context.Context,
	database *sql.DB,
	datID, datSHA, parserVersion string,
	now time.Time,
) (string, error) {
	canonical, _ := json.Marshal(map[string]string{"datVersionId": datID, "parserVersion": parserVersion})
	digest := sha256.New()
	_, _ = digest.Write([]byte("retrom-job-dedupe-v1\x00DAT_PARSE\x00"))
	_, _ = digest.Write(canonical)
	dedupeKey := hex.EncodeToString(digest.Sum(nil))
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var jobID, state string
	err = transaction.QueryRowContext(ctx, `
SELECT id,
state
FROM jobs
WHERE kind='DAT_PARSE'
AND dedupe_key=?
`, dedupeKey).
		Scan(&jobID, &state)
	if err == nil {
		if state == "FAILED" || state == "CANCELLED" {
			return "", errDATParseFailed
		}
		if state == "RUNNING" {
			if _, err := transaction.ExecContext(ctx, `
UPDATE jobs
SET state='QUEUED',
execution_started_at_ms=NULL,
execution_deadline_at_ms=NULL,
leased_until_ms=NULL,
heartbeat_at_ms=NULL,
worker_id=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now.UnixMilli(), jobID); err != nil {
				return "", fmt.Errorf("dependencies/dependencies: %w", err)
			}
		}
		if err := transaction.Commit(); err != nil {
			return "", fmt.Errorf("dependencies/dependencies: %w", err)
		}
		return jobID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	generated, _ := uuid.NewV7()
	executionID, _ := uuid.NewV7()
	jobID = generated.String()
	var datVersion int64
	var baseID sql.NullString
	if err := transaction.QueryRowContext(ctx, `
SELECT version,
(SELECT id
FROM dat_versions active
WHERE active.core_artifact_id=target.core_artifact_id
AND active.is_active=1)
FROM dat_versions target
WHERE target.id=?
`, datID).Scan(&datVersion, &baseID); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	input := map[string]any{
		"schemaVersion": 1,
		"kind":          "DAT_PARSE",
		"scope":         map[string]any{"type": "DAT_VERSION", "id": datID},
		"executionId":   executionID.String(),
		"inputs": map[string]any{
			"datVersion":       datVersion,
			"datSha256":        datSHA,
			"parserVersion":    parserVersion,
			"baseDatVersionId": nullableString(baseID),
		},
	}
	inputJSON, _ := json.Marshal(input)
	inputDigest := sha256.Sum256(inputJSON)
	payload := `{"schemaVersion":1,"inputExecutionNo":1}`
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO jobs(id,
scope_type,
scope_id,
kind,
dedupe_key,
execution_no,
payload_json,
cancellable,
state,
attempt_count,
max_attempts,
available_at_ms,
created_at_ms,
updated_at_ms) VALUES(?,
'DAT_VERSION',
?,
'DAT_PARSE',
?,
1,
?,
0,
'QUEUED',
0,
2,
?,
?,
?)
`, jobID, datID, dedupeKey, payload, now.UnixMilli(), now.UnixMilli(), now.UnixMilli()); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_input_snapshots(job_id,
execution_no,
input_json,
input_digest,
created_at_ms) VALUES(?,
1,
?,
?,
?)
`, jobID, string(inputJSON), hex.EncodeToString(inputDigest[:]), now.UnixMilli()); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'QUEUED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
0),
?)
`, jobID, datID, now.UnixMilli()); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return "", fmt.Errorf("dependencies/dependencies: %w", err)
	}
	return jobID, nil
}

func failBuiltInDAT(ctx context.Context, database *sql.DB, datID, jobID, code string, now time.Time) {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer cleanup.Rollback(transaction)
	_, _ = transaction.ExecContext(
		ctx,
		`
UPDATE dat_versions
SET parse_status='FAILED',
is_active=0,
machine_count=NULL,
rom_entry_count=NULL,
disk_entry_count=NULL,
bios_set_count=NULL,
default_bios_set_count=NULL,
explicit_bios_machine_count=NULL,
base_dependency_target_count=NULL,
unresolved_relation_count=NULL,
parsed_at_ms=NULL,
updated_at_ms=?,
version=version+1
WHERE id=?
`,
		now.UnixMilli(),
		datID,
	)
	_, _ = transaction.ExecContext(
		ctx,
		`
UPDATE jobs
SET state='FAILED',
error_code=?,
error_retryable=0,
finished_at_ms=?,
leased_until_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
AND state='RUNNING'
`,
		code,
		now.UnixMilli(),
		now.UnixMilli(),
		jobID,
	)
	_, _ = transaction.ExecContext(
		ctx,
		`
INSERT INTO job_events(job_id,
scope_type,
scope_id,
event_type,
data_json,
created_at_ms) VALUES(?,
'DAT_VERSION',
?,
'FAILED',
json_object('schemaVersion',
1,
'executionNo',
1,
'attempt',
1,
'errorCode',
?,
'errorRetryable',
false),
?)
`,
		jobID,
		datID,
		code,
		now.UnixMilli(),
	)
	_ = transaction.Commit()
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}

func statsMatch(actual arcadedat.Stats, expected struct {
	MachineCount              int64 `json:"machine_count"`
	ROMEntryCount             int64 `json:"rom_entry_count"`
	DiskEntryCount            int64 `json:"disk_entry_count"`
	BIOSSetCount              int64 `json:"bios_set_count"`
	DefaultBIOSSetCount       int64 `json:"default_bios_set_count"`
	ExplicitBIOSMachineCount  int64 `json:"explicit_bios_machine_count"`
	BaseDependencyTargetCount int64 `json:"base_dependency_target_count"`
	UnresolvedCloneofCount    int64 `json:"unresolved_cloneof_target_count"`
	UnresolvedRomofCount      int64 `json:"unresolved_romof_target_count"`
},
) bool {
	return int64(actual.MachineCount) == expected.MachineCount && int64(actual.ROMEntryCount) == expected.ROMEntryCount &&
		int64(actual.DiskEntryCount) == expected.DiskEntryCount &&
		int64(actual.BIOSSetCount) == expected.BIOSSetCount &&
		int64(actual.DefaultBIOSSetCount) == expected.DefaultBIOSSetCount &&
		int64(actual.ExplicitBIOSMachineCount) == expected.ExplicitBIOSMachineCount &&
		int64(actual.BaseDependencyTargetCount) == expected.BaseDependencyTargetCount &&
		int64(actual.UnresolvedCloneofTargetCount) == expected.UnresolvedCloneofCount &&
		int64(actual.UnresolvedRomofTargetCount) == expected.UnresolvedRomofCount
}

type staticBIOS struct {
	coreID       string
	logical      string
	mode         string
	condition    string
	size         int64
	md5          string
	sha256       string
	options      string
	sourceURL    string
	delivery     string
	emulatorPath string
}

var staticBIOSCatalog = []staticBIOS{
	{
		coreID:    "fceumm",
		logical:   "disksys.rom",
		mode:      "CONDITIONAL",
		condition: "FDS_CONTENT",
		size:      8192,
		md5:       "ca30b50f880eb660a320674ed365ef7a",
		sha256:    "99c18490ed9002d9c6d999b9d8d15be5c051bdfa7cc7e73318053c9a994b0178",
		sourceURL: "https://docs.libretro.com/library/fceumm/",
	},
	{
		coreID:    "fceumm",
		logical:   "gamegenie.nes",
		mode:      "CONDITIONAL",
		condition: "GAME_GENIE_ADDON_MODE",
		md5:       "7f98d77d7a094ad7d069b74bd553ec98",
		sourceURL: "https://docs.libretro.com/library/fceumm/",
	},
	{
		coreID:    "snes9x",
		logical:   "BS-X.bin",
		mode:      "OPTIONAL",
		condition: "SNES_BSX_FIRMWARE",
		size:      1048576,
		md5:       "fed4d8242cfbed61343d53d48432aced",
		sha256:    "3ce321496edc5d77038de2034eb3fb354d7724afd0bc7fd0319f3eb5d57b984d",
		sourceURL: "https://docs.libretro.com/library/snes9x/",
	},
	{
		coreID:    "snes9x",
		logical:   "STBIOS.bin",
		mode:      "OPTIONAL",
		condition: "SNES_SUFAMI_FIRMWARE",
		size:      262144,
		md5:       "d3a44ba7d42a74d3ac58cb9c14c6a5ca",
		sha256:    "edacb453da14f825f05d1134d6035f4bf034e55f7cfb97c70c4ee107eabc7342",
		sourceURL: "https://docs.libretro.com/library/snes9x/",
	},
	{
		coreID:    "gambatte",
		logical:   "gb_bios.bin",
		mode:      "OPTIONAL",
		condition: "GB_CONTENT",
		size:      256,
		md5:       "32fbbd84168d3482956eb3c5051637f5",
		sha256:    "cf053eccb4ccafff9e67339d4e78e98dce7d1ed59be819d2a1ba2232c6fce1c7",
		options:   `{"gambatte_gb_bootloader":"enabled"}`,
		sourceURL: "https://docs.libretro.com/library/gambatte/",
	},
	{
		coreID:    "gambatte",
		logical:   "gbc_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBC_CONTENT",
		size:      2304,
		md5:       "dbfce9db9deaa2567f6a84fde55f9680",
		sha256:    "b4f2e416a35eef52cba161b159c7c8523a92594facb924b3ede0d722867c50c7",
		options:   `{"gambatte_gb_bootloader":"enabled"}`,
		sourceURL: "https://docs.libretro.com/library/gambatte/",
	},
	{
		coreID:    "mgba",
		logical:   "gba_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBA_CONTENT",
		size:      16384,
		md5:       "a860e8c0b6d573d191e4ec7db1b1e4f6",
		sha256:    "fd2547724b505f487e6dcb29ec2ecff3af35a841a77ab2e85fd87350abd36570",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "gb_bios.bin",
		mode:      "OPTIONAL",
		condition: "GB_CONTENT",
		size:      256,
		md5:       "32fbbd84168d3482956eb3c5051637f5",
		sha256:    "cf053eccb4ccafff9e67339d4e78e98dce7d1ed59be819d2a1ba2232c6fce1c7",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "gbc_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBC_CONTENT",
		size:      2304,
		md5:       "dbfce9db9deaa2567f6a84fde55f9680",
		sha256:    "b4f2e416a35eef52cba161b159c7c8523a92594facb924b3ede0d722867c50c7",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "sgb_bios.bin",
		mode:      "OPTIONAL",
		condition: "MGBA_SGB_MODEL",
		size:      256,
		md5:       "d574d4f9c12f305074798f54c091a8b4",
		sha256:    "0e4ddff32fc9d1eeaae812a157dd246459b00c9e14f2f61751f661f32361e360",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID: "nestopia", logical: "disksys.rom", mode: "CONDITIONAL", condition: "FDS_CONTENT", size: 8192,
		md5: "ca30b50f880eb660a320674ed365ef7a", sha256: "99c18490ed9002d9c6d999b9d8d15be5c051bdfa7cc7e73318053c9a994b0178",
		sourceURL: "https://docs.libretro.com/library/nestopia_ue/",
	},
	{
		coreID: "melonds", logical: "bios7.bin", mode: "REQUIRED", size: 16384,
		md5: "df692a80a5b1bc90728bc3dfc76cd948", sha256: "ba65f690eb04ec92db67c0e299e21ad71de087d6d5de8a9cb17a62eaab563c17",
		sourceURL:    "https://docs.libretro.com/library/melonds/",
		delivery:     "EXTERNAL_FILE",
		emulatorPath: "/retroarch/userdata/system/bios7.bin",
	},
	{
		coreID: "melonds", logical: "bios9.bin", mode: "REQUIRED", size: 4096,
		md5: "a392174eb3e572fed6447e956bde4b25", sha256: "1693983a7707ae394786fa526c0552457888a51d4e410d715ef07acd5a540555",
		sourceURL:    "https://docs.libretro.com/library/melonds/",
		delivery:     "EXTERNAL_FILE",
		emulatorPath: "/retroarch/userdata/system/bios9.bin",
	},
	{
		coreID: "melonds", logical: "firmware.bin", mode: "REQUIRED", size: 262144,
		md5: "6de7f8d5bdf66f6f5583fac51fcc5a07", sha256: "7d0e3e7f9ae2d9eda596d889ed8ce6d517da227460c120c0ab8d54432246380d",
		sourceURL:    "https://docs.libretro.com/library/melonds/",
		delivery:     "EXTERNAL_FILE",
		emulatorPath: "/retroarch/userdata/system/firmware.bin",
	},
	{
		coreID: "a5200", logical: "5200.rom", mode: "REQUIRED", size: 2048,
		md5: "281f20ea4320404ec820fb7ec0693b38", sha256: "06b250f18983d058c0f156ce7ee88ae48b6eaf11e6f10f21dccf6ac7ffb6a6af",
		sourceURL: "https://docs.libretro.com/library/atari800/",
	},
	{
		coreID: "pcsx_rearmed", logical: "scph5500.bin", mode: "REQUIRED", size: 524288,
		md5: "8dd7d5296a650fac7319bce665a6a53c", sha256: "9c0421858e217805f4abe18698afea8d5aa36ff0727eb8484944e00eb5e7eadb",
		sourceURL: "https://docs.libretro.com/library/pcsx_rearmed/",
	},
	{
		coreID: "mednafen_psx_hw", logical: "scph5500.bin", mode: "REQUIRED", size: 524288,
		md5: "8dd7d5296a650fac7319bce665a6a53c", sha256: "9c0421858e217805f4abe18698afea8d5aa36ff0727eb8484944e00eb5e7eadb",
		sourceURL: "https://docs.libretro.com/library/beetle_psx_hw/",
	},
	{
		coreID: "handy", logical: "lynxboot.img", mode: "REQUIRED", size: 512,
		md5: "fcd403db69f54290b51035d82f835e7b", sha256: "c26a36c1990bcf841155e5a6fea4d2ee1a4d53b3cc772e70f257a962ad43b383",
		sourceURL: "https://docs.libretro.com/library/handy/",
	},
	{
		coreID: "yabause", logical: "saturn_bios.bin", mode: "REQUIRED", size: 524288,
		md5: "af5828fdff51384f99b3c4926be27762", sha256: "ae4058627bb5db9be6d8d83c6be95a4aa981acc8a89042e517e73317886c8bc2",
		sourceURL: "https://docs.libretro.com/library/yabause/",
	},
	{
		coreID: "opera", logical: "panafz10.bin", mode: "REQUIRED", size: 1048576,
		md5: "51f2f43ae2f3508a14d9f56597e2d3ce", sha256: "8d72334395cfc98e44c89804eabf036cf95a23645353e7fe8ab886445a3b6354",
		sourceURL: "https://docs.libretro.com/library/opera/",
	},
	{
		coreID: "prosystem", logical: "7800 BIOS (U).rom", mode: "REQUIRED", size: 4096,
		md5: "0763f1ffb006ddbe32e52d497ee848ae", sha256: "7d94551defcd8e7b045a34255654d6d169a683f63062d51dee3eedabf2042db0",
		sourceURL: "https://docs.libretro.com/library/prosystem/",
	},
	{
		coreID: "mednafen_pcfx", logical: "pcfx.rom", mode: "REQUIRED", size: 1048576,
		md5: "08e36edbea28a017f79f8d4f7ff9b6d7", sha256: "4b44ccf5d84cc83daa2e6a2bee00fdafa14eb58bdf5859e96d8861a891675417",
		sourceURL: "https://docs.libretro.com/library/beetle_pc_fx/",
	},
}

//nolint:funlen // Static BIOS definitions are synchronized atomically with their aliases and version provenance.
func bootstrapStaticBIOS(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	selectedCoreIDs map[string]struct{},
	now time.Time,
) error {
	if err := validateBIOSActivationOptions(staticBIOSCatalog); err != nil {
		return err
	}
	for _, requirement := range staticBIOSCatalog {
		if _, selected := selectedCoreIDs[requirement.coreID]; !selected {
			continue
		}
		delivery := requirement.delivery
		if delivery == "" {
			delivery = "BIOS_BUNDLE"
		}
		var artifactID string
		if err := transaction.QueryRowContext(ctx, `
SELECT id
FROM core_artifacts
WHERE core_id=?
AND emulatorjs_version=?
`, requirement.coreID, versionName).Scan(&artifactID); err != nil {
			return fmt.Errorf("find BIOS core artifact: %w", err)
		}
		canonical, _ := json.Marshal(
			map[string]any{
				"activationOptions": json.RawMessage(nullableJSON(requirement.options)),
				"conditionCode":     requirement.condition,
				"deliveryKind":      delivery,
				"emulatorPath":      nullableStringValue(requirement.emulatorPath),
				"logicalName":       requirement.logical,
				"md5":               requirement.md5,
				"mode":              requirement.mode,
				"sha256":            nullableStringValue(requirement.sha256),
				"sizeBytes":         nullablePositive(requirement.size),
			},
		)
		digest := sha256.Sum256(canonical)
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("retrom:bios:"+artifactID+":"+requirement.logical)).String()
		_, err := transaction.ExecContext(
			ctx,
			`
INSERT INTO bios_requirements(id,
core_id,
core_artifact_id,
source_kind,
dat_machine_name,
logical_name,
requirement_mode,
condition_code,
activation_options_json,
catalog_digest,
size_bytes,
md5,
sha1,
sha256,
source_url,
source_version,
enabled,
version,
created_at_ms,
updated_at_ms,
delivery_kind,
emulator_path) VALUES(?,
?,
?,
'STATIC',
NULL,
?,
?,
?,
?,
?,
?,
?,
NULL,
?,
?,
?,
1,
1,
?,
?,
?,
?) ON CONFLICT(core_artifact_id,
logical_name)
DO UPDATE SET requirement_mode=excluded.requirement_mode,
condition_code=excluded.condition_code,
activation_options_json=excluded.activation_options_json,
catalog_digest=excluded.catalog_digest,
size_bytes=excluded.size_bytes,
md5=excluded.md5,
sha256=excluded.sha256,
source_url=excluded.source_url,
source_version=excluded.source_version,
delivery_kind=excluded.delivery_kind,
emulator_path=excluded.emulator_path,
enabled=1,
version=CASE WHEN bios_requirements.catalog_digest!=excluded.catalog_digest
  THEN bios_requirements.version+1 ELSE bios_requirements.version END,
updated_at_ms=excluded.updated_at_ms
`,
			id,
			requirement.coreID,
			artifactID,
			requirement.logical,
			requirement.mode,
			requirement.condition,
			nullableOptions(requirement.options),
			hex.EncodeToString(digest[:]),
			nullablePositive(requirement.size),
			requirement.md5,
			nullableStringValue(requirement.sha256),
			requirement.sourceURL,
			versionName,
			now.UnixMilli(),
			now.UnixMilli(),
			delivery,
			nullableStringValue(requirement.emulatorPath),
		)
		if err != nil {
			return fmt.Errorf("seed BIOS requirement: %w", err)
		}
	}
	return nil
}

//nolint:gocognit,gocyclo // Delivery, option syntax, and cross-requirement conflicts are independent invariants.
func validateBIOSActivationOptions(catalog []staticBIOS) error {
	byCore := make(map[string]map[string]string)
	for _, requirement := range catalog {
		delivery := requirement.delivery
		if delivery == "" {
			delivery = "BIOS_BUNDLE"
		}
		if delivery == "BIOS_BUNDLE" && requirement.emulatorPath != "" ||
			delivery == "EXTERNAL_FILE" && !validEmulatorPath(requirement.emulatorPath) ||
			delivery != "BIOS_BUNDLE" && delivery != "EXTERNAL_FILE" ||
			requirement.size < 0 || requirement.sha256 != "" && len(requirement.sha256) != 64 {
			return fmt.Errorf("%w: %s/%s delivery", errBIOSOptions, requirement.coreID, requirement.logical)
		}
		if requirement.options == "" {
			continue
		}
		var options map[string]string
		if err := json.Unmarshal([]byte(requirement.options), &options); err != nil || len(options) > 8 {
			return fmt.Errorf("%w: %s/%s", errBIOSOptions, requirement.coreID, requirement.logical)
		}
		if byCore[requirement.coreID] == nil {
			byCore[requirement.coreID] = make(map[string]string)
		}
		for name, value := range options {
			if !validASCIIOption(name, 1) || !validASCIIOption(value, 0) {
				return fmt.Errorf("%w: %s/%s", errBIOSOptions, requirement.coreID, requirement.logical)
			}
			if existing, ok := byCore[requirement.coreID][name]; ok && existing != value {
				return fmt.Errorf("%w: %s/%s", errBIOSOptions, requirement.coreID, name)
			}
			byCore[requirement.coreID][name] = value
		}
	}
	return nil
}

func validEmulatorPath(value string) bool {
	if len(value) < 1 || len(value) > 512 || value[0] != '/' || strings.ContainsAny(value, "\\?#\x00") ||
		strings.Contains(value, "//") || strings.HasSuffix(value, "/") {
		return false
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func nullablePositive(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableStringValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func validASCIIOption(value string, minimum int) bool {
	if len(value) < minimum || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

func nullableJSON(value string) string {
	if value == "" {
		return "null"
	}
	return value
}

func nullableOptions(value string) any {
	if value == "" {
		return nil
	}
	return value
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func bootstrapDAT(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	coreID string,
	relativePath string,
	digest string,
	machineCount int64,
	romCount int64,
	diskCount int64,
	biosSetCount int64,
	defaultBIOSCount int64,
	explicitBIOSCount int64,
	baseTargetCount int64,
	unresolvedCount int64,
	activeVersion bool,
	now time.Time,
) error {
	var artifactID string
	if err := transaction.QueryRowContext(ctx,
		"SELECT id FROM core_artifacts WHERE core_id = ? AND emulatorjs_version = ? AND enabled = ?",
		coreID, versionName, boolToInteger(activeVersion)).Scan(&artifactID); err != nil {
		return fmt.Errorf("find DAT core artifact: %w", err)
	}
	var id string
	err := transaction.QueryRowContext(
		ctx,
		`SELECT id FROM dat_versions
WHERE core_artifact_id = ? AND sha256 = ? AND parser_version = 'retrom-dat-v1' AND source = 'BUILTIN'`,
		artifactID,
		digest,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		generated, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			return fmt.Errorf("generate DAT version id: %w", uuidErr)
		}
		id = generated.String()
	} else if err != nil {
		return fmt.Errorf("find DAT version: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO dat_versions(id,
 core_id,
 core_artifact_id,
 source,
 builtin_relative_path,
 blob_id,
 sha256,
 parser_version,
 compatibility_status,
 parse_status,
 is_active,
 machine_count,
 rom_entry_count,
 disk_entry_count,
 bios_set_count,
 default_bios_set_count,
 explicit_bios_machine_count,
 base_dependency_target_count,
 unresolved_relation_count,
 version,
 created_at_ms,
 updated_at_ms,
 parsed_at_ms,
 activated_at_ms)
VALUES(?,
?,
?,
'BUILTIN',
?,
NULL,
?,
'retrom-dat-v1',
'MATCHED',
'PENDING',
0,
NULL,
NULL,
NULL,
NULL,
NULL,
NULL,
NULL,
NULL,
1,
?,
?,
NULL,
NULL)
ON CONFLICT(core_artifact_id,
 sha256,
 parser_version)
WHERE source = 'BUILTIN' DO UPDATE SET
  builtin_relative_path=excluded.builtin_relative_path,
compatibility_status='MATCHED',
updated_at_ms=excluded.updated_at_ms
`,
		id, coreID, artifactID, relativePath, digest, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return fmt.Errorf("upsert builtin DAT version: %w", err)
	}
	var parseStatus string
	var selectedActive int
	var indexedMachineCount, indexedROMCount, indexedDiskCount, indexedBIOSCount int64
	var indexedDefaultBIOSCount, indexedExplicitBIOSCount, indexedBaseTargetCount, indexedUnresolvedCount int64
	if err := transaction.QueryRowContext(ctx, `
SELECT d.parse_status,
d.is_active,
COALESCE(d.machine_count,
-1),
COALESCE(d.rom_entry_count,
-1),
COALESCE(d.disk_entry_count,
-1),
COALESCE(d.bios_set_count,
-1),
COALESCE(d.default_bios_set_count,
-1),
COALESCE(d.explicit_bios_machine_count,
-1),
COALESCE(d.base_dependency_target_count,
-1),
COALESCE(d.unresolved_relation_count,
-1)
FROM dat_versions d
WHERE d.id=?
`, id).Scan(
		&parseStatus, &selectedActive, &indexedMachineCount, &indexedROMCount, &indexedDiskCount, &indexedBIOSCount,
		&indexedDefaultBIOSCount, &indexedExplicitBIOSCount, &indexedBaseTargetCount, &indexedUnresolvedCount,
	); err != nil {
		return fmt.Errorf("inspect built-in DAT index: %w", err)
	}
	statsMatch := indexedMachineCount == machineCount && indexedROMCount == romCount && indexedDiskCount == diskCount &&
		indexedBIOSCount == biosSetCount && indexedDefaultBIOSCount == defaultBIOSCount &&
		indexedExplicitBIOSCount == explicitBIOSCount && indexedBaseTargetCount == baseTargetCount &&
		indexedUnresolvedCount == unresolvedCount
	if parseStatus == "READY" && !statsMatch {
		if err := repairBuiltInDATIndex(ctx, transaction, id, artifactID, selectedActive == 1, now); err != nil {
			return err
		}
	}
	if !activeVersion {
		return nil
	}
	return retireSupersededBuiltInDAT(ctx, transaction, artifactID, id, now)
}

func repairBuiltInDATIndex(
	ctx context.Context,
	transaction *sql.Tx,
	datID, artifactID string,
	wasActive bool,
	now time.Time,
) error {
	if _, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='PENDING',
is_active=0,
machine_count=NULL,
rom_entry_count=NULL,
disk_entry_count=NULL,
bios_set_count=NULL,
default_bios_set_count=NULL,
explicit_bios_machine_count=NULL,
base_dependency_target_count=NULL,
unresolved_relation_count=NULL,
parsed_at_ms=NULL,
activated_at_ms=NULL,
version=version+1,
updated_at_ms=?
WHERE id=?
`, now.UnixMilli(), datID); err != nil {
		return fmt.Errorf("repair incomplete built-in DAT index: %w", err)
	}
	if !wasActive {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, now.UnixMilli(), artifactID); err != nil {
		return fmt.Errorf("advance artifact for repaired active DAT: %w", err)
	}
	return nil
}

func retireSupersededBuiltInDAT(
	ctx context.Context,
	transaction *sql.Tx,
	artifactID, selectedDATID string,
	now time.Time,
) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE dat_versions
SET is_active=0,version=version+1,updated_at_ms=?
WHERE core_artifact_id=? AND source='BUILTIN' AND id<>? AND is_active=1
`, now.UnixMilli(), artifactID, selectedDATID)
	if err != nil {
		return fmt.Errorf("retire superseded built-in DAT: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count superseded built-in DAT rows: %w", err)
	}
	if changed == 0 {
		return nil
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE core_artifacts SET version=version+1,updated_at_ms=? WHERE id=?
`, now.UnixMilli(), artifactID); err != nil {
		return fmt.Errorf("advance DAT-selected artifact: %w", err)
	}
	return nil
}

func boolToInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

//nolint:funlen // Contract branches stay contiguous for a single auditable decision.
func bootstrapCore(
	ctx context.Context,
	transaction *sql.Tx,
	versionName string,
	version *Version,
	activeVersion bool,
	index int,
	core SelectedCore,
	component struct {
		Repository, SourceCommit, Association string
	},
	now time.Time,
) error {
	path := core.LocalPath
	if core.PathInRelease != nil {
		path = *core.PathInRelease
	}
	compatibilitySchema := 2
	if version.Manifest.SchemaVersion == 5 {
		compatibilitySchema = 3
	}
	compatibility := map[string]any{
		"schemaVersion": compatibilitySchema, "runtimeCoreId": core.RuntimeCoreID,
		"requestedArtifactBasename": core.RequestedArtifactBasename,
		"canvasResizePolicy":        core.CanvasResizePolicy,
		"defaultOptions":            core.DefaultOptions,
		"persistentSaveMode":        core.PersistentSaveMode,
		"persistentSaveKind":        core.PersistentSaveKind,
		"inputMode":                 core.InputMode,
		"startupActions":            core.StartupActions,
	}
	if compatibilitySchema == 3 {
		compatibility["supportedContentKinds"] = core.SupportedContentKinds
		compatibility["multiDisc"] = nil
		if core.MultiDisc != nil {
			compatibility["multiDisc"] = map[string]any{
				"maxDiscs":      core.MultiDisc.MaxDiscs,
				"maxTotalBytes": core.MultiDisc.MaxTotalBytes,
				"delivery":      core.MultiDisc.Delivery,
			}
		}
	}
	association := "INFERRED_BUILD_TIME"
	if component.Association == "EMBEDDED_GIT_VERSION" || component.Association == "EXACT_COMMIT" ||
		component.Association == "EXACT_RELEASE" {
		association = "EXACT_COMMIT"
	}
	provenance := map[string]any{
		"schemaVersion": 1, "dependencyManifestSha256": version.ManifestSHA256,
		"manifestEntryPointer":    fmt.Sprintf("/emulatorjs/selected_core_artifacts/%d", index),
		"sourceAssociationStatus": association,
		"sourceUrl":               component.Repository + "/tree/" + component.SourceCommit,
		"notes":                   []string{},
	}
	compatibilityJSON, _ := json.Marshal(compatibility)
	provenanceJSON, _ := json.Marshal(provenance)
	var id string
	err := transaction.QueryRowContext(ctx,
		"SELECT id FROM core_artifacts WHERE core_id = ? AND emulatorjs_version = ? AND sha256 = ?",
		core.CoreID, versionName, core.SHA256).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		generated, uuidErr := uuid.NewV7()
		if uuidErr != nil {
			return fmt.Errorf("generate core artifact id: %w", uuidErr)
		}
		id = generated.String()
	} else if err != nil {
		return fmt.Errorf("find core artifact: %w", err)
	}
	active := 0
	if activeVersion {
		active = 1
	}
	// Disable first so the partial unique index permits an active-version switch.
	if active == 1 {
		if _, err := transaction.ExecContext(
			ctx,
			`UPDATE core_artifacts SET enabled = 0, version = version + 1, updated_at_ms = ?
WHERE core_id = ? AND enabled = 1 AND id != ?`,
			now.UnixMilli(),
			core.CoreID,
			id,
		); err != nil {
			return fmt.Errorf("disable previous core artifact: %w", err)
		}
	}
	_, err = transaction.ExecContext(
		ctx,
		`
INSERT INTO core_artifacts(id,
 core_id,
 emulatorjs_version,
 bundle_version,
 flavor,
 relative_path,
 size_bytes,
 sha256,
 source_commit,
 provenance_json,
 compatibility_config_json,
 enabled,
 version,
 created_at_ms,
 updated_at_ms)
VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
1,
?,
?)
ON CONFLICT(core_id,
 emulatorjs_version,
 sha256) DO UPDATE SET
  bundle_version=excluded.bundle_version,
 flavor=excluded.flavor,
 relative_path=excluded.relative_path,
  size_bytes=excluded.size_bytes,
 source_commit=excluded.source_commit,
 provenance_json=excluded.provenance_json,
  compatibility_config_json=excluded.compatibility_config_json,
 enabled=excluded.enabled,
 version=core_artifacts.version + CASE WHEN
  core_artifacts.bundle_version IS NOT excluded.bundle_version OR
  core_artifacts.flavor IS NOT excluded.flavor OR
  core_artifacts.relative_path IS NOT excluded.relative_path OR
  core_artifacts.size_bytes IS NOT excluded.size_bytes OR
  core_artifacts.compatibility_config_json IS NOT excluded.compatibility_config_json OR
  core_artifacts.enabled IS NOT excluded.enabled
 THEN 1 ELSE 0 END,
  updated_at_ms=excluded.updated_at_ms
WHERE core_artifacts.bundle_version IS NOT excluded.bundle_version
 OR core_artifacts.flavor IS NOT excluded.flavor
 OR core_artifacts.relative_path IS NOT excluded.relative_path
 OR core_artifacts.size_bytes IS NOT excluded.size_bytes
 OR core_artifacts.source_commit IS NOT excluded.source_commit
 OR core_artifacts.provenance_json IS NOT excluded.provenance_json
 OR core_artifacts.compatibility_config_json IS NOT excluded.compatibility_config_json
 OR core_artifacts.enabled IS NOT excluded.enabled
`,
		id,
		core.CoreID,
		versionName,
		core.BundleVersion,
		core.ArtifactFlavor,
		path,
		core.SizeBytes,
		core.SHA256,
		nullableCommit(association, component.SourceCommit),
		string(provenanceJSON),
		string(compatibilityJSON),
		active,
		now.UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("upsert core artifact: %w", err)
	}
	return nil
}

func nullableCommit(association, commit string) any {
	if association == "EXACT_COMMIT" {
		return commit
	}
	return nil
}
