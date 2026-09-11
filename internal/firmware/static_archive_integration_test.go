//go:build integration

package firmware

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/firmwaremanifest"
	"retrom/internal/legacychecksum"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestStaticArchiveUploadValidatesMembersAndSupportsInspection(t *testing.T) {
	for _, test := range []struct{ name, memberName, contents, status string }{
		{"complete", "boot.rom", "Retrom owned test firmware", "MATCHED"},
		{"wrong-hash", "boot.rom", "Retrom wrong test firmware", "INVALID"},
		{"missing", "other.rom", "another file", "INVALID"},
		{"renamed", "other.rom", "Retrom owned test firmware", "INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			database, err := testsupport.OpenDatabase(ctx, filepath.Join(dir, "retrom.db"), time.Now)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { cleanup.Error("close", database.Close()) })

			contents := []byte("Retrom owned test firmware")
			_, sha := legacychecksum.Sum(contents)
			members, err := json.Marshal([]firmwaremanifest.Member{{Name: "boot.rom", SizeBytes: int64(len(contents)), CRC32: fmt.Sprintf("%08x", crc32.ChecksumIEEE(contents)), SHA1: sha, Required: true}})
			if err != nil {
				t.Fatal(err)
			}
			identity, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "same_cdi")
			if err != nil {
				t.Fatal(err)
			}
			_, err = database.SQL.ExecContext(ctx, `INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,logical_name,requirement_mode,catalog_digest,source_url,source_version,enabled,version,created_at_ms,updated_at_ms,delivery_kind,emulator_path,archive_members_json)
VALUES('fixture','same_cdi',?,?,'STATIC','fixture.zip','REQUIRED',?,'retrom:test','fixture-v1',1,1,1,1,'EXTERNAL_FILE','/same_cdi/bios/fixture.zip',?)`, identity.ProviderID, identity.TargetID, fmt.Sprintf("%064x", 1), string(members))
			if err != nil {
				t.Fatal(err)
			}
			blobs, err := blobstore.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			upload := uploads.New(database.SQL, blobs, dir, time.Now)
			var archive bytes.Buffer
			writer := zip.NewWriter(&archive)
			entry, err := writer.Create(test.memberName)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = entry.Write([]byte(test.contents)); err != nil {
				t.Fatal(err)
			}
			if err = writer.Close(); err != nil {
				t.Fatal(err)
			}
			fileID := completeFirmwareUpload(t, ctx, database.SQL, upload, "fixture.zip", archive.Bytes())
			service := New(database.SQL, time.Now).WithBlobStore(blobs)
			installed, err := service.Install(ctx, "fixture", 1, InstallRequest{UploadFileID: fileID})
			if test.status == "INVALID" {
				var invalid *ArchiveContentError
				if !errors.As(err, &invalid) || invalid.Details == nil {
					t.Fatalf("missing archive diagnostics: %v", err)
				}
				var count int
				if queryErr := database.SQL.QueryRowContext(ctx, "SELECT count(*) FROM bios_installations").Scan(&count); queryErr != nil || count != 0 {
					t.Fatalf("invalid archive was installed: %d/%v", count, queryErr)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}
			if installed.Status != test.status {
				t.Fatalf("status=%s want=%s", installed.Status, test.status)
			}
			inspection, err := service.InspectArchive(ctx, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			if inspection.LogicalName != "fixture.zip" || len(inspection.Entries) == 0 {
				t.Fatalf("inspection=%#v", inspection)
			}
		})
	}
}
