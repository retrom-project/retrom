//go:build integration

package libraryimport

import (
	"reflect"
	"testing"

	application "retrom/internal/model/libraryimport"
	payloadreleasemodel "retrom/internal/model/payloadrelease"
	"retrom/internal/repo/dbexec"
	payloadpersistence "retrom/internal/repo/payloadrelease"
	payloadreleaseservice "retrom/internal/service/payloadrelease"
)

func TestOwnedESSourceDuplicateReplaysAfterPayloadCleanup(t *testing.T) {
	fixture, request := ownedESSourceFixture(t)
	result := createOwnedESDuplicate(t, fixture, request)
	finishOwnedESDuplicate(t, fixture, result.Items[0].ExistingGameID)
	releaseOwnedSourceFixture(t, fixture)
	fixture.execute(t, `UPDATE jobs SET worker_id='replacement',execution_no=2 WHERE id='es-owner-work'`)
	replay, found, err := fixture.service.LookupOwnedServerSource(fixture.ctx, request.Intent)
	if err != nil || !found || len(replay.Items) != 1 {
		t.Fatalf("found=%v result=%+v err=%v", found, replay, err)
	}
	item := replay.Items[0]
	if item.ItemID != result.Items[0].ItemID || item.State != "DISCARDED" || len(item.SourceRelativePaths) != 0 ||
		!reflect.DeepEqual(item.ExistingMatches, result.Items[0].ExistingMatches) {
		t.Fatalf("released permanent identity changed: %+v", item)
	}
	repeated, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || !reflect.DeepEqual(repeated, replay) {
		t.Fatalf("repeat=%+v err=%v", repeated, err)
	}
	var count int
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT count(*) FROM import_items`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("duplicate replay created another review: %d", count)
	}
}

func finishOwnedESDuplicate(t *testing.T, fixture deduplicateFixture, gameID string) {
	t.Helper()
	tx, err := fixture.database.BeginTx(fixture.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := tx.ExecContext(fixture.ctx, `UPDATE emulationstation_import_items SET execution_state='SKIPPED_EXISTING',
 existing_game_id=?,completed_at_ms=?,version=version+1 WHERE id='es-owner-source'`, gameID, ownedSourceNow().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := payloadreleaseservice.NewScheduler(nil).TerminalSource(fixture.ctx, payloadpersistence.BindScheduling(tx), payloadreleasemodel.Scope{Type: payloadreleasemodel.ScopeEmulationStationImportItem, ID: "es-owner-source"}, ownedSourceNow().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func createOwnedESDuplicate(t *testing.T, fixture deduplicateFixture, request application.OwnedServerSourceRequest) ServerImportResult {
	t.Helper()
	original, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Approve(fixture.ctx, original.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(result.Items) != 1 || len(result.Items[0].ExistingMatches) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	return result
}
