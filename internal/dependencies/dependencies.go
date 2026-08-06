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
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/arcadedat"
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
		RuntimeAllowlist []File `json:"runtime_allowlist"`
		SelectedCores    []struct {
			CoreID        string  `json:"core_id"`
			PathInRelease *string `json:"path_in_release"`
			LocalPath     string  `json:"local_path"`
			SizeBytes     int64   `json:"size_bytes"`
			SHA256        string  `json:"sha256"`
			Threads       bool    `json:"requires_threads"`
		} `json:"selected_core_artifacts"`
	} `json:"emulatorjs"`
	Cores []struct {
		CoreID     string `json:"core_id"`
		CoreSource struct {
			Commit            string `json:"commit"`
			AssociationStatus string `json:"association_status"`
		} `json:"core_source"`
		DAT struct {
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
		NoticePath string `json:"third_party_notices_relative_path"`
		Entries    []struct {
			ComponentID             string `json:"component_id"`
			OutputPath              string `json:"output_relative_path"`
			SizeBytes               int64  `json:"size_bytes"`
			SHA256                  string `json:"sha256"`
			SourceCommit            string `json:"source_commit"`
			BinaryAssociationStatus string `json:"binary_association_status"`
			Repository              string `json:"repository"`
		} `json:"entries"`
	} `json:"license_materialization"`
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
	Active   *Version
}

func Load(root string, versions []string, active string) (*Set, error) {
	result := &Set{Versions: make(map[string]*Version, len(versions))}
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

//nolint:gocyclo // Manifest fields are independently validated against the signed dependency contract.
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
	if manifest.SchemaVersion != 3 || manifest.EmulatorJS.Version != versionName {
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
		if err := checkFile(datRoot, core.DAT.LocalPath, core.DAT.SizeBytes, core.DAT.SHA256); err != nil {
			return nil, err
		}
	}
	for _, license := range manifest.Licenses.Entries {
		if err := checkFile(runtimeRoot, license.OutputPath, license.SizeBytes, license.SHA256); err != nil {
			return nil, err
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
	for versionName, version := range set.Versions {
		for index, core := range version.Manifest.EmulatorJS.SelectedCores {
			if err := bootstrapCore(
				ctx,
				transaction,
				versionName,
				version,
				version == set.Active,
				index,
				core.CoreID,
				core.PathInRelease,
				core.LocalPath,
				core.SizeBytes,
				core.SHA256,
				core.Threads,
				now,
			); err != nil {
				return err
			}
		}
		if err := bootstrapStaticBIOS(ctx, transaction, versionName, now); err != nil {
			return err
		}
		for _, core := range version.Manifest.Cores {
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
				version == set.Active,
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
			var artifactEnabled int
			if err := transaction.QueryRowContext(ctx, `
SELECT core_artifact_id,
(SELECT enabled
FROM core_artifacts
WHERE id=core_artifact_id)
FROM dat_versions
WHERE id=?
`, datID).Scan(&artifactID, &artifactEnabled); err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			var activeCount int64
			if err := transaction.QueryRowContext(ctx, `
SELECT count(*)
FROM dat_versions
WHERE core_artifact_id=?
AND is_active=1
`, artifactID).Scan(&activeCount); err != nil {
				cleanup.Rollback(transaction)
				failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
				if firstFailure == nil {
					firstFailure = err
				}
				continue
			}
			activate := artifactEnabled == 1 && activeCount == 0
			_, err = transaction.ExecContext(
				ctx,
				`
UPDATE dat_versions
SET parse_status='READY',
is_active=?,
machine_count=?,
rom_entry_count=?,
disk_entry_count=?,
bios_set_count=?,
default_bios_set_count=?,
explicit_bios_machine_count=?,
base_dependency_target_count=?,
unresolved_relation_count=?,
parsed_at_ms=?,
activated_at_ms=CASE WHEN ? THEN ? ELSE activated_at_ms END,
updated_at_ms=?,
version=version+1
WHERE id=?
`,
				boolToInteger(activate),
				stats.MachineCount,
				stats.ROMEntryCount,
				stats.DiskEntryCount,
				stats.BIOSSetCount,
				stats.DefaultBIOSSetCount,
				stats.ExplicitBIOSMachineCount,
				stats.BaseDependencyTargetCount,
				stats.UnresolvedCloneofTargetCount+stats.UnresolvedRomofTargetCount,
				finishedAtMS,
				activate,
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
			if activate {
				if err := datindex.SyncRequirements(ctx, transaction, datID, now); err != nil {
					cleanup.Rollback(transaction)
					failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
					if firstFailure == nil {
						firstFailure = fmt.Errorf("publish built-in DAT requirements: %w", err)
					}
					continue
				}
				auditID, _ := uuid.NewV7()
				if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(id,
actor,
action,
resource_type,
resource_id,
before_json,
after_json,
diff_json,
created_at_ms) VALUES(?,
'local',
'BUILTIN_DAT_ACTIVATED',
'DAT_VERSION',
?,
'{"active":false}',
'{"active":true}',
json_object('source',
'startup-indexer'),
?)
`, auditID.String(), datID, finishedAtMS); err != nil {
					cleanup.Rollback(transaction)
					failBuiltInDAT(ctx, database, datID, jobID, "DEPENDENCY_DAT_INDEX_WRITE_FAILED", now)
					if firstFailure == nil {
						firstFailure = err
					}
					continue
				}
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
	coreID    string
	logical   string
	mode      string
	condition string
	md5       string
	options   string
	sourceURL string
}

var staticBIOSCatalog = []staticBIOS{
	{
		coreID:    "fceumm",
		logical:   "disksys.rom",
		mode:      "CONDITIONAL",
		condition: "FDS_CONTENT",
		md5:       "ca30b50f880eb660a320674ed365ef7a",
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
		md5:       "fed4d8242cfbed61343d53d48432aced",
		sourceURL: "https://docs.libretro.com/library/snes9x/",
	},
	{
		coreID:    "snes9x",
		logical:   "STBIOS.bin",
		mode:      "OPTIONAL",
		condition: "SNES_SUFAMI_FIRMWARE",
		md5:       "d3a44ba7d42a74d3ac58cb9c14c6a5ca",
		sourceURL: "https://docs.libretro.com/library/snes9x/",
	},
	{
		coreID:    "gambatte",
		logical:   "gb_bios.bin",
		mode:      "OPTIONAL",
		condition: "GB_CONTENT",
		md5:       "32fbbd84168d3482956eb3c5051637f5",
		options:   `{"gambatte_gb_bootloader":"enabled"}`,
		sourceURL: "https://docs.libretro.com/library/gambatte/",
	},
	{
		coreID:    "gambatte",
		logical:   "gbc_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBC_CONTENT",
		md5:       "dbfce9db9deaa2567f6a84fde55f9680",
		options:   `{"gambatte_gb_bootloader":"enabled"}`,
		sourceURL: "https://docs.libretro.com/library/gambatte/",
	},
	{
		coreID:    "mgba",
		logical:   "gba_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBA_CONTENT",
		md5:       "a860e8c0b6d573d191e4ec7db1b1e4f6",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "gb_bios.bin",
		mode:      "OPTIONAL",
		condition: "GB_CONTENT",
		md5:       "32fbbd84168d3482956eb3c5051637f5",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "gbc_bios.bin",
		mode:      "OPTIONAL",
		condition: "GBC_CONTENT",
		md5:       "dbfce9db9deaa2567f6a84fde55f9680",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
	{
		coreID:    "mgba",
		logical:   "sgb_bios.bin",
		mode:      "OPTIONAL",
		condition: "MGBA_SGB_MODEL",
		md5:       "d574d4f9c12f305074798f54c091a8b4",
		options:   `{"mgba_use_bios":"ON"}`,
		sourceURL: "https://docs.libretro.com/library/mgba/",
	},
}

//nolint:funlen // Static BIOS definitions are synchronized atomically with their aliases and version provenance.
func bootstrapStaticBIOS(ctx context.Context, transaction *sql.Tx, versionName string, now time.Time) error {
	if err := validateBIOSActivationOptions(staticBIOSCatalog); err != nil {
		return err
	}
	for _, requirement := range staticBIOSCatalog {
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
				"logicalName":       requirement.logical,
				"md5":               requirement.md5,
				"mode":              requirement.mode,
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
updated_at_ms) VALUES(?,
?,
?,
'STATIC',
NULL,
?,
?,
?,
?,
?,
NULL,
?,
NULL,
NULL,
?,
?,
1,
1,
?,
?) ON CONFLICT(core_artifact_id,
logical_name)
DO UPDATE SET requirement_mode=excluded.requirement_mode,
condition_code=excluded.condition_code,
activation_options_json=excluded.activation_options_json,
catalog_digest=excluded.catalog_digest,
md5=excluded.md5,
source_url=excluded.source_url,
source_version=excluded.source_version,
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
			requirement.md5,
			requirement.sourceURL,
			versionName,
			now.UnixMilli(),
			now.UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("seed BIOS requirement: %w", err)
		}
	}
	return nil
}

func validateBIOSActivationOptions(catalog []staticBIOS) error {
	byCore := make(map[string]map[string]string)
	for _, requirement := range catalog {
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
			if !validASCIIOption(name, 1, 128) || !validASCIIOption(value, 0, 128) {
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

func validASCIIOption(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
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
	var indexedMachineCount, indexedROMCount, indexedDiskCount, indexedBIOSCount int64
	var indexedDefaultBIOSCount, indexedExplicitBIOSCount, indexedBaseTargetCount, indexedUnresolvedCount int64
	if err := transaction.QueryRowContext(ctx, `
SELECT d.parse_status,
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
		&parseStatus, &indexedMachineCount, &indexedROMCount, &indexedDiskCount, &indexedBIOSCount,
		&indexedDefaultBIOSCount, &indexedExplicitBIOSCount, &indexedBaseTargetCount, &indexedUnresolvedCount,
	); err != nil {
		return fmt.Errorf("inspect built-in DAT index: %w", err)
	}
	statsMatch := indexedMachineCount == machineCount && indexedROMCount == romCount && indexedDiskCount == diskCount &&
		indexedBIOSCount == biosSetCount && indexedDefaultBIOSCount == defaultBIOSCount &&
		indexedExplicitBIOSCount == explicitBIOSCount && indexedBaseTargetCount == baseTargetCount &&
		indexedUnresolvedCount == unresolvedCount
	if parseStatus == "READY" && !statsMatch {
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
`, now.UnixMilli(), id); err != nil {
			return fmt.Errorf("repair incomplete built-in DAT index: %w", err)
		}
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
	coreID string,
	pathInRelease *string,
	localPath string,
	size int64,
	digest string,
	threads bool,
	now time.Time,
) error {
	path := localPath
	flavor := "OVERRIDE"
	bundleVersion := versionName
	if pathInRelease != nil {
		path = *pathInRelease
		flavor = "WASM"
		if threads {
			flavor = "THREAD_WASM"
		}
	}
	if coreID == "mame2003" {
		bundleVersion = "4.2.1"
	}
	requestedBasename := coreID + "-wasm.data"
	compatibility := map[string]any{
		"schemaVersion": 1, "requestedArtifactBasename": requestedBasename,
		"canvasResizePolicy": "NONE",
	}
	if coreID == "mame2003" {
		compatibility["canvasResizePolicy"] = "ON_GAME_START_TO_CSS_PIXELS"
	}
	license := version.Manifest.Licenses.Entries[index+1]
	association := "INFERRED_BUILD_TIME"
	if license.BinaryAssociationStatus == "EMBEDDED_GIT_VERSION" {
		association = "EXACT_COMMIT"
	}
	provenance := map[string]any{
		"schemaVersion": 1, "dependencyManifestSha256": version.ManifestSHA256,
		"manifestEntryPointer":    fmt.Sprintf("/emulatorjs/selected_core_artifacts/%d", index),
		"sourceAssociationStatus": association,
		"sourceUrl":               license.Repository + "/tree/" + license.SourceCommit,
		"notes":                   []string{},
	}
	compatibilityJSON, _ := json.Marshal(compatibility)
	provenanceJSON, _ := json.Marshal(provenance)
	var id string
	err := transaction.QueryRowContext(ctx,
		"SELECT id FROM core_artifacts WHERE core_id = ? AND emulatorjs_version = ? AND sha256 = ?",
		coreID, versionName, digest).Scan(&id)
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
			coreID,
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
 provenance_json=excluded.provenance_json,
  compatibility_config_json=excluded.compatibility_config_json,
 enabled=excluded.enabled,
  updated_at_ms=excluded.updated_at_ms
`,
		id,
		coreID,
		versionName,
		bundleVersion,
		flavor,
		path,
		size,
		digest,
		nullableCommit(association, license.SourceCommit),
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
