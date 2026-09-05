package emulationstationimport

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/emulationstationmeta"
	"retrom/internal/libraryimport"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type lifecycleFixture struct {
	context  context.Context
	database *sql.DB
	service  *Service
	now      *time.Time
	source   string
	userID   string
}

func newLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	dataDirectory := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(dataDirectory, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	testassert.False(t, testsupport.SeedPlatformInstances(t.Context(), database.SQL) != nil, "seed platforms")
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	testassert.False(t, testsupport.SeedRuntimeProviders(t.Context(), database.SQL, dependencySet.RuntimeCatalog) != nil, "seed runtime providers")
	testassert.False(t, dependencySet.Bootstrap(t.Context(), database.SQL, time.Now()) != nil, "bootstrap dependencies")
	const userID = "01980000-0000-7000-8000-000000000841"
	mustExecEmulationStationTest(t, database.SQL, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('emulationstation-lifecycle-profile','Lifecycle',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'emulationstation-lifecycle-profile','es-lifecycle','Lifecycle','ADMIN','ENABLED',1,1)`, userID)
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{
		UserID: userID, ProfileID: "emulationstation-lifecycle-profile", Role: "ADMIN",
	})
	current := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	sourceRoot := createEmulationStationIntegrationSource(t, dataDirectory)
	blobs, err := blobstore.Open(dataDirectory)
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDirectory)
	testassert.False(t, err != nil, err)
	service := New(
		database.SQL,
		blobs,
		libraryimport.New(database.SQL, clock).WithBlobStore(blobs),
		credentials,
		[]config.ServerImportRoot{{ID: "games", Label: "Games", Path: sourceRoot, CanonicalPath: sourceRoot}},
		clock,
	)
	return lifecycleFixture{
		context:  ctx,
		database: database.SQL,
		service:  service,
		now:      &current,
		source:   sourceRoot,
		userID:   userID,
	}
}

func (fixture lifecycleFixture) createAndScan(t *testing.T) Summary {
	t.Helper()
	created, err := fixture.service.Create(
		fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID,
	)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "scan work was not claimable")
	fixture.service.execute(fixture.context, unit)
	scanned, err := fixture.service.Get(fixture.context, created.ID)
	testassert.Falsef(t, err != nil || scanned.State != "AWAITING_MAPPING", "scan = %#v, error = %v", scanned, err)
	return scanned
}

func TestExpirePlansTerminalizesItemsWithoutSchedulingPayloadRelease(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	*fixture.now = fixture.now.Add(8 * 24 * time.Hour)
	testassert.False(t, fixture.service.ExpirePlans(fixture.context) != nil, "expire plans")
	expired, err := fixture.service.Get(fixture.context, scanned.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return expired.State != "EXPIRED" },
		func() bool { return expired.Counts.Cancelled != expired.Counts.Games },
	), "expired = %#v, error = %v", expired, err)
	var cancelledItems, releaseJobs int64
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT
 (SELECT count(*) FROM emulationstation_import_items WHERE import_id=? AND execution_state='CANCELLED'),
 (SELECT count(*) FROM jobs WHERE scope_type='EMULATIONSTATION_IMPORT_ITEM'
   AND scope_id IN (SELECT id FROM emulationstation_import_items WHERE import_id=?))
`, scanned.ID, scanned.ID).Scan(&cancelledItems, &releaseJobs) != nil, "read expired plan state")
	testassert.Falsef(t, cancelledItems != expired.Counts.Games || releaseJobs != 0,
		"cancelled items = %d, release jobs = %d", cancelledItems, releaseJobs)
	testassert.False(t, fixture.service.Delete(fixture.context, scanned.ID, expired.Version) != nil, "delete expired plan")
}

func TestScanPersistsAndQueriesOversizedGamelistEvidence(t *testing.T) {
	fixture := newLifecycleFixture(t)
	oversizedPath := filepath.Join(fixture.source, "oversized", "gamelist.xml")
	if err := os.MkdirAll(filepath.Dir(oversizedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Create(oversizedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Truncate(maxGamelistBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	scanned := fixture.createAndScan(t)
	testassert.Falsef(t, scanned.Counts.InvalidGamelists != 1, "summary = %#v", scanned)
	gamelists, err := fixture.service.Gamelists(fixture.context, scanned.ID, "INVALID", "", 10)
	testassert.False(t, err != nil, err)
	if len(gamelists) != 1 || gamelists[0].RelativePath != "oversized/gamelist.xml" ||
		gamelists[0].ErrorCode == nil || *gamelists[0].ErrorCode != emulationstationmeta.ErrTooLarge.Error() {
		t.Fatalf("gamelists = %#v", gamelists)
	}
}

func TestRecoverWorkTerminalizesEveryUnfinishedItem(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	collections, err := fixture.service.Collections(fixture.context, scanned.ID, "", "", 10)
	testassert.Falsef(t, err != nil || len(collections) != 1, "collections = %#v, error = %v", collections, err)
	var platformInstanceID string
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT id FROM platform_instances WHERE platform_id='nes' AND enabled=1 ORDER BY sort_order,id LIMIT 1
`).Scan(&platformInstanceID) != nil, "resolve platform instance")
	mapped, err := fixture.service.UpdateMappings(fixture.context, scanned.ID, scanned.Version, []Mapping{{
		CollectionID: collections[0].ID, Action: "IMPORT", PlatformInstanceID: platformInstanceID,
		TagIDs: []string{},
	}})
	testassert.False(t, err != nil, err)
	_, err = fixture.service.StartImport(fixture.context, scanned.ID, mapped.Version)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found && unit.Kind == "SERVER_EMULATIONSTATION_IMPORT", "import work was not claimed")
	*fixture.now = fixture.now.Add(9 * time.Hour)
	err = fixture.service.recoverWork(fixture.context)
	testassert.False(t, err != nil, err)
	failed, err := fixture.service.Get(fixture.context, scanned.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return failed.State != "FAILED" },
		func() bool { return failed.Retryable },
		func() bool { return failed.Counts.Failed != failed.Counts.Games },
	), "failed = %#v, error = %v", failed, err)
	var failedItems int64
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT count(*) FROM emulationstation_import_items
WHERE import_id=? AND execution_state='COMMIT_FAILED' AND retryable=0
`, scanned.ID).Scan(&failedItems) != nil, "read recovered items")
	testassert.Falsef(t, failedItems != failed.Counts.Games, "failed items = %d", failedItems)
}

func TestAllInvalidScanPersistsBoundedGamelistEvidence(t *testing.T) {
	fixture := newLifecycleFixture(t)
	writeScanFile(t, fixture.source, "gamelist.xml", []byte(`<gameList><game>`))
	created, err := fixture.service.Create(
		fixture.context, CreateRequest{RootID: "games", SourceRelativePath: ""}, fixture.userID,
	)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "invalid scan work was not claimable")
	fixture.service.execute(fixture.context, unit)
	failed, err := fixture.service.Get(fixture.context, created.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return failed.State != "FAILED" },
		func() bool { return failed.LastErrorCode == nil || *failed.LastErrorCode != ErrNoValidGamelist.Error() },
		func() bool { return failed.Counts.Gamelists != 1 || failed.Counts.InvalidGamelists != 1 },
	), "failed scan = %#v, error = %v", failed, err)
	var invalidEvidence int
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT count(*) FROM emulationstation_import_gamelists
WHERE import_id=? AND relative_path='gamelist.xml' AND parse_state='INVALID'
AND error_code='EMULATIONSTATION_XML_INVALID'
`, created.ID).Scan(&invalidEvidence) != nil, "read invalid gamelist evidence")
	testassert.Falsef(t, invalidEvidence != 1, "invalid gamelist evidence = %d", invalidEvidence)
}
