//go:build integration

package libraryimport

import (
	"testing"
	"time"

	application "retrom/internal/service/payloadrelease"

	"retrom/internal/dbexec"
	"retrom/internal/payloadrelease"
)

func TestEmulationStationDuplicateBindingReleasesSharedImportPayload(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	original, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	seedESDuplicateSource(t, fixture)
	result, err := fixture.service.CreateServerSourceOnce(fixture.ctx, "SERVER_EMULATIONSTATION_IMPORT:es-source", fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil || len(result.Items) != 1 || result.Items[0].ExistingGameID == "" {
		t.Fatalf("ES duplicate: %#v %v", result, err)
	}
	finishESDuplicateSource(t, fixture, result)
	releaseOwnedSourceFixture(t, fixture)
	var state string
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT payload_state FROM emulationstation_import_items WHERE id='es-source'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "RELEASED" {
		t.Fatalf("ES duplicate payload state=%s", state)
	}
}

func seedESDuplicateSource(t *testing.T, fixture deduplicateFixture) {
	t.Helper()
	fixture.execute(t, `INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,attempt_count,max_attempts,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
SELECT 'es-scan','EMULATIONSTATION_IMPORT','es-plan','SERVER_EMULATIONSTATION_SCAN',dedupe_key,1,'{}',1,'SUCCEEDED',1,4,1,1,1,1 FROM jobs WHERE id='owner-scan'`)
	fixture.execute(t, `INSERT INTO emulationstation_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,release_year_max,state,scan_job_id,collection_count,game_count,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
SELECT 'es-plan',root_id,root_label_snapshot,source_relative_path,root_config_digest,2034,'AWAITING_MAPPING','es-scan',1,1,created_by_user_id,1,1,expires_at_ms FROM pegasus_imports WHERE id='owner-plan'`)
	fixture.execute(t, `INSERT INTO emulationstation_import_collections(id,import_id,gamelist_relative_path,relative_directory,display_name,game_count,created_at_ms,updated_at_ms)
VALUES('es-collection','es-plan','gamelist.xml','games','Games',1,1,1)`)
	fixture.execute(t, `INSERT INTO emulationstation_import_items(id,import_id,collection_id,gamelist_relative_path,game_ordinal,source_key,title,source_flags_json,discovery_state,execution_state,content_kind,metadata_json,source_manifest_json,source_manifest_digest,created_at_ms,updated_at_ms)
SELECT 'es-source','es-plan','es-collection','gamelist.xml',1,source_key,title,'{}','READY','COPYING',content_kind,'{}',source_manifest_json,source_manifest_digest,1,1 FROM pegasus_import_items WHERE id='unlinked-source'`)
	fixture.execute(t, `INSERT INTO emulationstation_import_item_files(item_id,ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,blob_id,state,created_at_ms,updated_at_ms)
SELECT 'es-source',ordinal,declared_kind,relative_path,size_bytes,source_facts_digest,blob_id,state,created_at_ms,updated_at_ms FROM pegasus_import_item_files WHERE item_id='unlinked-source'`)
}

func finishESDuplicateSource(t *testing.T, fixture deduplicateFixture, result ServerImportResult) {
	t.Helper()
	tx, err := fixture.database.BeginTx(fixture.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := tx.ExecContext(fixture.ctx, `UPDATE emulationstation_import_items SET execution_state='SKIPPED_EXISTING',library_import_job_id=?,library_import_item_id=?,existing_game_id=?,completed_at_ms=?,version=version+1 WHERE id='es-source'`, result.Created.ImportJobID, result.Items[0].ItemID, result.Items[0].ExistingGameID, ownedSourceNow().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := payloadrelease.ScheduleTerminalEmulationStationItem(fixture.ctx, tx, "es-source", ownedSourceNow().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedDuplicatePayloadReleaseRequiresRecordedGameMatch(t *testing.T) {
	for _, kind := range []string{"PEGASUS", "EMULATIONSTATION"} {
		t.Run(kind, func(t *testing.T) {
			fixture, itemID := finishedDuplicateSourceFixture(t, kind)
			fixture.execute(t, `DELETE FROM import_item_duplicate_matches WHERE import_item_id=?`, itemID)
			releases, err := payloadrelease.New(fixture.database, fixture.blobs, ownedSourceNow, 24*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			for range 16 {
				worked, err := releases.RunOnce(fixture.ctx)
				if err != nil {
					if application.WorkErrorCode(err) != "PAYLOAD_RELEASE_SOURCE_NOT_TERMINAL" {
						t.Fatal(err)
					}
					assertDuplicatePayloadRetained(t, fixture, itemID)
					return
				}
				if !worked {
					break
				}
			}
			t.Fatal("release accepted unproved duplicate identity")
		})
	}
}

func finishedDuplicateSourceFixture(t *testing.T, kind string) (deduplicateFixture, string) {
	t.Helper()
	fixture, request := ownedSourceFixture(t)
	original, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	if kind == "PEGASUS" {
		result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		finishOwnedDuplicateFixture(t, fixture, result.Items[0].ExistingGameID)
		return fixture, result.Items[0].ItemID
	}
	seedESDuplicateSource(t, fixture)
	result, err := fixture.service.CreateServerSourceOnce(fixture.ctx, "SERVER_EMULATIONSTATION_IMPORT:es-source", fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	finishESDuplicateSource(t, fixture, result)
	return fixture, result.Items[0].ItemID
}

func assertDuplicatePayloadRetained(t *testing.T, fixture deduplicateFixture, itemID string) {
	t.Helper()
	var count int
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT count(*) FROM import_item_source_files WHERE import_item_id=?`, itemID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("failed release removed source files: %d", count)
	}
}
