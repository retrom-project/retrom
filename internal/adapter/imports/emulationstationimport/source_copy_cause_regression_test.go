package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"io/fs"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testkit/testsupport"
)

func TestESSourceCopyPreservesOpenFailure(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	_, err := fixture.service.copySource(
		fixture.context,
		fixture.service.roots[unit.RootID],
		unit,
		unit.RelativePath,
		"missing.nes",
		1,
		"frozen",
	)
	if !errors.Is(err, fs.ErrNotExist) || !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("source open cause=%v", err)
	}
}

func TestESSourceCopyPreservesCancellationReadFailure(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, found, err := fixture.service.nextItem(fixture.context, unit)
	if err != nil || !found {
		t.Fatalf("item=%v error=%v", found, err)
	}
	file := item.Files[0]
	cause := errors.New("execution read unavailable")
	var hits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(
		t,
		fixture.database,
		testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
			if sourceCancellationRead(query, args, unit) {
				hits.Add(1)
				return cause
			}
			return nil
		}},
	)
	fixture.service.database = faultDB
	_, err = fixture.service.copySource(
		fixture.context,
		fixture.service.roots[unit.RootID],
		unit,
		unit.RelativePath,
		file.Path,
		file.Size,
		file.Facts,
	)
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Fatalf("cancellation read cause=%v hits=%d", err, hits.Load())
	}
}

func sourceCancellationRead(query string, args []driver.NamedValue, unit work) bool {
	return strings.Contains(
		query,
		"FROM jobs job JOIN emulationstation_imports plan ON plan.id=job.scope_id",
	) && len(
		args,
	) == 1 && args[0].Value == unit.JobID
}
