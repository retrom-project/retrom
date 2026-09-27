//go:build integration

package libraryimport

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"retrom/internal/filestore"

	contentcapability "retrom/internal/content/capability"
	dbapi "retrom/internal/database"
	"retrom/internal/testsupport"
)

func TestServerSourceProjectResultRetainsDeclaredArchivePath(t *testing.T) {
	ctx := t.Context()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	archive := rpgMakerMVArchiveWithMToolSidecar(t)
	uploadID := completeProjectUpload(t, ctx, database.SQL, blobs, dataDir, "GENERAL", archive)
	var file ServerSourceFile
	if err := dbapi.QueryRowContext(ctx, database.SQL, `SELECT file.relative_path,file.final_file_record,json_extract(blob.value, '$.size_bytes')
FROM upload_files file JOIN json_each(json_array(file.final_file_record)) blob ON blob.value IS NOT NULL
WHERE file.upload_session_id=?`, uploadID).Scan(&file.RelativePath, &file.FileRecord, &file.SizeBytes); err != nil {
		t.Fatal(err)
	}
	target := testsupport.MustPlatformInstanceID(t, database.SQL, "rpgmaker/rpgmaker")
	service := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: time.Now})
	result, err := service.CreateServerSourceOnce(ctx, "project-path-fixture", target,
		contentcapability.ModeStandard, []ServerSourceFile{file}, nil, "")
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("project source result=%#v error=%v", result, err)
	}
	if !reflect.DeepEqual(result.Items[0].SourceRelativePaths, []string{file.RelativePath}) {
		t.Fatalf("project archive lost primary ownership path: %#v, want %s",
			result.Items[0].SourceRelativePaths, file.RelativePath)
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
	fileRecord, err := filestore.FileRecord(metadata, "application/zip")
	if err != nil {
		t.Fatal(err)
	}
	target := testsupport.MustPlatformInstanceID(t, fixture.database, "rpgmaker/rpgmaker")
	fixture.execute(t, `UPDATE source_import_collections SET
(target_platform_instance_id,target_platform_instance_version,target_platform_id,target_default_core_id,
target_provider_id,target_id)=
(SELECT p.id,p.version,p.platform_id,p.default_core_id,b.provider_id,b.target_id FROM platform_instances p
 JOIN runtime_target_bindings b ON b.core_id=p.default_core_id WHERE p.id=? ORDER BY b.binding_id LIMIT 1)
WHERE id='owner-collection'`, target)
	fixture.execute(t, `UPDATE source_import_item_files SET relative_path='fixture.zip',file_record=?,size_bytes=? WHERE
item_id='018fbe68-0000-7000-8000-000000000021'`, fileRecord, metadata.Size)
	request.TargetPlatformInstanceID = target
	request.Intent.PrimaryPaths = []string{"fixture.zip"}
	request.Files = []ServerSourceFile{{RelativePath: "fixture.zip", FileRecord: fileRecord, SizeBytes: metadata.Size}}
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
