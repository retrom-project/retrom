package emulationstationimport

import (
	"errors"
	"testing"

	library "retrom/internal/model/libraryimport"
)

func TestESReviewPreparationRejectsFrozenTargetDrift(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, unit)
	if err != nil || !found {
		t.Fatalf("item=%v error=%v", found, err)
	}
	if !fixture.service.copyExecutionFiles(fixture.context, unit, fixture.service.roots[unit.RootID], &item) {
		t.Fatal("copy source")
	}
	if _, err := fixture.database.ExecContext(
		fixture.context,
		`UPDATE platform_instances SET version=version+1 WHERE id=?`,
		item.TargetPlatformID,
	); err != nil {
		t.Fatal(err)
	}
	err = fixture.service.prepareReviewItem(fixture.context, unit, fixture.service.roots[unit.RootID], item)
	var count int
	if readErr := fixture.database.QueryRowContext(fixture.context, `SELECT count(*) FROM import_jobs`).Scan(
		&count,
	); readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(err, library.ErrVersionConflict) || count != 0 {
		t.Fatalf("changed frozen target created ordinary review: error=%v jobs=%d", err, count)
	}
}
