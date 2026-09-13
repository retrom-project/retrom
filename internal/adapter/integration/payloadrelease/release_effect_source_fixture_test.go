package payloadrelease

import (
	"database/sql"
	"strings"
	"testing"

	"retrom/internal/repo/dbexec"
)

func seedEffectOrdinarySource(t *testing.T, database *sql.DB) string {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `
INSERT INTO import_jobs(id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,
default_core_id,provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,
total_item_count,published_item_count,created_at_ms,updated_at_ms,completed_at_ms)
SELECT 'effect-import','effect-upload',instance.id,instance.version,instance.platform_id,instance.default_core_id,
binding.provider_id,binding.target_id,'NONE','{}',?,'COMPLETED',1,1,10,10,10
FROM platform_instances instance JOIN runtime_target_bindings binding ON binding.core_id=instance.default_core_id
WHERE instance.catalog_template_key='gba/mgba' LIMIT 1;
INSERT INTO import_items(id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,
search_text,version,created_at_ms,updated_at_ms,completed_at_ms)
VALUES('effect-item','effect-import',?,'PUBLISHED','{}',?,'fixture',1,10,10,10);
INSERT INTO import_item_source_files(import_item_id,role,logical_name,upload_file_id,blob_id,sort_order,created_at_ms)
VALUES('effect-item','CONTENT','fixture.gba','effect-file','effect-blob',0,10)`, strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	id, err := ScheduleTerminalImportItem(t.Context(), tx, "effect-item", ReasonImportPublished, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ScheduleTerminalImportJob(t.Context(), tx, "effect-import", 10); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedEffectBoundPegasus(t *testing.T, database *sql.DB) {
	t.Helper()
	_, err := database.ExecContext(t.Context(), `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('effect-profile','Effect',10);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('effect-user','effect-profile','effectuser','Effect','ADMIN','ENABLED',10,10);
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES('effect-scan','PEGASUS_IMPORT','effect-plan','SERVER_PEGASUS_SCAN',?,1,'{}',1,'SUCCEEDED',1,4,10,10,10,10);
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,scan_job_id,
game_count,published_item_count,created_by_user_id,created_at_ms,updated_at_ms,completed_at_ms,expires_at_ms)
VALUES('effect-plan','effect-root','Effect','',?,'COMPLETED','effect-scan',1,1,'effect-user',10,10,10,10000);
INSERT INTO pegasus_import_items(id,import_id,metadata_relative_path,game_ordinal,source_key,title,
discovery_state,execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,
library_import_job_id,library_import_item_id,published_game_id,created_at_ms,updated_at_ms,completed_at_ms)
VALUES('effect-source','effect-plan','metadata.pegasus.txt',0,?,'Effect','READY','PUBLISHED','SINGLE_FILE','{}','{}',?,
'effect-import','effect-item','schedule-game',10,10,10);
INSERT INTO pegasus_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,blob_id,state,created_at_ms,updated_at_ms)
VALUES('effect-source',0,'FILE','fixture.gba',1,'effect-blob','COPIED',10,10);
UPDATE games SET content_source_kind='SERVER_PEGASUS_IMPORT',content_source_ref_id='effect-source' WHERE id='schedule-game'`, strings.Repeat("9", 64), strings.Repeat("8", 64), strings.Repeat("7", 64), strings.Repeat("6", 64))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := ScheduleTerminalPegasusItem(t.Context(), tx, "effect-source", 10); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
