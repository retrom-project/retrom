package emulationstationimport

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"

	"retrom/internal/emulationstationmeta"
	application "retrom/internal/service/emulationstationimport"
)

func scanDatabase(t *testing.T) (*sql.DB, application.Execution, application.ScanProjection) {
	t.Helper()
	db, _ := leaseDatabase(t, false)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO content_kinds(id) VALUES('SINGLE_FILE')`); err != nil {
		t.Fatal(err)
	}
	unit, found, err := application.NewLeases(NewLeases(db), func() time.Time { return time.UnixMilli(1000) }).Claim(t.Context())
	if err != nil || !found {
		t.Fatalf("claim=%v %v", found, err)
	}
	projection := application.ScanProjection{
		SnapshotDigest: planDigest, EstimatedBytes: 16,
		Gamelists:   []application.ScanGamelist{{Path: "gamelist.xml", Size: 128, Digest: planDigest, Facts: planDigest, State: "VALID", Document: emulationstationmeta.Document{Games: []emulationstationmeta.Game{{}}}}},
		Collections: []application.ScanCollection{{ID: "collection", GamelistPath: "gamelist.xml", DisplayName: "Collection", GameCount: 1, ExtensionSummaryJSON: "[]"}},
		Items:       []application.ScanItem{{ID: "item", CollectionID: "collection", GamelistPath: "gamelist.xml", GameOrdinal: 1, SourceKey: planDigest, Title: "Game", SourceFlagsJSON: `{"hidden":false,"adult":false,"kidGame":false}`, DiscoveryState: "READY", ContentKind: "SINGLE_FILE", MetadataJSON: `{"schemaVersion":1,"title":"Game","description":"","developer":"","publisher":"","genre":"","players":null,"releaseYear":null}`, WarningsJSON: "[]", SourceManifestJSON: `{"schemaVersion":1,"contentKind":"SINGLE_FILE","files":[{"ordinal":0,"declaredKind":"FILE","relativePath":"game.nes","sizeBytes":16,"sourceFactsDigest":"` + planDigest + `"}]}`, SourceManifestDigest: planDigest, Files: []application.ScanItemFile{{Ordinal: 0, Kind: "FILE", Path: "game.nes", Size: 16, Facts: planDigest}}, Assets: []application.ScanAsset{{Kind: "COVER", Method: "EXPLICIT_IMAGE", Path: "cover.png", State: "MISSING"}}}},
	}
	return db, unit, projection
}

func scanService(db *sql.DB) *application.ScanPublication {
	return application.NewScanPublication(NewScanPublication(db), func() time.Time { return time.UnixMilli(1001) })
}

func TestScanPublicationCommitsCompleteFrozenProjection(t *testing.T) {
	t.Parallel()
	db, unit, value := scanDatabase(t)
	input := planTable(t, db, "job_input_snapshots")
	if err := scanService(db).Publish(t.Context(), unit, value); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil || summary.State != "AWAITING_MAPPING" || summary.Counts.Games != 1 || summary.Counts.Gamelists != 1 || summary.Counts.Collections != 1 {
		t.Fatalf("summary=%#v error=%v", summary, err)
	}
	actual := readLease(t, db, unit.JobID)
	if actual.JobState != "SUCCEEDED" || actual.WorkerID != "" || actual.LeaseUntilMS != 0 || actual.ReleaseYearMax != unit.ReleaseYearMax {
		t.Fatalf("job=%#v", actual)
	}
	assertRecoveryEvent(t, db, unit.JobID, "SUCCEEDED", map[string]any{"schemaVersion": float64(1), "gamelists": float64(1), "collections": float64(1), "games": float64(1), "invalidGamelists": float64(0)})
	assertScanInputs(t, db, input)
	before := planRows(t, db)
	if err := scanService(db).Finish(t.Context(), unit, value); err == nil {
		t.Fatal("completed scan replay was accepted")
	}
	if !reflect.DeepEqual(before, planRows(t, db)) {
		t.Fatal("scan replay changed state")
	}
}

func TestRejectedScanHeadersAndCountersCommitTogether(t *testing.T) {
	t.Parallel()
	db, unit, value := scanDatabase(t)
	value.Gamelists[0].State = "INVALID"
	value.Gamelists[0].ErrorCode = "EMULATIONSTATION_INVALID_XML"
	value.Gamelists[0].Document = emulationstationmeta.Document{}
	value.InvalidGamelists = 1
	value.Collections = nil
	value.Items = nil
	if err := scanService(db).Rejected(t.Context(), unit, value); err != nil {
		t.Fatal(err)
	}
	summary, err := NewQueries(db).Get(t.Context(), unit.ImportID)
	if err != nil || summary.State != "SCANNING" || summary.Counts.Gamelists != 1 || summary.Counts.InvalidGamelists != 1 || summary.Counts.Games != 0 {
		t.Fatalf("rejected=%#v %v", summary, err)
	}
	var code string
	if err := db.QueryRowContext(t.Context(), `SELECT error_code FROM emulationstation_import_gamelists`).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "EMULATIONSTATION_INVALID_XML" {
		t.Fatalf("diagnostic=%s", code)
	}
	if err := scanService(db).Reset(t.Context(), unit); err != nil {
		t.Fatal(err)
	}
	assertClearedScan(t, db, unit, false)
}

func expandScanItems(value application.ScanProjection, count int) application.ScanProjection {
	item := value.Items[0]
	value.Items = make([]application.ScanItem, count)
	for index := range value.Items {
		value.Items[index] = item
		value.Items[index].ID = fmt.Sprintf("item-%04d", index)
		value.Items[index].GameOrdinal = int64(index + 1)
		value.Items[index].SourceKey = fmt.Sprintf("%064x", index)
	}
	value.Gamelists[0].Document.Games = make([]emulationstationmeta.Game, count)
	value.Collections[0].GameCount = int64(count)
	value.EstimatedBytes = int64(count) * 16
	return value
}

func assertScanInputs(t *testing.T, db *sql.DB, input string) {
	t.Helper()
	var ignored string
	if err := db.QueryRowContext(t.Context(), `SELECT ignored_fields_json FROM emulationstation_import_gamelists`).Scan(&ignored); err != nil {
		t.Fatal(err)
	}
	if ignored != "[]" || input != planTable(t, db, "job_input_snapshots") {
		t.Fatalf("ignored=%s input changed=%v", ignored, input != planTable(t, db, "job_input_snapshots"))
	}
}
