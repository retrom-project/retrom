//go:build integration

package maintenance

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestBackupRestoreRoundTripAndOnlineRefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "source")
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencyRoot := filepath.Join(repositoryRoot, "data")
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	dependencySet, err := dependencies.Load(dependencyRoot, []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files:      []uploads.FileDeclaration{{ClientFileID: "part", RelativePath: "partial.bin", SizeBytes: 4}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	part := []byte("data")
	digest := "sha-256=:Om6weQ85rIfJTzhWst0sXREOaBFgImGpqSPTuyOtyLc=:"
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, "bytes 0-3/4", digest, bytes.NewReader(part)); err != nil {
		t.Fatal(err)
	}
	if _, err := retromruntime.LoadOrCreateCredentials(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	configuration := config.Maintenance{
		DataDir:            dataDir,
		DBPath:             filepath.Join(dataDir, "retrom.db"),
		DependencyRoot:     dependencyRoot,
		DependencyVersions: []string{"4.2.3"},
		ActiveEJSVersion:   "4.2.3",
	}
	lock, err := processlock.Acquire(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(ctx, configuration, filepath.Join(root, "online-backup"), time.Now); !errors.Is(
		err,
		ErrBackupOffline,
	) {
		t.Fatalf("online backup error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	manifest, err := Backup(ctx, configuration, bundle, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts.UploadPartCount != 1 || manifest.Counts.DependencyVersionCount != 1 {
		t.Fatalf("backup counts = %#v", manifest.Counts)
	}
	restored := filepath.Join(root, "restored")
	if _, err := Restore(ctx, config.Maintenance{DependencyRoot: dependencyRoot, DependencyVersions: []string{"4.2.3"}, ActiveEJSVersion: "4.2.3"}, bundle, restored); err != nil {
		t.Fatal(err)
	}
	if _, _, err := digestRegular(filepath.Join(restored, "tmp", "uploads", upload.ID, upload.Files[0].ID, "0")); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(ctx, config.Maintenance{DependencyRoot: dependencyRoot, DependencyVersions: []string{"4.2.3"}, ActiveEJSVersion: "4.2.3"}, bundle, restored); !errors.Is(
		err,
		ErrInvalidBundle,
	) {
		t.Fatalf("overwrite restore error = %v", err)
	}
}
