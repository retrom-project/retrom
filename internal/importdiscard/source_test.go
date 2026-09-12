package importdiscard

import (
	"strings"
	"testing"

	"retrom/internal/recordstore"

	"github.com/google/uuid"

	"retrom/internal/libraryimport"
	"retrom/internal/testsupport"
)

func (f *fixture) source(t *testing.T, kind string, file libraryimport.ServerSourceFile) (string, string) {
	t.Helper()
	if kind == "EMULATIONSTATION" {
		return f.emulationStationSource(t, file)
	}
	batch, _ := uuid.NewV7()
	item, _ := uuid.NewV7()
	job, _ := uuid.NewV7()
	table, err := batchTable(kind)
	if err != nil {
		t.Fatal(err)
	}
	items := table[:len(table)-1] + "_items"
	now := f.now().UnixMilli()
	f.exec(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
cancellable,state,attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,?,?,? ,?,1,'{}',1,'SUCCEEDED',1,4,?,?,?,?)`, job.String(), kind+"_IMPORT", batch.String(), "SERVER_"+kind+"_SCAN", strings.ReplaceAll(job.String(), "-", "")+strings.ReplaceAll(job.String(), "-", ""), now, now, now, now)
	extraColumns, extraValues := "", ""
	if kind == "EMULATIONSTATION" {
		extraColumns = ",release_year_max"
		extraValues = ",2026"
	}
	f.exec(t, `INSERT INTO `+table+`(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,
state,scan_job_id,game_count,blocked_item_count,created_by_user_id,created_at_ms,updated_at_ms,completed_at_ms,expires_at_ms`+extraColumns+`)
VALUES(?,'root','Root','',?,'PARTIAL_FAILURE',?,1,1,?,?,?,?,?`+extraValues+`)`,
		batch.String(), strings.Repeat("c", 64), job.String(), adminID, now, now, now, now+86400000)
	execution, _ := uuid.NewV7()
	f.exec(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,
cancellable,state,attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_IMPORT',?,1,'{}',1,'SUCCEEDED',1,4,?,?,?,?)`,
		execution.String(), batch.String(), strings.ReplaceAll(execution.String(), "-", "")+strings.ReplaceAll(execution.String(), "-", ""), now, now, now, now)
	f.exec(t, `UPDATE pegasus_imports SET import_job_id=? WHERE id=?`, execution.String(), batch.String())
	collection := f.sourceCollection(t, batch.String())
	pathColumn := "metadata_relative_path"
	if kind == "EMULATIONSTATION" {
		pathColumn = "gamelist_relative_path"
	}
	f.exec(t, `INSERT INTO `+items+`(id,import_id,collection_id,`+pathColumn+`,game_ordinal,source_key,title,
discovery_state,execution_state,metadata_json,source_manifest_json,source_manifest_digest,error_code,
created_at_ms,updated_at_ms,completed_at_ms)
VALUES(?,?,?,'metadata',0,?,'Rejected','BLOCKED_CONTENT','BLOCKED_CONTENT','{}','{}',?, ?,?,?,?)`,
		item.String(), batch.String(), collection, strings.Repeat("d", 64), strings.Repeat("e", 64), kind+"_CONTENT_FORMAT_UNSUPPORTED", now, now, now)
	f.exec(t, `INSERT INTO `+table[:len(table)-1]+`_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,
blob_id,state,created_at_ms,updated_at_ms) VALUES(?,0,'FILE',?,?,?,'COPIED',?,?)`, item.String(), file.RelativePath, file.SizeBytes, file.BlobID, now, now)
	return batch.String(), item.String()
}

func TestDiscardSourceRejectedBeforeReviewReleasesInternalEnvelope(t *testing.T) {
	for _, kind := range []string{"PEGASUS", "EMULATIONSTATION"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			file := f.file(t, "unsupported.txt", 11)
			batch, item := f.source(t, kind, file)
			result, err := f.importer.CreateServerSourceOnce(f.ctx, "SERVER_"+kind+"_IMPORT:"+item,
				testsupport.MustPlatformInstanceID(t, f.db, "nes/fceumm"), "STANDARD", []libraryimport.ServerSourceFile{file}, nil, adminID)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != 0 {
				t.Fatal("expected rejection without review")
			}
			if _, err := f.service.Request(f.ctx, kind, batch, adminID); err != nil {
				t.Fatal(err)
			}
			f.finish(t, kind, batch)
			table, _ := batchTable(kind)
			if n := f.count(t, `SELECT count(*) FROM `+table[:len(table)-1]+`_items WHERE import_id=? AND execution_state='REVIEW_DISCARDED' AND error_code=?`, batch, kind+"_CONTENT_FORMAT_UNSUPPORTED"); n != 1 {
				t.Fatal("source error evidence or discarded outcome lost")
			}
			if n := f.count(t, `SELECT count(*) FROM upload_files WHERE final_blob_id=?`, file.BlobID); n != 0 {
				t.Fatal("internal envelope still protects rejected blob")
			}
			if n := f.count(t, `SELECT count(*) FROM review_events`); n != 0 {
				t.Fatal("fabricated review for rejected input")
			}
			restart := recordstore.UpdatePegasusImports
			if kind == "EMULATIONSTATION" {
				restart = recordstore.UpdateEmulationstationImports
			}
			if _, err := restart(f.ctx, f.db, recordstore.Update{
				Set: "state='QUEUED',completed_at_ms=NULL",
				Scope: recordstore.Scope{
					Where: "id=?",
					Args:  []any{batch},
				},
			}); err == nil {
				t.Fatal("discarded source could restart")
			}
		})
	}
}
