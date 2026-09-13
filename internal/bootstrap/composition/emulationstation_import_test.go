package composition

import (
	"testing"

	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

func TestEmulationStationCompositionScansAndPreparesOwnedReview(t *testing.T) {
	fixture := newESCompositionFixture(t)
	fixture.service.Start()
	fixture.service.Start()
	created, err := fixture.service.Create(fixture.ctx, application.CreateRequest{RootID: "games"}, esCompositionActor)
	if err != nil {
		t.Fatal(err)
	}
	scanned := fixture.await(t, created.ID, "AWAITING_MAPPING")
	if scanned.Counts.Games != 1 || scanned.CreatedAtMS != fixture.now.UnixMilli() || scanned.ExpiresAtMS != fixture.now.AddDate(0, 0, 7).UnixMilli() {
		t.Fatalf("creation/scan clock changed: %#v", scanned)
	}
	collections, err := fixture.service.Collections(fixture.ctx, created.ID, "", "", 10)
	if err != nil || len(collections) != 1 {
		t.Fatalf("collections=%#v error=%v", collections, err)
	}
	mapped, err := fixture.service.UpdateMappings(fixture.ctx, created.ID, scanned.Version, []application.Mapping{{CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: testsupport.MustPlatformInstanceID(t, fixture.database, "nes/fceumm"), TagIDs: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.StartImport(fixture.ctx, created.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	completed := fixture.await(t, created.ID, "COMPLETED")
	if completed.Counts.ReviewPending != 1 || completed.CompletedAtMS == nil || *completed.CompletedAtMS != fixture.now.UnixMilli() {
		t.Fatalf("completion=%#v", completed)
	}
	fixture.assertOwnedReview(t, created.ID)
	fixture.service.Close()
	fixture.service.Close()
}

func (fixture esCompositionFixture) assertOwnedReview(t *testing.T, id string) {
	t.Helper()
	var source, ordinary, kind, title, actor string
	var frozenYear, year, games int
	if err := fixture.database.QueryRowContext(fixture.ctx, `
SELECT source.execution_state,ordinary.state,ordinary.review_handoff_kind,
json_extract(draft.metadata_json,'$.title'),json_extract(draft.metadata_json,'$.releaseYear'),
plan.release_year_max,event.actor_user_id,(SELECT count(*) FROM games)
FROM emulationstation_imports plan JOIN emulationstation_import_items source ON source.import_id=plan.id
JOIN import_items ordinary ON ordinary.id=source.library_import_item_id
JOIN audit_events event ON event.resource_id=plan.id AND event.action='EMULATIONSTATION_IMPORT_STARTED'
JOIN review_drafts draft ON draft.import_item_id=ordinary.id
WHERE plan.id=?`, id).Scan(&source, &ordinary, &kind, &title, &year, &frozenYear, &actor, &games); err != nil {
		t.Fatal(err)
	}
	if source != "REVIEW_PENDING" || ordinary != "REVIEW_PENDING" || kind != "EMULATIONSTATION" || title != "Composition smoke" || year != 2020 || frozenYear != 2027 || actor != esCompositionActor || games != 0 {
		t.Fatalf("source=%s ordinary=%s kind=%s metadata=%s/%d frozen=%d actor=%s games=%d", source, ordinary, kind, title, year, frozenYear, actor, games)
	}
}
