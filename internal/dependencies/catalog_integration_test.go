//go:build integration

package dependencies

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
)

func TestBootstrapCatalogsMaterializesPinnedDATsIdempotently(t *testing.T) {
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	set, err := Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	if err := set.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := set.BootstrapCatalogs(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	var machines int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*)
FROM dat_machines
`).Scan(&machines); err != nil {
		t.Fatal(err)
	}
	if machines != 7_980+4_727+5_257 {
		t.Fatalf("machine rows = %d", machines)
	}
	var activeDATs, succeededJobs, nonCancellableJobs, snapshots int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT
(SELECT count(*)
FROM dat_versions
WHERE source='BUILTIN'
AND parse_status='READY'
AND is_active=1),
(SELECT count(*)
FROM jobs
WHERE kind='DAT_PARSE'
AND state='SUCCEEDED'),
(SELECT count(*)
FROM jobs
WHERE kind='DAT_PARSE'
AND cancellable=0),
(SELECT count(*)
FROM job_input_snapshots s
JOIN jobs j ON j.id=s.job_id
WHERE j.kind='DAT_PARSE')
`).Scan(&activeDATs, &succeededJobs, &nonCancellableJobs, &snapshots); err != nil {
		t.Fatal(err)
	}
	if activeDATs != 3 || succeededJobs != 3 || nonCancellableJobs != 3 || snapshots != 3 {
		t.Fatalf(
			"published DAT/job/snapshot contract = %d/%d/%d/%d",
			activeDATs,
			succeededJobs,
			nonCancellableJobs,
			snapshots,
		)
	}
	var requirements int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*)
FROM bios_requirements
WHERE source_kind='DAT_MACHINE'
AND enabled=1
`).Scan(&requirements); err != nil ||
		requirements != 47 {
		t.Fatalf("active DAT requirements = %d, error=%v", requirements, err)
	}
	if err := set.BootstrapCatalogs(ctx, database.SQL, time.Now()); err != nil {
		t.Fatalf("idempotent bootstrap: %v", err)
	}
}
