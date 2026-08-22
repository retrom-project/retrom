//go:build integration

package dependencies

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
	"retrom/internal/testassert"
)

func TestBootstrapCatalogsMaterializesPinnedDATsIdempotently(t *testing.T) {
	ctx := context.Background()
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	set, err := Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	database, err := store.Open(ctx, filepath.Join(t.TempDir(), "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
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
	testassert.Falsef(t, machines != 7_980+4_727+5_257+227+284, "machine rows = %d", machines)
	var activeDATs, succeededJobs, nonCancellableJobs, snapshots int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT
(SELECT count(*)
FROM dat_versions
WHERE parse_status='READY'
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
	testassert.Falsef(t, testassert.Any(func() bool { return activeDATs != 5 }, func() bool { return succeededJobs != 5 }, func() bool { return nonCancellableJobs != 5 }, func() bool { return snapshots != 5 }), "published DAT/job/snapshot contract = %d/%d/%d/%d", activeDATs, succeededJobs, nonCancellableJobs, snapshots)
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
	var expansionRequirements int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM bios_requirements
WHERE source_kind='DAT_MACHINE' AND enabled=1
AND core_id IN ('fbalpha2012_cps1','fbalpha2012_cps2')
`).Scan(&expansionRequirements); err != nil || expansionRequirements != 0 {
		t.Fatalf("FBA2012 DAT requirements = %d, error=%v", expansionRequirements, err)
	}
	if err := set.BootstrapCatalogs(ctx, database.SQL, time.Now()); err != nil {
		t.Fatalf("idempotent bootstrap: %v", err)
	}
	var artifactID, selectedDATID string
	var artifactVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT a.id,a.version,d.id
FROM core_artifacts a
JOIN dat_versions d ON d.core_artifact_id=a.id AND d.is_active=1
WHERE a.core_id='fbneo' AND a.enabled=1
`).Scan(&artifactID, &artifactVersion, &selectedDATID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE dat_versions SET is_active=0 WHERE id=?`, selectedDATID); err != nil {
		t.Fatal(err)
	}
	const supersededID = "01990000-0000-7000-8000-000000000038"
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_versions(id,core_id,core_artifact_id,builtin_relative_path,sha256,parser_version,
parse_status,is_active,machine_count,rom_entry_count,disk_entry_count,bios_set_count,
default_bios_set_count,explicit_bios_machine_count,base_dependency_target_count,unresolved_relation_count,
version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
VALUES(?,'fbneo',?,'legacy/fbneo.dat',?,'legacy-parser','READY',1,
0,0,0,0,0,0,0,0,1,1,1,1,1)
`, supersededID, artifactID, strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	selectionTime := time.Now().Add(time.Second)
	if err := set.Bootstrap(ctx, database.SQL, selectionTime); err != nil {
		t.Fatal(err)
	}
	var activeAfterSelection, supersededActive int
	var advancedVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM dat_versions WHERE core_artifact_id=? AND is_active=1),
       (SELECT is_active FROM dat_versions WHERE id=?),
       (SELECT version FROM core_artifacts WHERE id=?)
`, artifactID, supersededID, artifactID).Scan(&activeAfterSelection, &supersededActive, &advancedVersion); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return activeAfterSelection != 0 }, func() bool { return supersededActive != 0 }, func() bool { return advancedVersion != artifactVersion+1 }), "manifest selection = active:%d superseded:%d artifactVersion:%d", activeAfterSelection, supersededActive, advancedVersion)
	if err := set.BootstrapCatalogs(ctx, database.SQL, selectionTime); err != nil {
		t.Fatal(err)
	}
	var selectedActive, selectedRequirements int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT is_active FROM dat_versions WHERE id=?),
       (SELECT count(*) FROM bios_requirements WHERE core_artifact_id=? AND source_kind='DAT_MACHINE' AND enabled=1 AND source_version=?)
`, selectedDATID, artifactID, selectedDATID).Scan(&selectedActive, &selectedRequirements); err != nil {
		t.Fatal(err)
	}
	testassert.Falsef(t, testassert.Any(func() bool { return selectedActive != 1 }, func() bool { return selectedRequirements == 0 }), "selected built-in DAT = active:%d requirements:%d", selectedActive, selectedRequirements)
}
