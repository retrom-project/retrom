package maintenance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	// Register the modernc SQLite driver used by openDatabase.
	_ "modernc.org/sqlite"

	"retrom/internal/blobregistry"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/processlock"
	"retrom/internal/store"
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
	MigrationLineageDigest  string               `json:"migrationLineageDigest"`
	DatabaseSHA256          string               `json:"databaseSha256"`
	ActiveEmulatorjsVersion string               `json:"activeEmulatorjsVersion"`
	DependencyVersions      []string             `json:"dependencyVersions"`
	DependencyManifests     []DependencyManifest `json:"dependencyManifests"`
	Files                   []FileEntry          `json:"files"`
	Counts                  Counts               `json:"counts"`
}

func Backup(
	ctx context.Context,
	configuration config.Maintenance,
	output string,
	now func() time.Time,
) (Manifest, error) {
	if err := validateBackupConfiguration(configuration, output); err != nil {
		return Manifest{}, err
	}
	lock, err := processlock.Acquire(configuration.DataDir)
	if errors.Is(err, processlock.ErrAlreadyRunning) {
		return Manifest{}, ErrBackupOffline
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", lock.Close()) }()
	if err := checkpointBackupDatabase(ctx, configuration.DBPath); err != nil {
		return Manifest{}, err
	}
	staging, err := createStaging(output)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		SchemaVersion:           2,
		CreatedAtMS:             now().UnixMilli(),
		ActiveEmulatorjsVersion: configuration.ActiveEJSVersion,
		DependencyVersions:      append([]string(nil), configuration.DependencyVersions...),
	}
	if err := stageBackupDatabase(ctx, configuration, staging, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := stageBackupSecrets(configuration, staging, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := stageBackupDependencies(configuration, staging, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := publishBackup(staging, output, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateBackupConfiguration(configuration config.Maintenance, output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output || pathWithin(configuration.DataDir, output) ||
		exists(output) {
		return ErrInvalidBundle
	}
	if _, err := dependencies.Load(
		configuration.DependencyRoot,
		configuration.DependencyVersions,
		configuration.ActiveEJSVersion,
	); err != nil {
		return ErrDependencyMismatch
	}
	return nil
}

func checkpointBackupDatabase(ctx context.Context, databasePath string) error {
	database, err := openDatabase(ctx, databasePath)
	if err != nil {
		return err
	}
	if err := checkDatabase(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return err
	}
	if _, err := store.ValidateCurrentMigrationLineage(ctx, database); err != nil {
		cleanup.Error("close", database.Close())
		return ErrInvalidBundle
	}
	var busy, logFrames, checkpointed int
	if err := database.QueryRowContext(ctx, `
PRAGMA wal_checkpoint(TRUNCATE)
`).Scan(&busy, &logFrames, &checkpointed); err != nil ||
		busy != 0 {
		cleanup.Error("close", database.Close())
		return errCheckpointFailed
	}
	if err := database.Close(); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	return nil
}

func stageBackupDatabase(
	ctx context.Context,
	configuration config.Maintenance,
	staging string,
	manifest *Manifest,
) error {
	databaseEntry, err := copyVerified(
		configuration.DBPath,
		filepath.Join(staging, "retrom.db"),
		"retrom.db",
		"DATABASE",
		"",
	)
	if err != nil {
		return err
	}
	manifest.Files = append(manifest.Files, databaseEntry)
	manifest.DatabaseSHA256 = databaseEntry.SHA256
	stagingDatabase, err := openDatabase(ctx, filepath.Join(staging, "retrom.db"))
	if err != nil {
		return err
	}
	if err := checkDatabase(ctx, stagingDatabase); err != nil {
		cleanup.Error("close", stagingDatabase.Close())
		return err
	}
	if err := blobregistry.ValidateSchema(ctx, stagingDatabase); err != nil {
		cleanup.Error("close", stagingDatabase.Close())
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	lineage, err := store.ValidateCurrentMigrationLineage(ctx, stagingDatabase)
	if err != nil {
		cleanup.Error("close", stagingDatabase.Close())
		return ErrInvalidBundle
	}
	manifest.DatabaseSchemaVersion = lineage.Version
	manifest.MigrationLineageDigest = lineage.Digest
	if err := copyBackupBlobs(ctx, stagingDatabase, configuration.DataDir, staging, manifest); err != nil {
		cleanup.Error("close", stagingDatabase.Close())
		return err
	}
	if err := copyBackupParts(ctx, stagingDatabase, configuration.DataDir, staging, manifest); err != nil {
		cleanup.Error("close", stagingDatabase.Close())
		return err
	}
	if err := stagingDatabase.Close(); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	return removeBackupSidecars(staging)
}

func copyBackupBlobs(
	ctx context.Context,
	database *sql.DB,
	dataDir, staging string,
	manifest *Manifest,
) error {
	blobRows, err := database.QueryContext(ctx, `
SELECT sha256,
size_bytes
FROM blobs
ORDER BY sha256
`)
	if err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", blobRows.Close()) }()
	for blobRows.Next() {
		var digest string
		var size int64
		if err := blobRows.Scan(&digest, &size); err != nil {
			return fmt.Errorf("maintenance/bundle: %w", err)
		}
		relative := filepath.ToSlash(filepath.Join("blobs", "sha256", digest[:2], digest[2:4], digest))
		entry, err := copyVerified(
			filepath.Join(dataDir, filepath.FromSlash(relative)),
			filepath.Join(staging, filepath.FromSlash(relative)),
			relative,
			"CAS_BLOB",
			digest,
		)
		if err != nil || entry.SizeBytes != size {
			return ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
		manifest.Counts.BlobCount++
	}
	if err := blobRows.Err(); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	return nil
}

func copyBackupParts(
	ctx context.Context,
	database *sql.DB,
	dataDir, staging string,
	manifest *Manifest,
) error {
	partRows, err := database.QueryContext(
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
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	defer func() { cleanup.Error("close", partRows.Close()) }()
	for partRows.Next() {
		var key, digest string
		var size int64
		if err := partRows.Scan(&key, &size, &digest); err != nil || !safeStorageKey(key) {
			return ErrInvalidBundle
		}
		relative := "tmp/uploads/" + key
		entry, err := copyVerified(
			filepath.Join(dataDir, filepath.FromSlash(relative)),
			filepath.Join(staging, filepath.FromSlash(relative)),
			relative,
			"UPLOAD_PART",
			digest,
		)
		if err != nil || entry.SizeBytes != size {
			return ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
		manifest.Counts.UploadPartCount++
	}
	if err := partRows.Err(); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	return nil
}

func removeBackupSidecars(staging string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		path := filepath.Join(staging, "retrom.db"+suffix)
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() || suffix == "-wal" && info.Size() != 0 {
				return ErrInvalidBundle
			}
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("maintenance/bundle: %w", err)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("maintenance/bundle: %w", err)
		}
	}
	return nil
}

func stageBackupSecrets(configuration config.Maintenance, staging string, manifest *Manifest) error {
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
			return ErrInvalidBundle
		}
		manifest.Files = append(manifest.Files, entry)
	}
	return nil
}

func stageBackupDependencies(configuration config.Maintenance, staging string, manifest *Manifest) error {
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
			return err
		}
		sumsEntry, err := copyVerified(
			filepath.Join(configuration.DependencyRoot, "dat", "emulatorjs", version, "SHA256SUMS"),
			filepath.Join(staging, filepath.FromSlash(sumsRelative)),
			sumsRelative,
			"DEPENDENCY_SHA256SUMS",
			"",
		)
		if err != nil {
			return err
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
	return nil
}

func publishBackup(staging, output string, manifest *Manifest) error {
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	manifest.Counts.FileCount = int64(len(manifest.Files))
	manifest.Counts.DependencyVersionCount = int64(len(manifest.DependencyVersions))
	if err := writeCanonicalManifest(filepath.Join(staging, "backup.json"), *manifest); err != nil {
		return err
	}
	if err := syncTree(staging); err != nil {
		return err
	}
	if err := os.Rename(staging, output); err != nil {
		return fmt.Errorf("maintenance/bundle: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return err
	}
	return nil
}
