//go:build integration

package libraryimport

import (
	"context"
	"testing"
	"time"

	"retrom/internal/contentcapability"
	"retrom/internal/testsupport"
)

func TestServerRPGArchiveHandoffReplaysCanonicalImport(t *testing.T) {
	ctx := context.Background()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	archive := rpgMakerMVArchiveWithMToolSidecar(t)
	uploadID := completeProjectUpload(t, ctx, database.SQL, blobs, dataDir, "GENERAL", archive)
	var file ServerSourceFile
	if err := database.SQL.QueryRowContext(ctx, `
SELECT file.relative_path,file.final_blob_id,blob.size_bytes
FROM upload_files file JOIN blobs blob ON blob.id=file.final_blob_id WHERE file.upload_session_id=?
`, uploadID).Scan(&file.RelativePath, &file.BlobID, &file.SizeBytes); err != nil {
		t.Fatal(err)
	}
	targetID := testsupport.MustPlatformInstanceID(t, database.SQL, "rpgmaker/rpgmaker")
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	first, err := service.CreateServerSourceOnce(ctx, "fixture-source-item", targetID,
		contentcapability.ModeStandard, []ServerSourceFile{file}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"", contentcapability.ModeStandard, contentcapability.ModeRPGMakerProject} {
		replayed, replayErr := service.CreateServerSourceOnce(ctx, "fixture-source-item", targetID,
			mode, []ServerSourceFile{file}, nil, "")
		if replayErr != nil {
			t.Fatalf("replay %q: %v", mode, replayErr)
		}
		if replayed.Created.ImportJobID != first.Created.ImportJobID {
			t.Fatalf("replay created another import: %s", replayed.Created.ImportJobID)
		}
	}
	var count int
	var mode string
	if err := database.SQL.QueryRowContext(ctx,
		"SELECT count(*),json_extract(config_snapshot_json,'$.contentMode') FROM import_jobs",
	).Scan(&count, &mode); err != nil {
		t.Fatal(err)
	}
	if count != 1 || mode != contentcapability.ModeRPGMakerProject {
		t.Fatalf("canonical import count/mode: %d/%s", count, mode)
	}
	direct, err := service.CreateServerSource(ctx, targetID, contentcapability.ModeStandard,
		[]ServerSourceFile{file}, nil, "")
	if err != nil || len(direct.Items) != 1 || direct.Items[0].ContentKind != contentcapability.ModeRPGMakerProject {
		t.Fatalf("direct server archive handoff: %#v, %v", direct, err)
	}
	file.RelativePath = "different.zip"
	if _, err := service.CreateServerSourceOnce(ctx, "fixture-source-item", targetID,
		contentcapability.ModeStandard, []ServerSourceFile{file}, nil, ""); err == nil {
		t.Fatal("changed source reused an existing handoff")
	}
}
