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

	"github.com/google/uuid"
	// Register the modernc SQLite driver used by openDatabase.
	_ "modernc.org/sqlite"

	"retrom/internal/cleanup"
	"retrom/internal/importing"
	"retrom/internal/store"
)

func validateBundle(root string) (Manifest, error) {
	manifest, err := loadBundleManifest(root)
	if err != nil {
		return Manifest{}, err
	}
	expected, err := validateManifestFiles(manifest)
	if err != nil {
		return Manifest{}, err
	}
	if err := validateBundleTree(root, expected); err != nil {
		return Manifest{}, err
	}
	if err := validateDependencyEvidence(manifest, expected); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func loadBundleManifest(root string) (Manifest, error) {
	contents, err := os.ReadFile(
		filepath.Join(root, "backup.json"),
	)
	if err != nil || len(contents) > 16<<20 {
		return Manifest{}, ErrInvalidBundle
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	lineage, lineageErr := store.CurrentMigrationLineage()
	if err := decoder.Decode(&manifest); err != nil || lineageErr != nil || manifest.SchemaVersion != 2 ||
		manifest.DatabaseSchemaVersion != lineage.Version || manifest.MigrationLineageDigest != lineage.Digest ||
		manifest.Counts.FileCount != int64(len(manifest.Files)) ||
		manifest.Counts.DependencyVersionCount != int64(len(manifest.DependencyVersions)) ||
		len(manifest.DependencyManifests) != len(manifest.DependencyVersions) {
		return Manifest{}, ErrInvalidBundle
	}
	return manifest, nil
}

type manifestInventory struct {
	databaseCount int64
	launchKeys    int64
	netplayKeys   int64
	blobs         int64
	parts         int64
}

func validateManifestFiles(manifest Manifest) (map[string]FileEntry, error) {
	seen, folded := map[string]struct{}{}, map[string]struct{}{}
	expected := map[string]FileEntry{}
	inventory := manifestInventory{}
	previous := ""
	for _, entry := range manifest.Files {
		if err := validateManifestEntry(entry, previous, manifest, seen, folded, &inventory); err != nil {
			return nil, ErrInvalidBundle
		}
		previous = entry.Path
		expected[entry.Path] = entry
	}
	if inventory.databaseCount != 1 || inventory.launchKeys != 1 || inventory.netplayKeys != 1 ||
		inventory.blobs != manifest.Counts.BlobCount || inventory.parts != manifest.Counts.UploadPartCount {
		return nil, ErrInvalidBundle
	}
	return expected, nil
}

func validateManifestEntry(
	entry FileEntry,
	previous string,
	manifest Manifest,
	seen, folded map[string]struct{},
	inventory *manifestInventory,
) error {
	if !safeManifestPath(entry.Path) || entry.Mode != "0600" || len(entry.SHA256) != 64 || entry.Path <= previous {
		return ErrInvalidBundle
	}
	fold := asciiFold(entry.Path)
	if _, duplicate := seen[entry.Path]; duplicate {
		return ErrInvalidBundle
	}
	if _, duplicate := folded[fold]; duplicate {
		return ErrInvalidBundle
	}
	seen[entry.Path], folded[fold] = struct{}{}, struct{}{}
	return validateManifestEntryKind(entry, manifest, inventory)
}

func validateManifestEntryKind(entry FileEntry, manifest Manifest, inventory *manifestInventory) error {
	switch entry.Kind {
	case "DATABASE":
		inventory.databaseCount++
		return validateDatabaseManifestEntry(entry, manifest)
	case "CAS_BLOB":
		inventory.blobs++
		return requireManifestEntry(validCASPath(entry.Path, entry.SHA256))
	case "UPLOAD_PART":
		inventory.parts++
		return validateUploadPartManifestEntry(entry)
	case "LAUNCH_KEY":
		inventory.launchKeys++
		return validateSecretManifestEntry(entry, "secrets/launch-capability.key")
	case "NETPLAY_KEY":
		inventory.netplayKeys++
		return validateSecretManifestEntry(entry, "secrets/netplay-capability.key")
	case "DEPENDENCY_MANIFEST", "DEPENDENCY_SHA256SUMS":
		return requireManifestEntry(strings.HasPrefix(entry.Path, "dependencies/emulatorjs/"))
	default:
		return ErrInvalidBundle
	}
}

func validateDatabaseManifestEntry(entry FileEntry, manifest Manifest) error {
	return requireManifestEntry(entry.Path == "retrom.db" && entry.SHA256 == manifest.DatabaseSHA256)
}

func validateUploadPartManifestEntry(entry FileEntry) error {
	return requireManifestEntry(strings.HasPrefix(entry.Path, "tmp/uploads/") &&
		safeStorageKey(strings.TrimPrefix(entry.Path, "tmp/uploads/")))
}

func validateSecretManifestEntry(entry FileEntry, expectedPath string) error {
	return requireManifestEntry(entry.Path == expectedPath && entry.SizeBytes == 32)
}

func requireManifestEntry(valid bool) error {
	if !valid {
		return ErrInvalidBundle
	}
	return nil
}

func validateBundleTree(root string, expected map[string]FileEntry) error {
	actual := map[string]struct{}{}
	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
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
		return ErrInvalidBundle
	}
	return nil
}

func validateDependencyEvidence(manifest Manifest, expected map[string]FileEntry) error {
	for index, evidence := range manifest.DependencyManifests {
		if evidence.Version != manifest.DependencyVersions[index] ||
			expected[evidence.ManifestPath].SHA256 != evidence.ManifestSHA256 ||
			expected[evidence.SHA256SumsPath].SHA256 != evidence.SHA256SumsSHA256 {
			return ErrInvalidBundle
		}
	}
	return nil
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
		"createdAtMs":            value.CreatedAtMS,
		"databaseSchemaVersion":  value.DatabaseSchemaVersion,
		"databaseSha256":         value.DatabaseSHA256,
		"migrationLineageDigest": value.MigrationLineageDigest,
		"dependencyManifests":    dependenciesValue,
		"dependencyVersions":     value.DependencyVersions,
		"files":                  files,
		"schemaVersion":          value.SchemaVersion,
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
	output, err := os.OpenFile(
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
	file, err := os.OpenFile(
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
	f, err := os.OpenFile(
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
	directory, err := os.Open(
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
			return os.Chmod(
				path,
				0o700,
			)
		}
		return os.Chmod(
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

func validateRestoredFiles(ctx context.Context, database *sql.DB, root string) error {
	if err := validateRestoredBlobs(ctx, database, root); err != nil {
		return err
	}
	return validateRestoredUploadParts(ctx, database, root)
}

func validateRestoredBlobs(ctx context.Context, database *sql.DB, root string) error {
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
	return nil
}

func validateRestoredUploadParts(ctx context.Context, database *sql.DB, root string) error {
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
