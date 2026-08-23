package emulationstationimport

import (
	"errors"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func TestRecoverWorkConfirmsExpiredCancellation(t *testing.T) {
	fixture := newLifecycleFixture(t)
	scanned := fixture.createAndScan(t)
	mapped := mapLifecycleCollection(t, fixture, scanned)
	queued, err := fixture.service.StartImport(fixture.context, scanned.ID, mapped.Version)
	testassert.False(t, err != nil, err)
	_, found := fixture.service.claim(fixture.context)
	testassert.True(t, found, "import work was not claimed")
	running, err := fixture.service.Get(fixture.context, queued.ID)
	testassert.False(t, err != nil, err)
	_, pending, err := fixture.service.Cancel(
		fixture.context, running.ID, running.Version, "cancel recovery test", fixture.userID,
	)
	testassert.Falsef(t, err != nil || !pending, "cancel pending = %t, error = %v", pending, err)
	*fixture.now = fixture.now.Add(61 * time.Second)
	testassert.False(t, fixture.service.recoverWork(fixture.context) != nil, "recover cancelled execution")
	cancelled, err := fixture.service.Get(fixture.context, running.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return cancelled.State != "CANCELLED" },
		func() bool { return cancelled.Counts.Cancelled != cancelled.Counts.Games },
	), "cancelled = %#v, error = %v", cancelled, err)
	var cancelledJob, activeImports int
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT
 (SELECT count(*) FROM jobs WHERE id=? AND state='CANCELLED' AND finished_at_ms IS NOT NULL),
 (SELECT count(*) FROM emulationstation_imports WHERE state IN ('QUEUED','RUNNING','CANCEL_REQUESTED'))
`, *cancelled.ImportJobID).Scan(&cancelledJob, &activeImports) != nil, "read cancellation recovery")
	testassert.Falsef(t, cancelledJob != 1 || activeImports != 0,
		"cancelled jobs = %d, active imports = %d", cancelledJob, activeImports)
}

func TestStartImportRejectsSecondActiveExecution(t *testing.T) {
	fixture := newLifecycleFixture(t)
	first := mapLifecycleCollection(t, fixture, fixture.createAndScan(t))
	second := mapLifecycleCollection(t, fixture, fixture.createAndScan(t))
	_, err := fixture.service.StartImport(fixture.context, first.ID, first.Version)
	testassert.False(t, err != nil, err)
	_, err = fixture.service.StartImport(fixture.context, second.ID, second.Version)
	testassert.Truef(t, errors.Is(err, ErrActive), "second start error = %v", err)
}

func mapLifecycleCollection(t *testing.T, fixture lifecycleFixture, scanned Summary) Summary {
	t.Helper()
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
	return mapped
}
