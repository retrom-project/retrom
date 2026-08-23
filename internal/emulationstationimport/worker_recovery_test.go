package emulationstationimport

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestLeaseRecoveryResumesPartiallyCopiedMultiDiscItem(t *testing.T) {
	fixture := newLifecycleFixture(t)
	fixture.service.importer.WithMultiDiscImportEnabled(true)
	writeScanFile(t, fixture.source, "saturn/game.m3u", []byte("one.chd\ntwo.chd\n"))
	writeScanFile(t, fixture.source, "saturn/one.chd", append([]byte("MComprHD"), []byte("one")...))
	writeScanFile(t, fixture.source, "saturn/two.chd", append([]byte("MComprHD"), []byte("two")...))
	writeScanFile(t, fixture.source, "saturn/gamelist.xml", []byte(
		`<gameList><game><path>./game.m3u</path><name>Recovered multi-disc</name></game></gameList>`,
	))

	summary, unit := startLifecycleImport(t, fixture, "saturn", "saturn")
	item, found, err := fixture.service.nextItem(fixture.context, summary.ID)
	testassert.Falsef(t, err != nil || !found || len(item.Files) != 3,
		"claimed item = %#v, found = %v, error = %v", item, found, err)
	first := item.Files[0]
	root := fixture.service.roots[unit.RootID]
	metadata, err := fixture.service.copySource(
		fixture.context,
		root,
		unit.ImportID,
		unit.RelativePath,
		first.Path,
		first.Size,
		first.Facts,
	)
	testassert.False(t, err != nil, err)
	firstBlobID, err := fixture.service.recordCopiedFile(
		fixture.context,
		item.ID,
		first.Ordinal,
		metadata,
	)
	testassert.False(t, err != nil, err)

	now := fixture.now.UnixMilli()
	mustExecEmulationStationTest(t, fixture.database, `
UPDATE jobs SET leased_until_ms=?,heartbeat_at_ms=? WHERE id=?`, now-1, now-1, unit.JobID)
	testassert.False(t, fixture.service.recoverWork(fixture.context) != nil, "recover expired lease")

	var itemState, jobState string
	var copied, discovered, availableAt, retryEvents int64
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT
 (SELECT execution_state FROM emulationstation_import_items WHERE id=?),
 (SELECT state FROM jobs WHERE id=?),
	(SELECT available_at_ms FROM jobs WHERE id=?),
	(SELECT count(*) FROM job_events WHERE job_id=? AND event_type='RETRY_SCHEDULED'),
 count(*) FILTER(WHERE state='COPIED'),
 count(*) FILTER(WHERE state='DISCOVERED')
FROM emulationstation_import_item_files WHERE item_id=?`,
		item.ID, unit.JobID, unit.JobID, unit.JobID, item.ID).
		Scan(&itemState, &jobState, &availableAt, &retryEvents, &copied, &discovered) != nil,
		"read recovered work")
	testassert.Falsef(t,
		itemState != "COPYING" || jobState != "QUEUED" || copied != 1 || discovered != 2 ||
			availableAt != now+time.Second.Milliseconds() || retryEvents != 1,
		"recovered state = item:%s job:%s available:%d events:%d copied:%d discovered:%d",
		itemState,
		jobState,
		availableAt,
		retryEvents,
		copied,
		discovered,
	)
	testassert.False(t, os.Remove(filepath.Join(fixture.source, filepath.FromSlash(first.Path))) != nil,
		"remove already copied source")

	_, claimedEarly := fixture.service.claim(fixture.context)
	testassert.False(t, claimedEarly, "recovered work was claimable before its backoff")
	*fixture.now = fixture.now.Add(time.Second)
	resumed, claimed := fixture.service.claim(fixture.context)
	testassert.Truef(t, claimed && resumed.JobID == unit.JobID && resumed.Attempt == 2,
		"resumed work = %#v, claimed = %v", resumed, claimed)
	testassert.Falsef(t, resumed.DeadlineAtMS != unit.DeadlineAtMS,
		"deadline changed across recovery: before=%d after=%d", unit.DeadlineAtMS, resumed.DeadlineAtMS)
	fixture.service.execute(fixture.context, resumed)
	finished, err := fixture.service.Get(fixture.context, summary.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return finished.State != "COMPLETED" },
		func() bool { return finished.Counts.ReviewPending != 1 },
		func() bool { return finished.Counts.Failed != 0 },
	), "finished summary = %#v, error = %v", finished, err)

	var retainedBlobID string
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT blob_id FROM emulationstation_import_item_files WHERE item_id=? AND ordinal=?`,
		item.ID,
		first.Ordinal,
	).Scan(&retainedBlobID) != nil, "read retained copied file")
	testassert.Falsef(t, retainedBlobID != firstBlobID,
		"copied blob changed across recovery: before=%s after=%s", firstBlobID, retainedBlobID)
}

func startLifecycleImport(
	t *testing.T,
	fixture lifecycleFixture,
	importDirectory string,
	platformID string,
) (Summary, work) {
	t.Helper()
	scanned := fixture.createAndScan(t)
	collections, err := fixture.service.Collections(fixture.context, scanned.ID, "", "", 100)
	testassert.False(t, err != nil, err)
	var platformInstanceID string
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT id FROM platform_instances
WHERE platform_id=? AND enabled=1 AND deleted_at_ms IS NULL
ORDER BY sort_order,id LIMIT 1`, platformID).Scan(&platformInstanceID) != nil, "resolve platform instance")
	mappings := make([]Mapping, 0, len(collections))
	for _, collection := range collections {
		action := "SKIP"
		target := ""
		if importDirectory == "" || collection.RelativeDirectory == importDirectory {
			action = "IMPORT"
			target = platformInstanceID
		}
		mappings = append(mappings, Mapping{
			CollectionID:       collection.ID,
			Action:             action,
			PlatformInstanceID: target,
			TagIDs:             []string{},
		})
	}
	mapped, err := fixture.service.UpdateMappings(fixture.context, scanned.ID, scanned.Version, mappings)
	testassert.False(t, err != nil, err)
	started, err := fixture.service.StartImport(fixture.context, scanned.ID, mapped.Version)
	testassert.False(t, err != nil, err)
	unit, found := fixture.service.claim(fixture.context)
	testassert.Truef(t, found && unit.Kind == "SERVER_EMULATIONSTATION_IMPORT",
		"import work = %#v, found = %v", unit, found)
	return started, unit
}
