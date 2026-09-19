package maintenance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"retrom/internal/adapter/runtime/dependencies"
	"retrom/internal/bootstrap/config"
	"retrom/internal/foundation/cleanup"
	"retrom/internal/foundation/processlock"
	model "retrom/internal/model/maintenance"
)

var (
	ErrBackupOffline      = errors.New("BACKUP_REQUIRES_OFFLINE")
	ErrDependencyMismatch = errors.New("RESTORE_DEPENDENCY_CONFIG_MISMATCH")
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

func (service *Service) Backup(
	ctx context.Context,
	configuration config.Maintenance,
	output string,
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
	if err := service.repository.Checkpoint(ctx, configuration.DBPath); err != nil {
		return Manifest{}, fmt.Errorf("prepare backup snapshot: %w", err)
	}
	staging, err := createStaging(output)
	if err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		SchemaVersion:           2,
		CreatedAtMS:             service.now().UnixMilli(),
		ActiveEmulatorjsVersion: configuration.ActiveEJSVersion,
		DependencyVersions:      append([]string(nil), configuration.DependencyVersions...),
	}
	if err := service.stageBackupDatabase(ctx, configuration, staging, &manifest); err != nil {
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
		return model.ErrInvalidBundle
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

func (service *Service) stageBackupDatabase(
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

	snapshot, err := service.repository.Inspect(ctx, filepath.Join(staging, "retrom.db"))
	if err != nil {
		return fmt.Errorf("inspect staged backup: %w", err)
	}
	manifest.DatabaseSchemaVersion = snapshot.Lineage.Version
	manifest.MigrationLineageDigest = snapshot.Lineage.Digest
	if err := copyBackupContents(ctx, snapshot, configuration.DataDir, staging, manifest); err != nil {
		return err
	}
	return removeBackupSidecars(staging)
}

func removeBackupSidecars(staging string) error {
	for _, suffix := range []string{"-wal", "-shm"} {
		path := filepath.Join(staging, "retrom.db"+suffix)
		if info, err := os.Lstat(path); err == nil {
			if !info.Mode().IsRegular() || suffix == "-wal" && info.Size() != 0 {
				return model.ErrInvalidBundle
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
			return model.ErrInvalidBundle
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
