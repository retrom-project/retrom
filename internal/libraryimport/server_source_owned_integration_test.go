//go:build integration

package libraryimport

import (
	"errors"
	"reflect"
	"testing"
	"time"

	payloadcomposition "retrom/internal/composition/payloadrelease"

	"retrom/internal/dbexec"
	payloadpersistence "retrom/internal/persistence/payloadrelease"
	application "retrom/internal/service/libraryimport"
	payloadservice "retrom/internal/service/payloadrelease"
)

func TestOwnedServerSourceCommitsUniquePrimaryAndPermanentBinding(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("owned create: %#v %v", result, err)
	}
	assertOwnedSourceBinding(t, fixture, result)
	fixture.execute(t, `UPDATE jobs SET worker_id='next-worker',execution_no=2 WHERE id='owner-work'`)
	replay, found, err := fixture.service.LookupOwnedServerSource(fixture.ctx, request.Intent)
	if err != nil || !found || !reflect.DeepEqual(replay, result) {
		t.Fatalf("read-only old worker replay: %#v found=%v err=%v", replay, found, err)
	}
	count := ownedImportCount(t, fixture)
	if count != 1 {
		t.Fatalf("source created %d imports", count)
	}
	changed := request
	changed.TargetPlatformInstanceID = "other"
	if result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, changed); !errors.Is(err, ErrInvalid) || result.Created.ImportJobID != "" {
		t.Fatalf("changed target replay: %#v %v", result, err)
	}
}

func assertOwnedSourceBinding(t *testing.T, fixture deduplicateFixture, result ServerImportResult) {
	t.Helper()
	var state, jobID, itemID string
	var version int64
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT execution_state,library_import_job_id,library_import_item_id,version FROM source_import_items WHERE id='unlinked-source'`).Scan(&state, &jobID, &itemID, &version); err != nil {
		t.Fatal(err)
	}
	if state != "VALIDATING" || jobID != result.Created.ImportJobID || itemID != result.Items[0].ItemID || version != 2 {
		t.Fatalf("binding state=%s job=%s item=%s version=%d", state, jobID, itemID, version)
	}
}

func ownedImportCount(t *testing.T, fixture deduplicateFixture) int {
	t.Helper()
	var count int
	if err := fixture.database.QueryRowContext(fixture.ctx, `SELECT count(*) FROM import_jobs imported JOIN server_import_upload_owners owner ON owner.upload_session_id=imported.upload_session_id WHERE owner.kind='SOURCE' AND owner.source_item_id='unlinked-source'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestOwnedServerSourceRejectsAdditionalIndependentReviewGroups(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	extra := request.Files[0]
	extra.RelativePath = "games/extra.gba"
	request.Files = append(request.Files, extra)
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, application.ErrSourceGrouping) || result.Created.ImportJobID != "" || ownedImportCount(t, fixture) != 0 {
		t.Fatalf("unowned review committed: %#v %v", result, err)
	}
}

func TestOwnedServerSourceRejectsWorkerWithoutCreatingImport(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	request.Intent.WorkerID = "expired-worker"
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if !errors.Is(err, ErrVersionConflict) || result.Created.ImportJobID != "" || ownedImportCount(t, fixture) != 0 {
		t.Fatalf("stale worker committed: %#v %v", result, err)
	}
}

func TestOwnedDuplicateReplaysByBindingAfterPayloadCleanup(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	published, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.Approve(fixture.ctx, published.Items[0].ItemID, 1); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(result.Items) != 1 || len(result.Items[0].ExistingMatches) != 1 {
		t.Fatalf("owned duplicate: %#v %v", result, err)
	}
	assertOwnedSourceBinding(t, fixture, result)
	finishOwnedDuplicateFixture(t, fixture, result.Items[0].ExistingGameID)
	releaseOwnedSourceFixture(t, fixture)
	replay, found, err := fixture.service.LookupOwnedServerSource(fixture.ctx, request.Intent)
	if err != nil || !found || len(replay.Items) != 1 {
		t.Fatalf("released replay: %#v found=%v err=%v", replay, found, err)
	}
	if replay.Items[0].ItemID != result.Items[0].ItemID || replay.Items[0].State != "DISCARDED" || !reflect.DeepEqual(replay.Items[0].ExistingMatches, result.Items[0].ExistingMatches) {
		t.Fatalf("released duplicate identity changed: %#v", replay)
	}
	if len(replay.Items[0].SourceRelativePaths) != 0 {
		t.Fatalf("fixture did not release original paths: %#v", replay.Items[0].SourceRelativePaths)
	}
	repeated, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(repeated.Items) != 1 || repeated.Items[0].ItemID != result.Items[0].ItemID || ownedImportCount(t, fixture) != 1 {
		t.Fatalf("released create replay: %#v %v", repeated, err)
	}
}

func releaseOwnedSourceFixture(t *testing.T, fixture deduplicateFixture) {
	t.Helper()
	releases, err := payloadcomposition.New(fixture.ctx, fixture.database, fixture.blobs, ownedSourceNow, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
	for range 16 {
		worked, err := releases.RunOnce(fixture.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			return
		}
	}
	t.Fatal("release fixture did not drain")
}

func TestOwnedSourceRejectsExistingUnboundLegacyCreation(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	_, err := fixture.service.CreateServerSourceOnce(fixture.ctx, "IMPORT_RECEIVE:unlinked-source", fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	result, found, err := fixture.service.LookupOwnedServerSource(fixture.ctx, application.SourceCreationIntent{Kind: application.SourceOwnerSource, ImportID: request.Intent.ImportID, ItemID: request.Intent.ItemID})
	if !errors.Is(err, ErrVersionConflict) || found || result.Created.ImportJobID != "" {
		t.Fatalf("guessed legacy ownership: %#v found=%v err=%v", result, found, err)
	}
}

func finishOwnedDuplicateFixture(t *testing.T, fixture deduplicateFixture, gameID string) {
	t.Helper()
	tx, err := fixture.database.BeginTx(fixture.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	if _, err := tx.ExecContext(fixture.ctx, `UPDATE source_import_items SET execution_state='SKIPPED_EXISTING',existing_game_id=?,completed_at_ms=?,version=version+1 WHERE id='unlinked-source'`, gameID, ownedSourceNow().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if _, err := payloadservice.NewScheduler(nil).TerminalSource(fixture.ctx, payloadpersistence.BindScheduling(tx), payloadservice.Scope{Type: payloadservice.ScopeSourceImportItem, ID: "unlinked-source"}, ownedSourceNow().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
