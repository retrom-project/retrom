//go:build integration

package arcadecatalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/store"
	"retrom/internal/uploads"
)

func TestUserDATRequiresParseDiffAndExplicitActivation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencySet.Bootstrap(ctx, database.SQL, time.Now()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	contents := []byte(
		`<?xml version="1.0"?><datafile><game name="tiny-a"><description>Tiny A</description><rom name="tiny-a.bin" size="1" crc="d202ef8d" sha1="5ba93c9db0cff93f52b521d7420e43f6eda2784f"/></game><game name="tiny-b"><description>Tiny B</description><rom name="tiny-b.bin" size="1" crc="d202ef8d" sha1="5ba93c9db0cff93f52b521d7420e43f6eda2784f"/></game></datafile>`,
	)
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files: []uploads.FileDeclaration{
				{ClientFileID: "dat", RelativePath: "candidate.dat", SizeBytes: int64(len(contents))},
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), "sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := uploadService.Get(ctx, upload.ID)
	finalizeJob, _, err := uploadService.Complete(ctx, upload.ID, snapshot.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, database, finalizeJob)
	var artifactID string
	var artifactVersion int64
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id,
version
FROM core_artifacts
WHERE core_id='fbneo'
AND enabled=1
`).Scan(&artifactID, &artifactVersion); err != nil {
		t.Fatal(err)
	}
	// This test owns a deliberately tiny active catalog so that every diff row
	// is asserted exactly. Production startup publishes the real built-in DAT
	// asynchronously through BootstrapCatalogs; that lifecycle has its own
	// integration test.
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE dat_versions
SET parse_status='READY',
is_active=1,
parsed_at_ms=?,
activated_at_ms=?
WHERE core_artifact_id=?
AND source='BUILTIN'
`, time.Now().UnixMilli(), time.Now().UnixMilli(), artifactID); err != nil {
		t.Fatal(err)
	}
	var baseDATVersionID string
	if err := database.SQL.QueryRowContext(ctx, `
SELECT id
FROM dat_versions
WHERE core_artifact_id=?
AND is_active=1
`, artifactID).Scan(&baseDATVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO dat_machines(dat_version_id,
machine_name,
description,
year,
manufacturer,
cloneof,
romof,
is_explicit_bios,
classification) VALUES
(?,
'old-only',
'Old only',
'',
'',
NULL,
NULL,
0,
'NORMAL'),
(?,
'tiny-a',
'Old Tiny A',
'',
'',
NULL,
NULL,
0,
'NORMAL');
INSERT INTO dat_rom_entries(dat_version_id,
machine_name,
ordinal,
name,
size_bytes,
crc32,
sha1,
status,
merge_name,
bios_name) VALUES
(?,
'old-only',
0,
'old.bin',
1,
'd202ef8d',
NULL,
'GOOD',
NULL,
NULL),
(?,
'tiny-a',
0,
'tiny-a.bin',
2,
'41d912ff',
NULL,
'GOOD',
NULL,
NULL)
	`, baseDATVersionID, baseDATVersionID, baseDATVersionID, baseDATVersionID); err != nil {
		t.Fatal(err)
	}
	service := New(database.SQL, blobs, time.Now)
	created, err := service.Create(ctx, CreateRequest{UploadFileID: upload.Files[0].ID, CoreArtifactID: artifactID})
	if err != nil {
		t.Fatal(err)
	}
	waitJob(t, database, created.JobID)
	waitDiff(t, database, created.DATVersionID)
	var machineCount, romCount int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT (SELECT count(*)
FROM dat_machines
WHERE dat_version_id=?),
(SELECT count(*)
FROM dat_rom_entries
WHERE dat_version_id=?)
`, created.DATVersionID, created.DATVersionID).Scan(&machineCount, &romCount); err != nil ||
		machineCount != 2 ||
		romCount != 2 {
		t.Fatalf("materialized catalog = %d/%d, error=%v", machineCount, romCount, err)
	}
	var active int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT is_active
FROM dat_versions
WHERE id=?
`, created.DATVersionID).Scan(&active); err != nil ||
		active != 0 {
		t.Fatalf("candidate active = %d, error=%v", active, err)
	}
	diff, err := service.Diff(ctx, created.DATVersionID)
	if err != nil || diff.ImpactDigest == "" {
		t.Fatalf("diff = %#v, error=%v", diff, err)
	}
	machineCounts, ok := diff.Summary["machines"].(map[string]any)
	if !ok || numericInt64(machineCounts["added"]) != 1 || numericInt64(machineCounts["removed"]) != 1 || numericInt64(machineCounts["changed"]) != 1 ||
		len(diff.Items) != 3 {
		t.Fatalf("machine summary/items = %#v / %#v", diff.Summary, diff.Items)
	}
	if _, err := database.SQL.ExecContext(ctx, `
UPDATE dat_diff_snapshots
SET state='RUNNING',
summary_json=NULL,
impact_json=NULL,
impact_digest=NULL,
completed_at_ms=NULL
WHERE dat_version_id=?
`, created.DATVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Diff(ctx, created.DATVersionID); !errors.Is(err, ErrDiffNotReady) {
		t.Fatalf("pending diff GET error = %v", err)
	}
	service.ResumeDiffJobs()
	waitDiff(t, database, created.DATVersionID)
	diff, err = service.Diff(ctx, created.DATVersionID)
	if err != nil || diff.ImpactDigest == "" {
		t.Fatalf("resumed diff = %#v, error=%v", diff, err)
	}
	firstPage, err := service.Diff(
		ctx,
		created.DATVersionID,
		DiffOptions{Section: "ROM_ENTRIES", Change: "ADDED", Limit: 1},
	)
	if err != nil || len(firstPage.Items) != 1 || firstPage.HasMore || firstPage.Items[0].Key["name"] != "tiny-b.bin" {
		t.Fatalf("first ROM page = %#v, error=%v", firstPage, err)
	}
	allROMFirstPage, err := service.Diff(
		ctx,
		created.DATVersionID,
		DiffOptions{Section: "ROM_ENTRIES", Change: "ALL", Limit: 1},
	)
	if err != nil || len(allROMFirstPage.Items) != 1 || !allROMFirstPage.HasMore {
		t.Fatalf("first all-ROM page = %#v, error=%v", allROMFirstPage, err)
	}
	secondPage, err := service.Diff(
		ctx,
		created.DATVersionID,
		DiffOptions{Section: "ROM_ENTRIES", Change: "ALL", Limit: 1, After: allROMFirstPage.LastCursorKey},
	)
	if err != nil || len(secondPage.Items) != 1 || !secondPage.HasMore || secondPage.ImpactDigest != diff.ImpactDigest {
		t.Fatalf("second all-ROM page = %#v, error=%v", secondPage, err)
	}
	activated, err := service.Activate(
		ctx,
		created.DATVersionID,
		artifactVersion,
		ActivateRequest{ImpactDigest: diff.ImpactDigest, ConfirmUnknownCompatibility: true},
		false,
	)
	if err != nil || !activated.Active {
		t.Fatalf("activate = %#v, error=%v", activated, err)
	}
	var diffState string
	var materializedItems int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT s.state,
(SELECT count(*) FROM dat_diff_items i WHERE i.snapshot_id=s.id)
FROM dat_diff_snapshots s
WHERE s.dat_version_id=?
`, created.DATVersionID).Scan(&diffState, &materializedItems); err != nil {
		t.Fatal(err)
	}
	if diffState != "STALE" || materializedItems != 0 {
		t.Fatalf("activated diff snapshot = %s with %d items", diffState, materializedItems)
	}
}

func waitJob(t *testing.T, database *store.DB, jobID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err := database.SQL.QueryRow(`
SELECT state
FROM jobs
WHERE id=?
`, jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("job %s state = %s", jobID, state)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitDiff(t *testing.T, database *store.DB, datID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		err := database.SQL.QueryRow(`
SELECT state
FROM dat_diff_snapshots
WHERE dat_version_id=?
`, datID).Scan(&state)
		if err == nil && state == "READY" {
			return
		}
		if err == nil && state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("diff %s state = %s, error=%v", datID, state, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
