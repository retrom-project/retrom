//go:build integration

package libraryimport

import (
	"bytes"
	"testing"
	"time"

	"retrom/internal/repo/blobcatalog"
	application "retrom/internal/service/libraryimport"
)

func ownedSourceFixture(t *testing.T) (deduplicateFixture, application.OwnedServerSourceRequest) {
	t.Helper()
	fixture := newDeduplicateFixture(t)
	fixture.service.now = ownedSourceNow
	metadata, err := fixture.blobs.Put(bytes.NewBufferString("owned server source bytes"))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobcatalog.EnsureRecord(fixture.ctx, fixture.database, metadata, "application/octet-stream", ownedSourceNow().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	file := ServerSourceFile{RelativePath: "games/owned.gba", BlobID: blobID, SizeBytes: metadata.Size}
	seedOwnedPegasusSource(t, fixture, file)
	request := application.OwnedServerSourceRequest{
		Intent:                   application.SourceCreationIntent{Kind: application.SourceOwnerPegasus, ImportID: "owner-plan", ItemID: "unlinked-source", JobID: "owner-work", WorkerID: "owner-worker", ExecutionNo: 1, Attempt: 1, PrimaryPaths: []string{file.RelativePath}},
		TargetPlatformInstanceID: fixture.platform, ContentMode: "STANDARD", Files: []ServerSourceFile{file}, AssignedByUserID: "owner-actor",
	}
	return fixture, request
}
func ownedSourceNow() time.Time { return time.UnixMilli(2000000000000) }

// Keep the owner source real: a future cleanup reservation must inspect its state.
func seedOwnedPegasusSource(t *testing.T, fixture deduplicateFixture, file ServerSourceFile) {
	t.Helper()
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixture.execute(t, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES('owner-profile','Owner',1)`)
	fixture.execute(t, `INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('owner-actor','owner-profile','owner-admin','Owner','ADMIN','ENABLED',1,1)`)
	fixture.execute(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES('owner-scan','PEGASUS_IMPORT','owner-plan','SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,1,1,1,1)`, digest)
	fixture.execute(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,worker_id,leased_until_ms,execution_deadline_at_ms,created_at_ms,updated_at_ms)
VALUES('owner-work','PEGASUS_IMPORT','owner-plan','SERVER_PEGASUS_IMPORT',?,1,'{}',1,'RUNNING',1,4,1,'owner-worker',2000000001000,2000000010000,1,1)`, digest)
	fixture.execute(t, `INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,scan_job_id,import_job_id,collection_count,game_count,mapped_collection_count,processable_item_count,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES('owner-plan','games','Games','Roms',?,'RUNNING','owner-scan','owner-work',1,1,1,1,'owner-actor',1,1,2000001000000)`, digest)
	fixture.execute(t, `INSERT INTO pegasus_import_collections(id,import_id,metadata_relative_path,segment_ordinal,name,game_count,mapping_action,target_platform_instance_id,target_platform_instance_version,target_platform_id,target_default_core_id,target_provider_id,target_id,created_at_ms,updated_at_ms)
SELECT 'owner-collection','owner-plan','metadata.pegasus.txt',0,'Collection',1,'IMPORT',p.id,p.version,p.platform_id,p.default_core_id,b.provider_id,b.target_id,1,1
FROM platform_instances p JOIN runtime_target_bindings b ON b.core_id=p.default_core_id WHERE p.id=?`, fixture.platform)
	fixture.execute(t, `INSERT INTO pegasus_import_items(id,import_id,collection_id,metadata_relative_path,game_ordinal,source_key,title,discovery_state,execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
VALUES('unlinked-source','owner-plan','owner-collection','metadata.pegasus.txt',0,?,'Duplicate','READY','COPYING','SINGLE_FILE','{}','{}',?,1,1)`, digest, digest)
	fixture.execute(t, `INSERT INTO pegasus_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,blob_id,role,logical_name,state,created_at_ms,updated_at_ms)
VALUES('unlinked-source',0,'FILE',?,?,?,?,'CONTENT',?,'COPIED',1,1)`, file.RelativePath, file.SizeBytes, digest, file.BlobID, file.RelativePath)
}
