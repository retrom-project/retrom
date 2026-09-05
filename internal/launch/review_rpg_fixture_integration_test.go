//go:build integration

package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	retromruntime "retrom/internal/runtime"
	"retrom/internal/runtimecatalog"
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
	t *testing.T, ctx context.Context, database *sql.DB, credentials *retromruntime.Credentials, now func() time.Time,
) *Service {
	t.Helper()
	builder, err := testsupport.NewRuntimeBuilder(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	return New(database, nil, credentials, now).WithRuntimeProvider(rpgReviewRuntimeCatalog(), builder)
}

type rpgReviewFixture struct {
	itemID, projectBlobID, projectSHA, indexBlobID string
}

func seedRPGReviewFixture(
	t *testing.T,
	database *sql.DB,
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
	fixture := rpgReviewFixture{
		itemID: "rpg-item", projectBlobID: "rpg-project-a",
		projectSHA: strings.Repeat("1", 64), indexBlobID: "rpg-index",
	}
	for _, blob := range []struct{ id, sha string }{
		{fixture.projectBlobID, fixture.projectSHA},
		{"rpg-project-b", strings.Repeat("2", 64)},
		{fixture.indexBlobID, strings.Repeat("3", 64)},
		{"rpg-checkpoint", strings.Repeat("4", 64)},
	} {
		mustRPGLaunchSQL(t, database, `
INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,10,?,?,?,'application/octet-stream',?)`, blob.id, blob.sha, strings.Repeat("a", 32),
			strings.Repeat("b", 40), strings.Repeat("c", 8), now)
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms)
VALUES('rpg-platform','rpgmaker','rpgmaker','RPG Maker validation','rpg-validation',999,1,1,?,?)`, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO upload_sessions(id,purpose,state,source_type,total_files,total_bytes,manifest_digest,
 expires_at_ms,created_at_ms,updated_at_ms)
VALUES('rpg-upload','PROJECT','COMPLETE','DIRECTORY',2,20,?,?,?,?)`,
		strings.Repeat("8", 64), now+1_000_000, now, now)
	for index, file := range []struct{ id, path, blob string }{
		{"rpg-upload-a", "RPG_RT.ldb", fixture.projectBlobID},
		{"rpg-upload-b", "Map0001.lmu", "rpg-project-b"},
	} {
		mustRPGLaunchSQL(t, database, `
INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,
 final_blob_id,state,created_at_ms,updated_at_ms)
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
		{"rpg-upload-a", "RPG_RT.ldb", fixture.projectBlobID},
		{"rpg-upload-b", "Map0001.lmu", "rpg-project-b"},
	} {
		mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
 blob_id,sort_order,created_at_ms)
VALUES('rpg-snapshot','PROJECT_FILE',?,?,?, ?,?)`, file.logical, file.upload, file.blob, index, now)
	}
	mustRPGLaunchSQL(t, database, `
INSERT INTO review_drafts(id,import_item_id,target_platform_instance_id,metadata_json,
 version,created_at_ms,updated_at_ms,effective_source_snapshot_id)
VALUES('01980000-0000-7000-8000-000000000901',?,'rpg-platform','{}',1,?,?,'rpg-snapshot')`, fixture.itemID, now, now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_core_validations(id,import_item_id,target_platform_instance_id,
 platform_instance_version,core_id,provider_id,target_id,
 source_manifest_digest,source_snapshot_id,prepublish_input_digest,status,compatibility_code,
 dependency_snapshot_json,created_at_ms)
VALUES('rpg-core-validation',?,'rpg-platform',1,'rpgmaker',?,?,?,
 'rpg-snapshot',?,'READY','READY','{"bindings":[],"schemaVersion":1}',?)`, fixture.itemID,
		target.ProviderID, target.TargetID,
		strings.Repeat("d", 64), strings.Repeat("e", 64), now)
	mustRPGLaunchSQL(t, database, `
INSERT INTO import_item_validation_files(import_item_core_validation_id,role,logical_name,blob_id,
 sort_order,created_at_ms)
VALUES('rpg-core-validation','RPG_EASYRPG_INDEX','index.json',?,0,?)`, fixture.indexBlobID, now)
	mustRPGLaunchSQL(t, database, `
UPDATE review_drafts SET version=version+1,updated_at_ms=?
WHERE id='01980000-0000-7000-8000-000000000901'`, now)
	projectFingerprint := strings.Repeat("c", 64)
	dependency := fmt.Sprintf("%x", sha256.Sum256([]byte(`{"bindings":[],"schemaVersion":1}`)))
	mustRPGLaunchSQL(t, database, `
INSERT INTO rpgmaker_review_profiles(
 review_draft_id,generation,evidence_family,evidence_generation,evidence_confidence,
 file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,self_contained_override,
 provider_id,target_id,dependency_snapshot_sha256,
 created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000000901','RPG2000','RPG2K','RPG2000','MATCHED',2,20,?,?,'{}',1,
 ?,?,?,?,?)`, projectFingerprint, strings.Repeat("0", 64), target.ProviderID, target.TargetID,
		dependency, now, now)

	return fixture
}

func mustRPGLaunchSQL(t *testing.T, database *sql.DB, query string, arguments ...any) {
	t.Helper()
	if _, err := database.Exec(query, arguments...); err != nil {
		t.Fatalf("RPG launch fixture SQL: %v\n%s", err, query)
	}
}
