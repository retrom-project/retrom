package emulationstationimport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"retrom/internal/testassert"
)

func TestContextReaderChecksCancellationEveryEightMiB(t *testing.T) {
	const interval = 8 << 20
	checks := 0
	reader := &contextReader{
		ctx:    context.Background(),
		reader: bytes.NewReader(make([]byte, interval+1)),
		cancelled: func() bool {
			checks++
			return checks == 2
		},
	}
	copied, err := io.Copy(io.Discard, reader)
	testassert.Truef(t, errors.Is(err, errImportCancelled), "copy error = %v", err)
	testassert.Falsef(t, copied != interval || checks != 2,
		"copied = %d, cancellation checks = %d", copied, checks)
}

func TestCloseCancelledRefreshesExistingFailureCounts(t *testing.T) {
	fixture := newLifecycleFixture(t)
	started, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, started.ID)
	testassert.Falsef(t, err != nil || !found, "item = %#v, found = %v, error = %v", item, found, err)
	fixture.service.closeItem(
		fixture.context,
		item.ID,
		"SOURCE_CHANGED",
		"EMULATIONSTATION_SOURCE_CHANGED",
		false,
		"",
	)
	current, err := fixture.service.Get(fixture.context, started.ID)
	testassert.False(t, err != nil, err)
	_, pending, err := fixture.service.Cancel(
		fixture.context,
		started.ID,
		current.Version,
		"stop after source failure",
		fixture.userID,
	)
	testassert.Truef(t, err == nil && pending, "cancel pending = %v, error = %v", pending, err)

	closed, err := fixture.service.closeCancelled(fixture.context, unit)
	testassert.Truef(t, err == nil && closed, "closed = %v, error = %v", closed, err)
	cancelled, err := fixture.service.Get(fixture.context, started.ID)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return cancelled.State != "CANCELLED" },
		func() bool { return cancelled.Counts.Failed != 1 },
		func() bool { return cancelled.Counts.Cancelled != 1 },
	), "cancelled summary = %#v, error = %v", cancelled, err)
}

func TestQueuedCancellationRefreshesSkippedMappingCounts(t *testing.T) {
	fixture := newLifecycleFixture(t)
	writeScanFile(t, fixture.source, "extra/extra.nes", []byte("extra fixture"))
	writeScanFile(t, fixture.source, "extra/gamelist.xml", []byte(
		`<gameList><game><path>./extra.nes</path><name>Extra fixture</name></game></gameList>`,
	))
	scanned := fixture.createAndScan(t)
	collections, err := fixture.service.Collections(fixture.context, scanned.ID, "", "", 100)
	testassert.False(t, err != nil, err)
	var platformInstanceID string
	testassert.False(t, fixture.database.QueryRowContext(fixture.context, `
SELECT id FROM platform_instances
WHERE platform_id='nes' AND enabled=1 AND deleted_at_ms IS NULL
ORDER BY sort_order,id LIMIT 1`).Scan(&platformInstanceID) != nil, "resolve NES platform instance")
	mappings := make([]Mapping, 0, len(collections))
	for _, collection := range collections {
		mapping := Mapping{CollectionID: collection.ID, Action: "SKIP", TagIDs: []string{}}
		if collection.RelativeDirectory == "extra" {
			mapping.Action = "IMPORT"
			mapping.PlatformInstanceID = platformInstanceID
		}
		mappings = append(mappings, mapping)
	}
	mapped, err := fixture.service.UpdateMappings(fixture.context, scanned.ID, scanned.Version, mappings)
	testassert.False(t, err != nil, err)
	queued, err := fixture.service.StartImport(fixture.context, scanned.ID, mapped.Version)
	testassert.False(t, err != nil, err)

	cancelled, pending, err := fixture.service.Cancel(
		fixture.context,
		queued.ID,
		queued.Version,
		"cancel before copy",
		fixture.userID,
	)
	testassert.Falsef(t, testassert.Any(
		func() bool { return err != nil },
		func() bool { return pending },
		func() bool { return cancelled.State != "CANCELLED" },
		func() bool { return cancelled.Counts.SkippedMapping != 2 },
		func() bool { return cancelled.Counts.Cancelled != 1 },
	), "cancelled summary = %#v, pending = %v, error = %v", cancelled, pending, err)
}
