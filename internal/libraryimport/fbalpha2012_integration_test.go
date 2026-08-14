//go:build integration && localfixtures

package libraryimport

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/launch"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestFBA2012RealDATImportVariantAndLaunchIsolation(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if _, err := database.SQL.Exec(`INSERT INTO profiles(id,display_name,created_at_ms) VALUES('local','Fixture',0)`); err != nil {
		t.Fatal(err)
	}
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(
		filepath.Join(repositoryRoot, "data"),
		[]string{"4.2.3", "4.3.0-pre"},
		"4.2.3",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.BootstrapCatalogs(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploader := uploads.New(database.SQL, blobs, dataDir, time.Now)
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	launcher := launch.New(database.SQL, dependencySet, credentials, time.Now)
	capabilities := launch.Capabilities{
		SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true,
	}

	tests := []struct {
		name, coreID, otherCoreID, machine, fixturePath, platformInstanceID string
	}{
		{
			name: "CPS1", coreID: "fbalpha2012_cps1", otherCoreID: "fbalpha2012_cps2", machine: "1941",
			fixturePath:        "data/example/local-fixtures/fbalpha2012_cps1/1941.zip",
			platformInstanceID: "01980000-0000-7000-8000-000000000027",
		},
		{
			name: "CPS2", coreID: "fbalpha2012_cps2", otherCoreID: "fbalpha2012_cps1", machine: "sgemf",
			fixturePath:        "data/example/local-fixtures/fbalpha2012_cps2/sgemf.zip",
			platformInstanceID: "01980000-0000-7000-8000-000000000028",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var artifactID, datVersionID string
			if err := database.SQL.QueryRowContext(ctx, `
SELECT artifact.id,dat.id
FROM core_artifacts artifact
JOIN dat_versions dat ON dat.core_artifact_id=artifact.id AND dat.is_active=1
WHERE artifact.core_id=? AND artifact.enabled=1
`, test.coreID).Scan(&artifactID, &datVersionID); err != nil {
				t.Fatal(err)
			}
			flatArchive := flatArcadeFixture(
				t,
				database.SQL,
				datVersionID,
				test.machine,
				filepath.Join(repositoryRoot, filepath.FromSlash(test.fixturePath)),
			)
			upload := uploadFBAFixture(t, ctx, database.SQL, uploader, test.machine+".zip", flatArchive)
			created, err := importer.Create(ctx, CreateRequest{
				UploadID: upload.uploadID, TargetPlatformInstanceID: test.platformInstanceID,
				MetadataProvider: "NONE",
			})
			if err != nil || created.ItemCount != 1 {
				t.Fatalf("import = %#v, error=%v", created, err)
			}
			itemID, draftVersion, _, validationID := reviewAttachmentInputs(t, database.SQL, created.ImportJobID)
			var validationStatus, compatibilityCode string
			if err := database.SQL.QueryRowContext(ctx, `
SELECT status,compatibility_code FROM import_item_core_validations WHERE id=?
`, validationID).Scan(&validationStatus, &compatibilityCode); err != nil ||
				validationStatus != "READY" || compatibilityCode != "READY" {
				t.Fatalf("validation = %s/%s, error=%v", validationStatus, compatibilityCode, err)
			}
			approved, err := importer.Approve(ctx, itemID, draftVersion)
			if err != nil {
				t.Fatal(err)
			}
			var variantArtifactID, variantDATID, variantStatus string
			if err := database.SQL.QueryRowContext(ctx, `
SELECT revision.core_artifact_id,revision.dat_version_id,revision.status
FROM game_variants variant
JOIN game_variant_revisions revision ON revision.id=variant.current_revision_id
WHERE variant.game_id=? AND variant.core_id=?
`, approved.GameID, test.coreID).Scan(&variantArtifactID, &variantDATID, &variantStatus); err != nil ||
				variantArtifactID != artifactID || variantDATID != datVersionID || variantStatus != "READY" {
				t.Fatalf(
					"variant = artifact:%s dat:%s status:%s, want %s/%s/READY, error=%v",
					variantArtifactID, variantDATID, variantStatus, artifactID, datVersionID, err,
				)
			}
			var biosRequirements int
			if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM bios_requirements WHERE core_artifact_id=? AND enabled=1
`, artifactID).Scan(&biosRequirements); err != nil || biosRequirements != 0 {
				t.Fatalf("BIOS requirements = %d, error=%v", biosRequirements, err)
			}
			createdLaunch, err := launcher.Create(ctx, "local", launch.CreateRequest{
				GameID: approved.GameID, CoreID: &test.coreID,
				ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities,
			})
			if err == nil && createdLaunch.Status == "VALIDATION_PENDING" {
				waitParentJob(t, database.SQL, createdLaunch.JobID, "SUCCEEDED")
				createdLaunch, err = launcher.Create(ctx, "local", launch.CreateRequest{
					GameID: approved.GameID, CoreID: &test.coreID,
					ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities,
				})
			}
			if err != nil || createdLaunch.LaunchID == "" {
				t.Fatalf("launch = %#v, error=%v", createdLaunch, err)
			}
			configuration, err := launcher.Config(ctx, createdLaunch.LaunchID, createdLaunch.Capability)
			if err != nil || configuration.Core != test.coreID || configuration.RuntimeCore != test.coreID ||
				configuration.BIOSURL != nil {
				t.Fatalf("launch config = %#v, error=%v", configuration, err)
			}

			wrongCore := test.otherCoreID
			pending, err := launcher.Create(ctx, "local", launch.CreateRequest{
				GameID: approved.GameID, CoreID: &wrongCore,
				ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities,
			})
			if err != nil || pending.Status != "VALIDATION_PENDING" || pending.JobID == "" {
				t.Fatalf("cross-core validation = %#v, error=%v", pending, err)
			}
			wrongState := waitFBAJobTerminal(t, database.SQL, pending.JobID)
			if wrongState != "FAILED" && wrongState != "SUCCEEDED" {
				t.Fatalf("cross-core validation job = %s", wrongState)
			}
			if _, err := launcher.Create(ctx, "local", launch.CreateRequest{
				GameID: approved.GameID, CoreID: &wrongCore,
				ReturnTo: "/games/" + approved.GameID, ClientCapabilities: capabilities,
			}); !errors.Is(err, launch.ErrBlocked) {
				t.Fatalf("cross-core launch error = %v, want %v", err, launch.ErrBlocked)
			}
		})
	}
}

func waitFBAJobTerminal(t *testing.T, database *sql.DB, jobID string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err := database.QueryRow(`SELECT state FROM jobs WHERE id=?`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED" {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s did not terminate; state=%s", jobID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func uploadFBAFixture(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	service *uploads.Service,
	name string,
	contents []byte,
) completedUpload {
	t.Helper()
	upload, err := service.Create(ctx, uploads.CreateRequest{
		SourceType: "FILES",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "fixture-" + name, RelativePath: name, SizeBytes: int64(len(contents)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for partNo, start := 0, int64(0); start < int64(len(contents)); partNo, start = partNo+1, start+uploads.PartSize {
		end := min(start+uploads.PartSize, int64(len(contents)))
		part := contents[start:end]
		digest := sha256.Sum256(part)
		if err := service.PutPart(
			ctx,
			upload.ID,
			upload.Files[0].ID,
			partNo,
			fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
			bytes.NewReader(part),
		); err != nil {
			t.Fatal(err)
		}
	}
	current, err := service.Get(ctx, upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitParentJob(t, database, jobID, "SUCCEEDED")
	return completedUpload{uploadID: upload.ID, fileID: upload.Files[0].ID}
}

func flatArcadeFixture(
	t *testing.T,
	database *sql.DB,
	datVersionID string,
	machine string,
	sourcePath string,
) []byte {
	t.Helper()
	source, err := zip.OpenReader(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", source.Close()) }()
	contents := make(map[string][]byte)
	for _, entry := range source.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		reader, openErr := entry.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		value, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatal(errors.Join(readErr, closeErr))
		}
		contents[entry.Name] = value
	}
	rows, err := database.Query(`
SELECT name FROM dat_rom_entries
WHERE dat_version_id=? AND machine_name=? AND status<>'NODUMP'
ORDER BY ordinal
`, datVersionID, machine)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	var result bytes.Buffer
	archive := zip.NewWriter(&result)
	count := 0
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		payload, exists := contents[name]
		if !exists {
			t.Fatalf("%s missing required DAT entry %s", sourcePath, name)
		}
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		writer, err := archive.CreateHeader(header)
		if err == nil {
			_, err = writer.Write(payload)
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count == 0 {
		t.Fatalf("%s has no required DAT entries", machine)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if result.Len() == 0 {
		t.Fatal(fmt.Errorf("empty flat archive for %s", machine))
	}
	return result.Bytes()
}

func TestFBA2012FixtureFilesExist(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	for _, path := range []string{
		"data/example/local-fixtures/fbalpha2012_cps1/1941.zip",
		"data/example/local-fixtures/fbalpha2012_cps2/sgemf.zip",
	} {
		if info, err := os.Stat(filepath.Join(repositoryRoot, filepath.FromSlash(path))); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("fixture %s is unavailable: %v", path, err)
		}
	}
}
