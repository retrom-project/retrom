//go:build integration

package libraryimport

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"retrom/internal/cleanup"
	variantcomposition "retrom/internal/composition/gamevariant"
	launchcomposition "retrom/internal/composition/launch"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
	"retrom/internal/launch"
	uploadpersistence "retrom/internal/persistence/uploads"
	retromruntime "retrom/internal/runtime"
	launchservice "retrom/internal/service/launch"
	"retrom/internal/service/uploads"
	"retrom/internal/testsupport"
	"retrom/internal/testsupport/workflowfixture"
)

func TestPublishedArcadeAlternateCoreUsesOwnDATWithoutCreatingImportWork(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	now := func() time.Time { return time.UnixMilli(1786000000000) }
	dir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dir, "retrom.db"), now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close alternate Arcade", database.Close()) })
	blobs, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	insertArcadeParentCatalog(t, database.SQL)
	insertAlternateArcadeDAT(t, database.SQL, "mame2003", "alternate-matched", false)
	insertAlternateArcadeDAT(t, database.SQL, "mame2003_plus", "alternate-mismatch", true)
	uploader := uploads.New(uploadpersistence.New(database.SQL), blobs, dir, now)
	uploaded := uploadCompleteFile(t, ctx, database.SQL, uploader, "c.zip", arcadeZIP(t, "c.bin", []byte("root")))
	importer := newTestImporter(t, database.SQL, blobs, testImportOptions{Now: now})
	imported, err := importer.Create(ctx, CreateRequest{UploadID: uploaded.uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "arcade/fbneo"), MetadataProvider: "NONE"})
	if err != nil {
		t.Fatal(err)
	}
	item, version, _ := reviewAttachmentInputs(t, database.SQL, imported.ImportJobID)
	approved, err := importer.Approve(ctx, item, version)
	if err != nil {
		t.Fatal(err)
	}
	workflowfixture.DeleteFinishedProcess(t, database.SQL, blobs, item)
	if _, err := database.SQL.ExecContext(ctx, `INSERT INTO profiles(id,display_name,created_at_ms)VALUES('alternate-player','Alternate',?)`, now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dir)
	if err != nil {
		t.Fatal(err)
	}
	builder, err := testsupport.NewRuntimeBuilder(ctx, database.SQL)
	if err != nil {
		t.Fatal(err)
	}
	source := launch.NewSources(blobs, credentials).WithRuntimeProvider(builder)
	variants := variantcomposition.New(database.SQL, source, now, blobs)
	t.Cleanup(variants.Close)
	launcher := launchcomposition.New(database.SQL, source, "", now, variants.Dispatch, variants.PrepareArcade)
	core := "mame2003"
	request := launchservice.CreateRequest{GameID: approved.GameID, CoreID: &core, ReturnTo: "/games/" + approved.GameID, ClientCapabilities: launch.Capabilities{SecureContext: true, CrossOriginIsolated: true, SharedArrayBuffer: true}}
	pending, err := launcher.Create(ctx, "alternate-player", request)
	if err != nil || pending.JobID == "" {
		t.Fatalf("first alternate launch: %+v %v", pending, err)
	}
	awaitAlternateArcadeJob(t, database.SQL, pending.JobID)
	ready, err := launcher.Create(ctx, "alternate-player", request)
	if err != nil || ready.LaunchID == "" {
		t.Fatalf("validated alternate launch: %+v %v", ready, err)
	}
	var dat, snapshot string
	err = dbapi.QueryRowContext(ctx, database.SQL, `SELECT dat_version_id,dependency_snapshot_json FROM game_variants WHERE game_id=? AND core_id='mame2003'`, approved.GameID).Scan(&dat, &snapshot)
	if err != nil || dat != "alternate-matched" || !strings.Contains(snapshot, `"datVersionId":"alternate-matched"`) {
		t.Fatalf("alternate evidence: DAT=%s snapshot=%s error=%v", dat, snapshot, err)
	}
	core = "mame2003_plus"
	rejected, err := launcher.Create(ctx, "alternate-player", request)
	if !errors.Is(err, launch.ErrBlocked) || rejected.LaunchID != "" {
		t.Fatalf("mismatched target DAT accepted: %+v %v", rejected, err)
	}
	var items, createdVariants, jobs int
	err = dbapi.QueryRowContext(ctx, database.SQL, `SELECT (SELECT count(*) FROM import_items),(SELECT count(*) FROM game_variants WHERE game_id=?),(SELECT count(*) FROM jobs WHERE kind='VARIANT_VALIDATE')`, approved.GameID).Scan(&items, &createdVariants, &jobs)
	if err != nil || items != 0 || createdVariants != 2 || jobs != 1 {
		t.Fatalf("alternate preparation created workflow state: items=%d variants=%d jobs=%d error=%v", items, createdVariants, jobs, err)
	}
}

func insertAlternateArcadeDAT(t *testing.T, database dbapi.DB, core, dat string, mismatch bool) {
	t.Helper()
	target, err := testsupport.LookupRuntimeTarget(t.Context(), database, core)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO dat_versions(id,core_id,provider_id,target_id,builtin_relative_path,sha256,parser_version,parse_status,is_active,version,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms)
 SELECT ?,?,?,?,'data/dat/alternate.xml',sha256,parser_version,parse_status,1,1,created_at_ms,updated_at_ms,parsed_at_ms,activated_at_ms FROM dat_versions WHERE id='attachment-dat'`, dat, core, target.ProviderID, target.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO dat_machines(dat_version_id,machine_name,description,year,manufacturer,cloneof,romof,is_explicit_bios,classification)
 SELECT ?,machine_name,description,year,manufacturer,cloneof,romof,is_explicit_bios,classification FROM dat_machines WHERE dat_version_id='attachment-dat' AND machine_name='c'`, dat)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(t.Context(), `INSERT INTO dat_rom_entries(dat_version_id,machine_name,ordinal,name,size_bytes,crc32,sha1,status)
 SELECT ?,machine_name,ordinal,name,size_bytes,crc32,sha1,status FROM dat_rom_entries WHERE dat_version_id='attachment-dat' AND machine_name='c'`, dat)
	if err != nil {
		t.Fatal(err)
	}
	if mismatch {
		if _, err := database.ExecContext(t.Context(), `UPDATE dat_rom_entries SET sha1=? WHERE dat_version_id=?`, strings.Repeat("a", 40), dat); err != nil {
			t.Fatal(err)
		}
	}
}

func awaitAlternateArcadeJob(t *testing.T, database dbapi.DB, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var state string
		err := dbapi.QueryRowContext(ctx, database, `SELECT state FROM jobs WHERE id=?`, id).Scan(&state)
		if err != nil {
			t.Fatal(err)
		}
		if state == "SUCCEEDED" {
			return
		}
		if state == "FAILED" || state == "CANCELLED" {
			t.Fatalf("alternate validation ended %s", state)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
}
