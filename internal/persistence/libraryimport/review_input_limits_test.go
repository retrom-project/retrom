package libraryimport

import (
	"testing"

	corevalidation "retrom/internal/core/validation"
)

func TestReviewReevaluatesCurrentInputLimitWithoutDiscardingEvidence(t *testing.T) {
	db := metadataDatabase(t)
	metadataExec(t, db, `INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,received_size_bytes,final_file_record,state,created_at_ms,updated_at_ms)
VALUES('content-file','upload','game.gba',11,11,'{"size_bytes":11}','COMPLETE',1,1)`)
	metadataExec(t, db, `INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,file_record,sort_order,created_at_ms)
 VALUES('snapshot','CONTENT','game.gba','content-file','{"size_bytes":11}',0,1)`)
	metadataExec(t, db, `INSERT INTO runtime_target_input_limits(provider_id,target_id,role,max_file_bytes)
 VALUES('emulatorjs','mgba','game',10)`)
	current := ReviewRuntime{SnapshotID: "snapshot", ProviderID: "emulatorjs", TargetID: "mgba", ContentKind: "SINGLE_FILE", Status: "READY", Code: "READY"}
	read := func(current ReviewRuntime) (ReviewRuntime, error) {
		value, err := readStaticCurrent(t.Context(), db, current)
		if err != nil {
			return ReviewRuntime{}, err
		}
		return applyReviewInputRejection(t.Context(), db, value)
	}
	blocked, err := read(current)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := corevalidation.ParseSnapshot(blocked.DependencyJSON)
	if err != nil || blocked.Code != "CONTENT_FILE_BYTES_EXCEEDED" || snapshot.ContentRejection == nil || snapshot.ContentRejection.Limit.Actual != 11 || snapshot.ContentRejection.Limit.Maximum != 10 {
		t.Fatalf("blocked=%+v snapshot=%+v err=%v", blocked, snapshot, err)
	}
	metadataExec(t, db, `UPDATE runtime_target_input_limits SET max_file_bytes=11 WHERE provider_id='emulatorjs' AND target_id='mgba'`)
	ready, err := read(blocked)
	if err != nil || ready.Status != "READY" || ready.Code != "READY" {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}
	snapshot, err = corevalidation.ParseSnapshot(ready.DependencyJSON)
	if err != nil || snapshot.ContentRejection != nil {
		t.Fatalf("obsolete limit retained: %+v %v", snapshot, err)
	}
}
