//go:build integration

package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"retrom/internal/filestore"

	dbapi "retrom/internal/database"

	reviewpersistence "retrom/internal/persistence/libraryimport"
	"retrom/internal/persistence/recordstore"
	retromruntime "retrom/internal/runtime"
	runtimecatalog "retrom/internal/runtime/catalog"
	"retrom/internal/testsupport"
)

func rpgReviewRuntimeCatalog() runtimecatalog.Catalog {
	return runtimecatalog.Catalog{SchemaVersion: 1, Bindings: []runtimecatalog.Binding{{
		ID: "retrom-runtime-rpgmaker-2000", CoreID: "rpgmaker", ProviderID: "retrom-runtime",
		TargetID: "rpgmaker-2000", PlatformIDs: []string{"rpgmaker"},
		AcceptedContentKinds: []string{"RPG_MAKER_PROJECT"}, DetectorProfile: "RPG2000",
		LaunchPolicy: "SUPPORTED",
	}}}
}

func newRPGReviewLaunchService(
	t *testing.T, ctx context.Context, database dbapi.DB, credentials *retromruntime.Credentials, now func() time.Time,
) *Service {
	t.Helper()
	builder, err := testsupport.NewRuntimeBuilder(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	return New(database, nil, credentials, now).WithRuntimeProvider(rpgReviewRuntimeCatalog(), builder)
}

type rpgReviewFixture struct {
	files                                                  *filestore.Store
	itemID, projectFileRecord, projectSHA, indexFileRecord string
}

func seedRPGReviewFixture(
	t *testing.T,
	database dbapi.DB,
	now int64,
) rpgReviewFixture {
	t.Helper()
	if err := testsupport.SeedRuntimeProviders(context.Background(), database, rpgReviewRuntimeCatalog()); err != nil {
		t.Fatal(err)
	}
	target, err := testsupport.LookupRuntimeTarget(context.Background(), database, "rpgmaker")
	if err != nil {
		t.Fatal(err)
	}
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture := rpgReviewFixture{
		files: files, itemID: "01980000-0000-7000-8000-000000000901",
		projectFileRecord: rpgFileRecord("rpg-project-a"), indexFileRecord: rpgFileRecord("rpg-index"),
	}
	record, err := filestore.ParseRecord(fixture.projectFileRecord)
	if err != nil {
		t.Fatal(err)
	}
	fixture.projectSHA = record.SHA256
	for _, name := range []string{"rpg-project-a", "rpg-project-b", "rpg-index", "rpg-checkpoint"} {
		payload := sha256.Sum256([]byte(name))
		metadata, err := files.Put(bytes.NewReader(payload[:10]))
		if err != nil {
			t.Fatal(err)
		}
		_, err = files.CopyTo(t.Context(), metadata.Record,
			filestore.ItemDirectory(fixture.itemID)+"/payload/content/fixture", name)
		if err != nil {
			t.Fatal(err)
		}
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,enabled,version,created_at_ms,updated_at_ms)
VALUES('rpg-platform','rpgmaker','rpgmaker','RPG Maker validation','rpg-validation',1,1,?,?)`, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO upload_sessions(id,purpose,state,source_type,total_files,total_bytes,manifest_digest,
 expires_at_ms,created_at_ms,updated_at_ms)
VALUES('rpg-upload','PROJECT','COMPLETE','DIRECTORY',2,20,?,?,?,?)`,
		strings.Repeat("8", 64), now+1_000_000, now, now)
	for index, file := range []struct{ id, path, blob string }{
		{"rpg-upload-a", "RPG_RT.ldb", fixture.projectFileRecord},
		{"rpg-upload-b", "Map0001.lmu", rpgFileRecord("rpg-project-b")},
	} {
		mustRPGReferenceSQL(t, database, "upload_files", `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
 final_file_record,state,created_at_ms,updated_at_ms)
VALUES(?,'rpg-upload',?,10,10,?,'COMPLETE',?,?)`, file.id, file.path, file.blob, now+int64(index), now+int64(index))
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,
 platform_id,default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,
 config_snapshot_digest,state,total_item_count,review_pending_item_count,created_at_ms,updated_at_ms)
VALUES('rpg-import','rpg-upload','rpg-platform',1,'rpgmaker','rpgmaker',?,?,
 'NONE','{}',?,'REVIEW_PENDING',1,1,?,?)`, target.ProviderID, target.TargetID,
		strings.Repeat("9", 64), now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
 search_text,created_at_ms,updated_at_ms)
VALUES(?,'rpg-import',?,'REVIEW_PENDING','{}',?,'rpg fixture',?,?)`, fixture.itemID,
		strings.Repeat("a", 64), strings.Repeat("b", 64), now, now)
	manifest := `{"schemaVersion":2,"contentKind":"RPG_MAKER_PROJECT","fileCount":2,"totalBytes":20,"filesDigest":"` +
		strings.Repeat("c", 64) + `"}`
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_source_snapshots(id,import_item_id,content_kind,
 source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES('rpg-snapshot',?,'RPG_MAKER_PROJECT',?,?,'IDENTIFICATION',?)`, fixture.itemID,
		manifest, strings.Repeat("d", 64), now)
	for index, file := range []struct{ upload, logical, blob string }{
		{"rpg-upload-a", "RPG_RT.ldb", fixture.projectFileRecord},
		{"rpg-upload-b", "Map0001.lmu", rpgFileRecord("rpg-project-b")},
	} {
		mustRPGReferenceSQL(t, database, "import_item_source_snapshot_files", `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
 file_record,sort_order,created_at_ms)
VALUES('rpg-snapshot','PROJECT_FILE',?,?,?, ?,?)`, file.logical, file.upload, file.blob, index, now)
	}
	mustRPGLaunchSQL(t, database, `
UPDATE import_items SET target_platform_instance_id='rpg-platform',metadata_json='{}',
 review_version=1,review_created_at_ms=?,review_updated_at_ms=?,
effective_source_snapshot_id='rpg-snapshot' WHERE id=?`, now, now, fixture.itemID)
	mustRPGReferenceSQL(t, database, "import_item_runtime_files", `
INSERT INTO import_item_runtime_files(import_item_id,role,logical_name,file_record,
 sort_order,created_at_ms)
VALUES(?,'RPG_EASYRPG_INDEX','index.json',?,0,?)`, fixture.itemID, fixture.indexFileRecord, now)
	mustRPGLaunchSQL(t, database, `
UPDATE import_items SET review_version=review_version+1,review_updated_at_ms=?
WHERE id=?`, now, fixture.itemID)
	projectFingerprint := strings.Repeat("c", 64)
	dependency := fmt.Sprintf("%x", sha256.Sum256([]byte(`{"externalRTP":[{"slot":0,"declaredName":"RPG2000_RTP","normalizedName":""}],"policy":"PROJECT_RESOURCES_ONLY","schemaVersion":2,"selfContainedOverride":true}`)))
	mustRPGLaunchSQL(t, database, `
UPDATE import_items SET review_profile_json=json_object('kind','RPG_MAKER_PROJECT','data',json_object(
 'generation','RPG2000','evidenceFamily','RPG2K','evidenceGeneration','RPG2000',
 'evidenceConfidence','MATCHED','engineVersion',NULL,'entryHtmlPath',NULL,
 'fileCount',2,'totalBytes',20,'projectFingerprint',?,'requirementsSha256',?,
 'analysis',json('{}'),'selfContainedOverride',1,'providerId',?,'targetId',?,
 'dependencySnapshotSha256',?)) WHERE id=?`, projectFingerprint, strings.Repeat("0", 64),
		target.ProviderID, target.TargetID, dependency, fixture.itemID)

	bindRPGFixtureValidation(t, database)
	return fixture
}

func mustRPGLaunchSQL(t *testing.T, database dbapi.DB, query string, arguments ...any) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), query, arguments...); err != nil {
		t.Fatalf("RPG launch fixture SQL: %v\n%s", err, query)
	}
}

func bindRPGFixtureValidation(t *testing.T, database dbapi.DB) {
	t.Helper()
	var itemID string
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT id FROM import_items WHERE review_version>0 LIMIT 1`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	runtime, err := reviewpersistence.ReadReviewRuntime(t.Context(), database, itemID)
	if err != nil || runtime.Status != "READY" {
		t.Fatalf("RPG current runtime=%+v error=%v", runtime, err)
	}
}

func mustRPGReferenceSQL(t *testing.T, database dbapi.DB, table, query string, arguments ...any) {
	t.Helper()
	if _, err := recordstore.InsertRows(t.Context(), database, table, query, arguments...); err != nil {
		t.Fatal(err)
	}
}

func rpgFileRecord(name string) string {
	payload := sha256.Sum256([]byte(name))
	metadata := testsupport.FileMetadata(string(payload[:10]))
	record, err := filestore.ParseRecord(metadata.Record)
	if err != nil {
		panic(err)
	}
	record.Path = "staging/items/01980000-0000-7000-8000-000000000901/payload/content/fixture/" + name
	value, err := record.Encode()
	if err != nil {
		panic(err)
	}
	return value
}
