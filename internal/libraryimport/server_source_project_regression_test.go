//go:build integration

package libraryimport

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"retrom/internal/contentcapability"
	"retrom/internal/persistence/blobcatalog"
	"retrom/internal/testsupport"
)

func TestServerSourceProjectResultRetainsDeclaredArchivePath(t *testing.T) {
	ctx := t.Context()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	archive := rpgMakerMVArchiveWithMToolSidecar(t)
	uploadID := completeProjectUpload(t, ctx, database.SQL, blobs, dataDir, "GENERAL", archive)
	var file ServerSourceFile
	if err := database.SQL.QueryRowContext(ctx, `SELECT file.relative_path,file.final_blob_id,blob.size_bytes
FROM upload_files file JOIN blobs blob ON blob.id=file.final_blob_id WHERE file.upload_session_id=?`, uploadID).Scan(&file.RelativePath, &file.BlobID, &file.SizeBytes); err != nil {
		t.Fatal(err)
	}
	target := testsupport.MustPlatformInstanceID(t, database.SQL, "rpgmaker/rpgmaker")
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	result, err := service.CreateServerSourceOnce(ctx, "project-path-fixture", target, contentcapability.ModeStandard, []ServerSourceFile{file}, nil, "")
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("project source result=%#v error=%v", result, err)
	}
	if !reflect.DeepEqual(result.Items[0].SourceRelativePaths, []string{file.RelativePath}) {
		t.Fatalf("project archive lost primary ownership path: %#v, want %s", result.Items[0].SourceRelativePaths, file.RelativePath)
	}
}

func TestOwnedProjectArchiveBindsAndReplaysCanonicalMode(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	archive := rpgMakerMVArchiveWithMToolSidecar(t)
	metadata, err := fixture.blobs.Put(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobcatalog.EnsureRecord(fixture.ctx, fixture.database, metadata, "application/zip", ownedSourceNow().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	target := testsupport.MustPlatformInstanceID(t, fixture.database, "rpgmaker/rpgmaker")
	fixture.execute(t, `UPDATE source_import_collections SET
(target_platform_instance_id,target_platform_instance_version,target_platform_id,target_default_core_id,target_provider_id,target_id)=
(SELECT p.id,p.version,p.platform_id,p.default_core_id,b.provider_id,b.target_id FROM platform_instances p
 JOIN runtime_target_bindings b ON b.core_id=p.default_core_id WHERE p.id=? ORDER BY b.binding_id LIMIT 1)
WHERE id='owner-collection'`, target)
	fixture.execute(t, `UPDATE source_import_item_files SET relative_path='fixture.zip',blob_id=?,size_bytes=? WHERE item_id='unlinked-source'`, blobID, metadata.Size)
	request.TargetPlatformInstanceID = target
	request.Intent.PrimaryPaths = []string{"fixture.zip"}
	request.Files = []ServerSourceFile{{RelativePath: "fixture.zip", BlobID: blobID, SizeBytes: metadata.Size}}
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("owned project: %#v %v", result, err)
	}
	if !reflect.DeepEqual(result.Items[0].SourceRelativePaths, []string{"fixture.zip"}) {
		t.Fatalf("project binding lost declared archive: %#v", result.Items[0])
	}
	assertOwnedSourceBinding(t, fixture, result)
	request.ContentMode = "RPG_MAKER_PROJECT"
	replay, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(replay.Items) != 1 || replay.Items[0].ItemID != result.Items[0].ItemID {
		t.Fatalf("canonical project replay: %#v %v", replay, err)
	}
}
