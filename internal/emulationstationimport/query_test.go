package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"retrom/internal/service/tagging"
)

func TestChildListsRejectUnknownImport(t *testing.T) {
	t.Parallel()
	fixture := newLifecycleFixture(t)
	const missing = "01980000-0000-7000-8000-000000000899"
	checks := []struct {
		name string
		run  func() error
	}{
		{name: "gamelists", run: func() error {
			_, err := fixture.service.Gamelists(fixture.context, missing, "", "", 10)
			return err
		}},
		{name: "collections", run: func() error {
			_, err := fixture.service.Collections(fixture.context, missing, "", "", 10)
			return err
		}},
		{name: "items", run: func() error {
			_, err := fixture.service.Items(fixture.context, missing, "", "", "", "", "", "", 10)
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if err := check.run(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want %v", err, ErrNotFound)
			}
		})
	}
}

func TestQuerySummaryCursorUsesTimeAndID(t *testing.T) {
	fixture := newLifecycleFixture(t)
	first := fixture.createAndScan(t)
	second := fixture.createAndScan(t)
	want := []string{first.ID, second.ID}
	slices.Sort(want)
	slices.Reverse(want)
	page, err := fixture.service.List(fixture.context, "AWAITING_MAPPING", 0, "", 1)
	if err != nil || len(page) != 1 || page[0].ID != want[0] {
		t.Fatalf("first page=%#v error=%v", page, err)
	}
	next, err := fixture.service.List(fixture.context, "AWAITING_MAPPING", page[0].CreatedAtMS, page[0].ID, 1)
	if err != nil || len(next) != 1 || next[0].ID != want[1] {
		t.Fatalf("second page=%#v error=%v", next, err)
	}
	empty, err := fixture.service.List(fixture.context, "AWAITING_MAPPING", next[0].CreatedAtMS, next[0].ID, 1)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("last page=%#v error=%v", empty, err)
	}
	absent, err := fixture.service.List(fixture.context, "FAILED", 0, "", 21)
	if err != nil || absent == nil || len(absent) != 0 {
		t.Fatalf("state filter=%#v error=%v", absent, err)
	}
}

func TestQueryGamelistCursor(t *testing.T) {
	fixture, scanned := queryCollectionFixture(t)
	gamelists, err := fixture.service.Gamelists(fixture.context, scanned.ID, "", "", 1)
	if err != nil || len(gamelists) != 1 || gamelists[0].RelativePath != "a/gamelist.xml" {
		t.Fatalf("gamelist first page=%#v error=%v", gamelists, err)
	}
	next, err := fixture.service.Gamelists(fixture.context, scanned.ID, "VALID", gamelists[0].RelativePath, 101)
	if err != nil || len(next) != 1 || next[0].RelativePath != "gamelist.xml" {
		t.Fatalf("gamelist next page=%#v error=%v", next, err)
	}
	invalid, err := fixture.service.Gamelists(fixture.context, scanned.ID, "INVALID", "", 101)
	if err != nil || len(invalid) != 1 || invalid[0].RelativePath != "z/gamelist.xml" || invalid[0].IgnoredFieldNames == nil {
		t.Fatalf("gamelist filter=%#v error=%v", invalid, err)
	}
}

func TestQueryCollectionCursor(t *testing.T) {
	fixture, scanned := queryCollectionFixture(t)
	collections, err := fixture.service.Collections(fixture.context, scanned.ID, "", "", 1)
	if err != nil || len(collections) != 1 || collections[0].GamelistRelativePath != "a/gamelist.xml" {
		t.Fatalf("collection first page=%#v error=%v", collections, err)
	}
	tail, err := fixture.service.Collections(fixture.context, scanned.ID, collections[0].GamelistRelativePath, collections[0].ID, 101)
	if err != nil || len(tail) != 1 || tail[0].GamelistRelativePath != "gamelist.xml" || tail[0].TagSnapshot == nil {
		t.Fatalf("collection next page=%#v error=%v", tail, err)
	}
	empty, err := fixture.service.Collections(fixture.context, scanned.ID, tail[0].GamelistRelativePath, tail[0].ID, 101)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty collection page=%#v error=%v", empty, err)
	}
}

func TestQueryItemsFilterBeforePagination(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	mustExecEmulationStationTest(t, fixture.database, `UPDATE emulationstation_import_items SET title='FiX%ture' WHERE import_id=?`, scanned.ID)
	items, err := fixture.service.Items(fixture.context, scanned.ID, "%", "PENDING", "", "", "", "", 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("first page=%#v error=%v", items, err)
	}
	next, err := fixture.service.Items(fixture.context, scanned.ID, "fix%", "PENDING", "", "", items[0].Title, items[0].ID, 51)
	if err != nil || len(next) != 1 || next[0].ID <= items[0].ID {
		t.Fatalf("second page=%#v error=%v", next, err)
	}
	mustExecEmulationStationTest(t, fixture.database, `UPDATE emulationstation_import_items SET warnings_json='[{"code":"EMULATIONSTATION_IMAGE_MISSING","field":"cover"}]' WHERE id=?`, next[0].ID)
	filtered, err := fixture.service.Items(fixture.context, scanned.ID, "fix%", "PENDING", "EMULATIONSTATION_IMAGE_MISSING", *next[0].CollectionID, "", "", 51)
	if err != nil || len(filtered) != 1 || filtered[0].ID != next[0].ID || filtered[0].Media.Cover != "WARNING" {
		t.Fatalf("filtered=%#v error=%v", filtered, err)
	}
	empty, err := fixture.service.Items(fixture.context, scanned.ID, "", "REVIEW_PENDING", "", "", "", "", 51)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty outcome=%#v error=%v", empty, err)
	}
}

func TestQueryCollectionTagsSwitchFromLiveToFrozen(t *testing.T) {
	fixture, mapped, current := queryMappedTagFixture(t)
	renamed, err := fixture.service.tags.Rename(fixture.context, fixture.userID, current.TagID, "Live", current.Version)
	if err != nil {
		t.Fatal(err)
	}
	live, err := fixture.service.Collections(fixture.context, mapped.ID, "", "", 10)
	if err != nil || len(live) != 1 || len(live[0].TagSnapshot) != 1 || live[0].TagSnapshot[0].Name != "Live" {
		t.Fatalf("live=%#v error=%v", live, err)
	}
	if _, err := fixture.service.StartImport(fixture.context, mapped.ID, mapped.Version); err != nil {
		t.Fatal(err)
	}
	frozen, err := fixture.service.Collections(fixture.context, mapped.ID, "", "", 10)
	if err != nil || len(frozen) != 1 || len(frozen[0].TagSnapshot) != 1 {
		t.Fatalf("frozen=%#v error=%v", frozen, err)
	}
	if _, err := fixture.service.tags.Rename(fixture.context, fixture.userID, current.TagID, "After start", renamed.Version); err != nil {
		t.Fatal(err)
	}
	after, err := fixture.service.Collections(fixture.context, mapped.ID, "", "", 10)
	if err != nil || len(after) != 1 || !reflect.DeepEqual(after[0].TagSnapshot, frozen[0].TagSnapshot) {
		t.Fatalf("frozen tags drifted=%#v error=%v", after, err)
	}
}

func queryCollectionFixture(t *testing.T) (lifecycleFixture, Summary) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	writeScanFile(t, fixture.source, "a/gamelist.xml", []byte(`<gameList><game><path>missing.nes</path><name>Missing</name></game></gameList>`))
	writeScanFile(t, fixture.source, "z/gamelist.xml", []byte(`<wrong/>`))
	scanned := fixture.createAndScan(t)
	return fixture, scanned
}

func queryMappedTagFixture(t *testing.T) (lifecycleFixture, Summary, tagging.AdminItem) {
	t.Helper()
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	collections, err := fixture.service.Collections(fixture.context, scanned.ID, "", "", 10)
	if err != nil || len(collections) != 1 {
		t.Fatalf("collections=%#v error=%v", collections, err)
	}
	tag, err := fixture.service.tags.Create(fixture.context, fixture.userID, "Original")
	if err != nil {
		t.Fatal(err)
	}
	var target string
	if err := fixture.database.QueryRowContext(fixture.context, `SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY id LIMIT 1`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	mapped, err := fixture.service.UpdateMappings(fixture.context, scanned.ID, scanned.Version, []Mapping{{CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: target, TagIDs: []string{tag.TagID}}})
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.tags.Get(fixture.context, tag.TagID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, mapped, current
}

func TestQueryCancellationPreservesCause(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	ctx, cancel := context.WithCancel(fixture.context)
	cancel()
	_, getErr := fixture.service.Get(ctx, scanned.ID)
	_, listErr := fixture.service.List(ctx, "", 0, "", 21)
	_, gamelistErr := fixture.service.Gamelists(ctx, scanned.ID, "", "", 101)
	_, collectionErr := fixture.service.Collections(ctx, scanned.ID, "", "", 101)
	_, itemErr := fixture.service.Items(ctx, scanned.ID, "", "", "", "", "", "", 51)
	for _, err := range []error{getErr, listErr, gamelistErr, collectionErr, itemErr} {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("query discarded cancellation cause: %v", err)
		}
	}
	if inUse := fixture.database.Stats().InUse; inUse != 0 {
		t.Fatalf("query retained %d database connections", inUse)
	}
}
