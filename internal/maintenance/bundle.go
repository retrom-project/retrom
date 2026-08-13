package maintenance

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	// Register the modernc SQLite driver used by openDatabase.
	_ "modernc.org/sqlite"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/importing"
	"retrom/internal/processlock"
)

var (
	ErrBackupOffline      = errors.New("BACKUP_REQUIRES_OFFLINE")
	ErrInvalidBundle      = errors.New("BACKUP_BUNDLE_INVALID")
	ErrDependencyMismatch = errors.New("RESTORE_DEPENDENCY_CONFIG_MISMATCH")
	errCheckpointFailed   = errors.New("BACKUP_CHECKPOINT_FAILED")
)

type FileEntry struct {
	Path      string `json:"path"`
	Kind      string `json:"kind"`
	SizeBytes int64  `json:"sizeBytes"`
	SHA256    string `json:"sha256"`
	Mode      string `json:"mode"`
}

type DependencyManifest struct {
	Version          string `json:"version"`
	ManifestPath     string `json:"manifestPath"`
	ManifestSHA256   string `json:"manifestSha256"`
	SHA256SumsPath   string `json:"sha256sumsPath"`
	SHA256SumsSHA256 string `json:"sha256sumsSha256"`
}

type Counts struct {
	FileCount              int64 `json:"fileCount"`
	BlobCount              int64 `json:"blobCount"`
	UploadPartCount        int64 `json:"uploadPartCount"`
	DependencyVersionCount int64 `json:"dependencyVersionCount"`
}

type Manifest struct {
	SchemaVersion           int                  `json:"schemaVersion"`
	CreatedAtMS             int64                `json:"createdAtMs"`
	DatabaseSchemaVersion   int64                `json:"databaseSchemaVersion"`
	DatabaseSHA256          string               `json:"databaseSha256"`
	ActiveEmulatorjsVersion string               `json:"activeEmulatorjsVersion"`
	DependencyVersions      []string             `json:"dependencyVersions"`
	DependencyManifests     []DependencyManifest `json:"dependencyManifests"`
	Files                   []FileEntry          `json:"files"`
	Counts                  Counts               `json:"counts"`
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func Backup(
	ctx context.Context,
	configuration config.Maintenance,
	output string,
	now func() time.Time,
) (Manifest, error) {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output || pathWithin(configuration.DataDir, output) ||
		exists(output) {
		return Manifest{}, ErrInvalidBundle
	}
	if _, err := dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	); err != nil {
		return Manifest{}, ErrDependencyMismatch
	}
	lock, err := processlock.Acquire(configuration.DataDir)
	if errors.Is(err, processlock.ErrAlreadyRunning) {
		return Manifest{}, ErrBackupOffline
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", lock.Close()) }()
	database, err := openDatabase(ctx, configuration.DBPath)
	if err != nil {
		return Manifest{}, err
	}
	if err := checkDatabase(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return Manifest{}, err
	}
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, `
PRAGMA wal_checkpoint(TRUNCATE)
`).Scan(&busy, &logFrames, &checkpointed); err != nil ||
		busy != 0 {
		cleanup.Error("close", database.Close())
		return Manifest{}, errCheckpointFailed
	}
	if err := database.Close(); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	staging, err := createStaging(output)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		SchemaVersion:           1,
		CreatedAtMS:             now().UnixMilli(),
		ActiveEmulatorjsVersion: configuration.ActiveEJSVersion,
		DependencyVersions:      append([]string(nil), configuration.DependencyVersions...),
	}
	databaseEntry, err := copyVerified(
		configuration.DBPath,
		filepath.Join(staging, "retrom.db"),
		"retrom.db",
		"DATABASE",
		"",
	)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Files = append(manifest.Files, databaseEntry)
	manifest.DatabaseSHA256 = databaseEntry.SHA256
	stagingDatabase, err := openDatabase(ctx, filepath.Join(staging, "retrom.db"))
	if err != nil {
		return Manifest{}, err
	}
	if err := checkDatabase(ctx, stagingDatabase); err != nil {
		return Manifest{}, err
	}
	if err := blobregistry.ValidateSchema(ctx, stagingDatabase); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := stagingDatabase.QueryRowContext(ctx, `
SELECT COALESCE(MAX(version),
0)
FROM schema_migrations
`).Scan(&manifest.DatabaseSchemaVersion); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	blobRows, err := stagingDatabase.QueryContext(ctx, `
SELECT sha256,
size_bytes
FROM blobs
ORDER BY sha256
`)
	if err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", blobRows.Close()) }()
	for blobRows.Next() {
		var digest string
		var size int64
		if err := blobRows.Scan(&digest, &size); err != nil {
			return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
		}
		relative := filepath.ToSlash(filepath.Join("blobs", "sha256", digest[:2], digest[2:4], digest))
		entry, err := copyVerified(
			filepath.Join(configuration.DataDir, filepath.FromSlash(relative)),
			filepath.Join(staging, filepath.FromSlash(relative)),
			relative,
			"CAS_BLOB",
			digest,
		)
		if err != nil || entry.SizeBytes != size {
			return Manifest{}, ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
		manifest.Counts.BlobCount++
	}
	if err := blobRows.Err(); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	partRows, err := stagingDatabase.QueryContext(
		ctx,
		`
SELECT p.storage_key,
p.size_bytes,
p.sha256
FROM upload_parts p
JOIN upload_files f ON f.id=p.upload_file_id
JOIN upload_sessions u ON u.id=f.upload_session_id
WHERE u.state!='COMPLETE'
ORDER BY p.storage_key
`,
	)
	if err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", partRows.Close()) }()
	for partRows.Next() {
		var key, digest string
		var size int64
		if err := partRows.Scan(&key, &size, &digest); err != nil || !safeStorageKey(key) {
			return Manifest{}, ErrInvalidBundle
		}
		relative := "tmp/uploads/" + key
		entry, err := copyVerified(
			filepath.Join(configuration.DataDir, filepath.FromSlash(relative)),
			filepath.Join(staging, filepath.FromSlash(relative)),
			relative,
			"UPLOAD_PART",
			digest,
		)
		if err != nil || entry.SizeBytes != size {
			return Manifest{}, ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
		manifest.Counts.UploadPartCount++
	}
	if err := partRows.Err(); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := stagingDatabase.Close(); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		path := filepath.Join(staging, "retrom.db"+suffix)
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() || suffix == "-wal" && info.Size() != 0 {
				return Manifest{}, ErrInvalidBundle
			}
			if err := os.Remove(path); err != nil {
				return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
		}
	}
	for _, secret := range []struct{ name, kind string }{
		{"launch-capability.key", "LAUNCH_KEY"}, {"netplay-capability.key", "NETPLAY_KEY"},
	} {
		entry, err := copyVerified(
			filepath.Join(configuration.DataDir, "secrets", secret.name),
			filepath.Join(staging, "secrets", secret.name),
			"secrets/"+secret.name,
			secret.kind,
			"",
		)
		if err != nil || entry.SizeBytes != 32 {
			return Manifest{}, ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
	}
	for _, version := range configuration.DependencyVersions {
		manifestRelative := "dependencies/emulatorjs/" + version + "/manifest.json"
		sumsRelative := "dependencies/emulatorjs/" + version + "/SHA256SUMS"
		manifestEntry, err := copyVerified(
			filepath.Join(configuration.DependencyRoot, "dat", "emulatorjs", version, "manifest.json"),
			filepath.Join(staging, filepath.FromSlash(manifestRelative)),
			manifestRelative,
			"DEPENDENCY_MANIFEST",
			"",
		)
		if err != nil {
			return Manifest{}, err
		}
		sumsEntry, err := copyVerified(
			filepath.Join(configuration.DependencyRoot, "dat", "emulatorjs", version, "SHA256SUMS"),
			filepath.Join(staging, filepath.FromSlash(sumsRelative)),
			sumsRelative,
			"DEPENDENCY_SHA256SUMS",
			"",
		)
		if err != nil {
			return Manifest{}, err
		}
		manifest.Files = append(manifest.Files, manifestEntry, sumsEntry)
		manifest.DependencyManifests = append(
			manifest.DependencyManifests,
			DependencyManifest{
				Version:          version,
				ManifestPath:     manifestRelative,
				ManifestSHA256:   manifestEntry.SHA256,
				SHA256SumsPath:   sumsRelative,
				SHA256SumsSHA256: sumsEntry.SHA256,
			},
		)
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	manifest.Counts.FileCount = int64(len(manifest.Files))
	manifest.Counts.DependencyVersionCount = int64(len(manifest.DependencyVersions))
	if err := writeCanonicalManifest(filepath.Join(staging, "backup.json"), manifest); err != nil {
		return Manifest{}, err
	}
	if err := syncTree(staging); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(staging, output); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func Restore(ctx context.Context, configuration config.Maintenance, input, output string) (Manifest, error) {
	if !filepath.IsAbs(input) || !filepath.IsAbs(output) || filepath.Clean(input) != input ||
		filepath.Clean(output) != output ||
		exists(output) {
		return Manifest{}, ErrInvalidBundle
	}
	manifest, err := validateBundle(input)
	if err != nil {
		return Manifest{}, err
	}
	if strings.Join(configuration.DependencyVersions, ",") != strings.Join(manifest.DependencyVersions, ",") ||
		configuration.ActiveEJSVersion != manifest.ActiveEmulatorjsVersion {
		return Manifest{}, ErrDependencyMismatch
	}
	if _, err := dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	); err != nil {
		return Manifest{}, ErrDependencyMismatch
	}
	for _, evidence := range manifest.DependencyManifests {
		manifestRoot := filepath.Join(configuration.DependencyRoot, "dat", "emulatorjs", evidence.Version)
		for _, value := range []struct{ source, expected string }{
			{filepath.Join(manifestRoot, "manifest.json"), evidence.ManifestSHA256},
			{filepath.Join(manifestRoot, "SHA256SUMS"), evidence.SHA256SumsSHA256},
		} {
			if digest, _, err := digestRegular(value.source); err != nil || digest != value.expected {
				return Manifest{}, ErrDependencyMismatch
			}
		}
	}
	staging, err := createStaging(output)
	if err != nil {
		return Manifest{}, err
	}
	for _, entry := range manifest.Files {
		if entry.Kind == "DEPENDENCY_MANIFEST" || entry.Kind == "DEPENDENCY_SHA256SUMS" {
			continue
		}
		if _, err := copyVerified(
			filepath.Join(input, filepath.FromSlash(entry.Path)),
			filepath.Join(staging, filepath.FromSlash(entry.Path)),
			entry.Path,
			entry.Kind,
			entry.SHA256,
		); err != nil {
			return Manifest{}, err
		}
	}
	database, err := openDatabase(ctx, filepath.Join(staging, "retrom.db"))
	if err != nil {
		return Manifest{}, err
	}
	if err := checkDatabase(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return Manifest{}, err
	}
	if err := blobregistry.ValidateSchema(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := validateRestoredFiles(ctx, database, staging); err != nil {
		cleanup.Error("close", database.Close())
		return Manifest{}, err
	}
	if err := applyRestoreSecurityFence(ctx, database, time.Now().UTC()); err != nil {
		cleanup.Error("close", database.Close())
		return Manifest{}, err
	}
	if err := database.Close(); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := syncTree(staging); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(staging, output); err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

//nolint:funlen,lll // Restore revocations and external-source task fencing are one atomic security boundary.
func applyRestoreSecurityFence(ctx context.Context, database *sql.DB, now time.Time) error {
	nowMS := now.UnixMilli()
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: begin restore security fence: %w", err)
	}
	defer cleanup.Rollback(transaction)
	sessions, err := transaction.ExecContext(ctx, `
UPDATE auth_sessions SET revoked_at_ms=?,revoked_reason='RESTORE' WHERE revoked_at_ms IS NULL
`, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored sessions: %w", err)
	}
	links, err := transaction.ExecContext(ctx, `
UPDATE account_links SET revoked_at_ms=?,revoked_by_kind='SYSTEM',version=version+1
WHERE consumed_at_ms IS NULL AND revoked_at_ms IS NULL AND expires_at_ms>?
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored account links: %w", err)
	}
	launches, err := transaction.ExecContext(ctx, `
UPDATE launch_sessions SET state='REVOKED',finished_at_ms=?,updated_at_ms=?,version=version+1
WHERE state IN ('CREATED','ACTIVE')
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored launches: %w", err)
	}
	serverJobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind='SERVER_BIOS_IMPORT' AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored server import jobs: %w", err)
	}
	pegasusJobs, err := transaction.ExecContext(ctx, `
UPDATE jobs SET state='FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',error_retryable=0,
finished_at_ms=?,leased_until_ms=NULL,heartbeat_at_ms=NULL,worker_id=NULL,
cancel_requested_at_ms=NULL,cancel_reason=NULL,version=version+1,updated_at_ms=?
WHERE kind IN ('SERVER_PEGASUS_SCAN','SERVER_PEGASUS_IMPORT') AND state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored Pegasus jobs: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_bios_import_items SET state='COMMIT_FAILED',outcome_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
completed_at_ms=?,updated_at_ms=? WHERE server_import_id IN (
  SELECT id FROM server_imports WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
) AND state IN ('PENDING','EVALUATING')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored server import items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE server_imports SET state='FAILED',last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
cancel_requested_at_ms=NULL,cancel_reason=NULL,
imported_matched_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_MATCHED'),
imported_warning_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_WARNING'),
imported_missing_entry_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='IMPORTED_MISSING_ENTRY'),
not_found_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='NOT_FOUND'),
skipped_existing_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='SKIPPED_EXISTING'),
skipped_not_better_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='SKIPPED_NOT_BETTER'),
same_bytes_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='ALREADY_SAME_BYTES'),
failed_item_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state IN ('SOURCE_CHANGED','CATALOG_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM server_bios_import_items item WHERE item.server_import_id=server_imports.id AND item.state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored server imports: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_import_items SET execution_state='COMMIT_FAILED',error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,completed_at_ms=?,updated_at_ms=? WHERE import_id IN (
  SELECT id FROM pegasus_imports WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
) AND execution_state IN ('PENDING','COPYING','VALIDATING','PUBLISHING')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored Pegasus items: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE pegasus_imports SET state='FAILED',phase=NULL,last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED',
retryable=0,cancel_reason=NULL,
published_item_count=(SELECT count(*) FROM pegasus_import_items item WHERE item.import_id=pegasus_imports.id AND item.execution_state='PUBLISHED'),
existing_item_count=(SELECT count(*) FROM pegasus_import_items item WHERE item.import_id=pegasus_imports.id AND item.execution_state='SKIPPED_EXISTING'),
blocked_item_count=(SELECT count(*) FROM pegasus_import_items item WHERE item.import_id=pegasus_imports.id AND item.execution_state IN ('BLOCKED_SOURCE','BLOCKED_CONTENT','BLOCKED_VALIDATION')),
failed_item_count=(SELECT count(*) FROM pegasus_import_items item WHERE item.import_id=pegasus_imports.id AND item.execution_state IN ('SOURCE_CHANGED','READ_FAILED','COMMIT_FAILED')),
cancelled_item_count=(SELECT count(*) FROM pegasus_import_items item WHERE item.import_id=pegasus_imports.id AND item.execution_state='CANCELLED'),
completed_at_ms=?,version=version+1,updated_at_ms=?
WHERE state IN ('SCANNING','AWAITING_MAPPING','QUEUED','RUNNING','CANCEL_REQUESTED')
`, nowMS, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: fence restored Pegasus imports: %w", err)
	}
	sessionCount, _ := sessions.RowsAffected()
	linkCount, _ := links.RowsAffected()
	launchCount, _ := launches.RowsAffected()
	serverJobCount, _ := serverJobs.RowsAffected()
	pegasusJobCount, _ := pegasusJobs.RowsAffected()
	auditID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("maintenance/bundle: create restore audit ID: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO audit_events(
id,actor_kind,actor_user_id,actor_label,action,resource_type,resource_id,
before_json,after_json,diff_json,request_id,created_at_ms)
VALUES(?,'SYSTEM',NULL,'restore-security-fence','RESTORE_SECURITY_FENCE','INSTANCE','instance',
NULL,json_object('revokedSessionCount',?,'revokedAccountLinkCount',?,'revokedLaunchCount',?,'failedServerImportCount',?,'failedPegasusJobCount',?),
'{}',NULL,?)
`, auditID.String(), sessionCount, linkCount, launchCount, serverJobCount, pegasusJobCount, nowMS); err != nil {
		return fmt.Errorf("maintenance/bundle: audit restore security fence: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("maintenance/bundle: commit restore security fence: %w", err)
	}
	return nil
}

//nolint:funlen,gocognit,gocyclo // Contract branches stay contiguous for a single auditable decision.
func validateBundle(root string) (Manifest, error) {
	contents, err := os.ReadFile( //nolint:gosec // Fixed slot in an exclusive bundle root.
		filepath.Join(root, "backup.json"),
	)
	if err != nil || len(contents) > 16<<20 {
		return Manifest{}, ErrInvalidBundle
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil || manifest.SchemaVersion != 1 ||
		manifest.DatabaseSchemaVersion < 1 ||
		manifest.Counts.FileCount != int64(len(manifest.Files)) ||
		manifest.Counts.DependencyVersionCount != int64(len(manifest.DependencyVersions)) ||
		len(manifest.DependencyManifests) != len(manifest.DependencyVersions) {
		return Manifest{}, ErrInvalidBundle
	}
	seen, folded := map[string]struct{}{}, map[string]struct{}{}
	expected := map[string]FileEntry{}
	var databaseCount, launchKeyCount, netplayKeyCount, blobs, parts int64
	previous := ""
	for _, entry := range manifest.Files {
		if !safeManifestPath(entry.Path) || entry.Mode != "0600" || len(entry.SHA256) != 64 || entry.Path <= previous {
			return Manifest{}, ErrInvalidBundle
		}
		previous = entry.Path
		fold := asciiFold(entry.Path)
		if _, ok := seen[entry.Path]; ok {
			return Manifest{}, ErrInvalidBundle
		}
		if _, ok := folded[fold]; ok {
			return Manifest{}, ErrInvalidBundle
		}
		seen[entry.Path], folded[fold], expected[entry.Path] = struct{}{}, struct{}{}, entry
		switch entry.Kind {
		case "DATABASE":
			if entry.Path != "retrom.db" || entry.SHA256 != manifest.DatabaseSHA256 {
				return Manifest{}, ErrInvalidBundle
			}
			databaseCount++
		case "CAS_BLOB":
			if !validCASPath(entry.Path, entry.SHA256) {
				return Manifest{}, ErrInvalidBundle
			}
			blobs++
		case "UPLOAD_PART":
			if !strings.HasPrefix(entry.Path, "tmp/uploads/") ||
				!safeStorageKey(strings.TrimPrefix(entry.Path, "tmp/uploads/")) {
				return Manifest{}, ErrInvalidBundle
			}
			parts++
		case "LAUNCH_KEY":
			if entry.Path != "secrets/launch-capability.key" || entry.SizeBytes != 32 {
				return Manifest{}, ErrInvalidBundle
			}
			launchKeyCount++
		case "NETPLAY_KEY":
			if entry.Path != "secrets/netplay-capability.key" || entry.SizeBytes != 32 {
				return Manifest{}, ErrInvalidBundle
			}
			netplayKeyCount++
		case "DEPENDENCY_MANIFEST", "DEPENDENCY_SHA256SUMS":
			if !strings.HasPrefix(entry.Path, "dependencies/emulatorjs/") {
				return Manifest{}, ErrInvalidBundle
			}
		default:
			return Manifest{}, ErrInvalidBundle
		}
	}
	if databaseCount != 1 || launchKeyCount != 1 || netplayKeyCount != 1 || blobs != manifest.Counts.BlobCount ||
		parts != manifest.Counts.UploadPartCount {
		return Manifest{}, ErrInvalidBundle
	}
	actual := map[string]struct{}{}
	err = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := item.Info()
		if err != nil {
			return fmt.Errorf("maintenance/bundle: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidBundle
		}
		if item.IsDir() {
			if info.Mode().Perm() != 0o700 {
				return ErrInvalidBundle
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return ErrInvalidBundle
		}
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		actual[relative] = struct{}{}
		if relative == "backup.json" {
			return nil
		}
		entry, ok := expected[relative]
		if !ok {
			return ErrInvalidBundle
		}
		digest, size, err := digestRegular(path)
		if err != nil || digest != entry.SHA256 || size != entry.SizeBytes {
			return ErrInvalidBundle
		}
		return nil
	})
	if err != nil || len(actual) != len(expected)+1 {
		return Manifest{}, ErrInvalidBundle
	}
	for index, evidence := range manifest.DependencyManifests {
		if evidence.Version != manifest.DependencyVersions[index] ||
			expected[evidence.ManifestPath].SHA256 != evidence.ManifestSHA256 ||
			expected[evidence.SHA256SumsPath].SHA256 != evidence.SHA256SumsSHA256 {
			return Manifest{}, ErrInvalidBundle
		}
	}
	return manifest, nil
}

func writeCanonicalManifest(path string, manifest Manifest) error {
	encoded, err := json.Marshal(manifestToMap(manifest))
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	encoded = append(encoded, '\n')
	return writeExclusive(path, encoded)
}

func manifestToMap(value Manifest) map[string]any {
	files := make([]any, 0, len(value.Files))
	for _, item := range value.Files {
		files = append(
			files,
			map[string]any{
				"kind":      item.Kind,
				"mode":      item.Mode,
				"path":      item.Path,
				"sha256":    item.SHA256,
				"sizeBytes": item.SizeBytes,
			},
		)
	}
	dependenciesValue := make([]any, 0, len(value.DependencyManifests))
	for _, item := range value.DependencyManifests {
		dependenciesValue = append(
			dependenciesValue,
			map[string]any{
				"manifestPath":     item.ManifestPath,
				"manifestSha256":   item.ManifestSHA256,
				"sha256sumsPath":   item.SHA256SumsPath,
				"sha256sumsSha256": item.SHA256SumsSHA256,
				"version":          item.Version,
			},
		)
	}
	return map[string]any{
		"activeEmulatorjsVersion": value.ActiveEmulatorjsVersion,
		"counts": map[string]any{
			"blobCount":              value.Counts.BlobCount,
			"dependencyVersionCount": value.Counts.DependencyVersionCount,
			"fileCount":              value.Counts.FileCount,
			"uploadPartCount":        value.Counts.UploadPartCount,
		},
		"createdAtMs":           value.CreatedAtMS,
		"databaseSchemaVersion": value.DatabaseSchemaVersion,
		"databaseSha256":        value.DatabaseSHA256,
		"dependencyManifests":   dependenciesValue,
		"dependencyVersions":    value.DependencyVersions,
		"files":                 files,
		"schemaVersion":         value.SchemaVersion,
	}
}

func createStaging(output string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		return "", fmt.Errorf("maintenance/bundle: %w", err)
	}
	id, _ := uuid.NewV7()
	staging := filepath.Join(filepath.Dir(output), "."+filepath.Base(output)+".staging-"+id.String())
	if err := os.Mkdir(staging, 0o700); err != nil {
		return "", fmt.Errorf("maintenance/bundle: %w", err)
	}
	return staging, nil
}

func copyVerified(source, target, relative, kind, expected string) (FileEntry, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return FileEntry{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	input, err := openRegular(source)
	if err != nil {
		return FileEntry{}, err
	}
	defer func() { cleanup.Error("close", input.Close()) }()
	output, err := os.OpenFile( //nolint:gosec // Validated path in a new exclusive root.
		target,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return FileEntry{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, hash), input)
	syncErr := output.Sync()
	closeErr := output.Close()
	digest := hex.EncodeToString(hash.Sum(nil))
	if copyErr != nil || syncErr != nil || closeErr != nil || expected != "" && digest != expected {
		return FileEntry{}, ErrInvalidBundle
	}
	return FileEntry{Path: relative, Kind: kind, SizeBytes: size, SHA256: digest, Mode: "0600"}, nil
}

func openRegular(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidBundle
	}
	file, err := os.OpenFile( //nolint:gosec // Lstat plus O_NOFOLLOW blocks symlinks.
		path,
		os.O_RDONLY|syscall.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, ErrInvalidBundle
	}
	return file, nil
}

func digestRegular(path string) (string, int64, error) {
	file, err := openRegular(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	return hex.EncodeToString(hash.Sum(nil)), size, err
}

func writeExclusive(path string, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	f, err := os.OpenFile( //nolint:gosec // Validated slot in a new exclusive restore root.
		path,
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if _, err = f.Write(contents); err != nil {
		cleanup.Error("close", f.Close())
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err = f.Sync(); err != nil {
		cleanup.Error("close", f.Close())
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close synchronized file: %w", err)
	}
	return nil
}

func openDatabase(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("maintenance/bundle: %w", err)
	}
	for _, pragma := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.ExecContext(ctx, pragma); err != nil {
			cleanup.Error("close", db.Close())
			return nil, fmt.Errorf("maintenance/bundle: %w", err)
		}
	}
	return db, nil
}

func checkDatabase(ctx context.Context, db *sql.DB) error {
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return ErrInvalidBundle
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	if rows.Next() {
		return ErrInvalidBundle
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("scan foreign-key violations: %w", err)
	}
	return nil
}

func safeStorageKey(value string) bool {
	_, err := importing.ValidateLogicalPath(value)
	return !strings.HasPrefix(value, "tmp/uploads/") && err == nil
}

func safeManifestPath(value string) bool {
	if _, err := importing.ValidateLogicalPath(value); err != nil {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	return true
}

func asciiFold(value string) string {
	var result strings.Builder
	for _, r := range value {
		if r >= 'A' && r <= 'Z' {
			r += 32
		}
		result.WriteRune(r)
	}
	return result.String()
}

func validCASPath(path, digest string) bool {
	return len(digest) == 64 && path == "blobs/sha256/"+digest[:2]+"/"+digest[2:4]+"/"+digest
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}

func syncDirectory(path string) error {
	directory, err := os.Open( //nolint:gosec // Application-created directory in an exclusive root.
		path,
	)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("synchronize directory: %w", err)
	}
	return nil
}

func syncTree(root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if item.IsDir() {
			directories = append(directories, path)
			return os.Chmod( //nolint:gosec // Directories require owner execute permission and remain owner-only.
				path,
				0o700,
			)
		}
		return os.Chmod( //nolint:gosec // No untrusted writer can race the exclusive root.
			path,
			0o600,
		)
	})
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	sort.Slice(directories, func(i, j int) bool { return len(directories[i]) > len(directories[j]) })
	for _, directory := range directories {
		if err := syncDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

//nolint:gocyclo // Each branch validates a distinct restored-file ownership and digest invariant.
func validateRestoredFiles(ctx context.Context, database *sql.DB, root string) error {
	rows, err := database.QueryContext(ctx, `
SELECT sha256,
size_bytes
FROM blobs
`)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	for rows.Next() {
		var digest string
		var size int64
		if rows.Scan(&digest, &size) != nil {
			return ErrInvalidBundle
		}
		path := filepath.Join(root, "blobs", "sha256", digest[:2], digest[2:4], digest)
		actualDigest, actualSize, err := digestRegular(path)
		if err != nil || actualDigest != digest || actualSize != size {
			return ErrInvalidBundle
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	parts, err := database.QueryContext(
		ctx,
		`
SELECT storage_key,
size_bytes,
sha256
FROM upload_parts p
JOIN upload_files f ON f.id=p.upload_file_id
JOIN upload_sessions u ON u.id=f.upload_session_id
WHERE u.state!='COMPLETE'
`,
	)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", parts.Close()) }()
	for parts.Next() {
		var key, digest string
		var size int64
		if parts.Scan(&key, &size, &digest) != nil || !safeStorageKey(key) {
			return ErrInvalidBundle
		}
		actualDigest, actualSize, err := digestRegular(filepath.Join(root, "tmp", "uploads", filepath.FromSlash(key)))
		if err != nil || actualDigest != digest || actualSize != size {
			return ErrInvalidBundle
		}
	}
	if err := parts.Err(); err != nil {
		return fmt.Errorf("scan upload parts: %w", err)
	}
	return nil
}
