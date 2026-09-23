//go:build integration

package libraryimport

import (
	"bytes"
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/testsupport"
)

func TestRPGReviewRequiresExplicitSelfContainedConfirmation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	database, blobs, dataDir := openImportGroupFixture(t, ctx)
	uploadID := completeProjectUpload(t, ctx, database.SQL, blobs, dataDir, "GENERAL", requiredRPGPackArchive(t))
	importer := New(database.SQL, time.Now).WithBlobStore(blobs)
	created, err := importer.Create(ctx, CreateRequest{
		UploadID: uploadID, TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "rpgmaker/rpgmaker"),
		MetadataProvider: "NONE", ContentMode: "STANDARD", TagIDs: []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var itemID string
	if err := database.SQL.QueryRowContext(ctx, "SELECT id FROM import_items WHERE import_job_id=?", created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	assertRPGDependencyStatus(t, database.SQL, itemID, "BLOCKED", "RPG_EXTERNAL_RTP_REQUIRED")
	current := refreshRPGDraft(t, importer, itemID, 1)
	current = refreshRPGDraft(t, importer, itemID, current.Version)
	assertRPGDependencyStatus(t, database.SQL, itemID, "BLOCKED", "RPG_EXTERNAL_RTP_REQUIRED")
	if _, err := importer.Approve(ctx, itemID, current.Version); err == nil {
		t.Fatal("external RTP declaration bypassed self-contained project policy")
	}
	confirmed := true
	current, err = importer.PatchDraft(ctx, itemID, current.Version, DraftPatch{RPGSelfContainedOverride: &confirmed, TagIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	assertRPGDependencyStatus(t, database.SQL, itemID, "READY", "READY")
	confirmed = false
	current, err = importer.PatchDraft(ctx, itemID, current.Version, DraftPatch{RPGSelfContainedOverride: &confirmed, TagIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	assertRPGDependencyStatus(t, database.SQL, itemID, "BLOCKED", "RPG_EXTERNAL_RTP_REQUIRED")
	confirmed = true
	current, err = importer.PatchDraft(ctx, itemID, current.Version, DraftPatch{RPGSelfContainedOverride: &confirmed, TagIDs: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := importer.Approve(ctx, itemID, current.Version)
	if err != nil || approved.GameID == "" {
		t.Fatalf("explicit administrator confirmation cannot publish: %+v %v", approved, err)
	}
}

func refreshRPGDraft(t *testing.T, importer *Service, itemID string, version int64) DraftResult {
	t.Helper()
	current, err := importer.PatchDraft(t.Context(), itemID, version, DraftPatch{
		Metadata: &MetadataPatch{}, TagIDs: []string{},
	})
	if err != nil {
		t.Fatalf("ordinary RPG metadata/recheck patch: %v", err)
	}
	return current
}

func assertRPGDependencyStatus(t *testing.T, database *sql.DB, itemID, status, code string) string {
	t.Helper()
	var id, gotStatus, gotCode string
	if err := database.QueryRowContext(t.Context(), `
SELECT id,status,compatibility_code FROM import_item_core_validations WHERE import_item_id=?
ORDER BY created_at_ms DESC,id DESC LIMIT 1`, itemID).Scan(&id, &gotStatus, &gotCode); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotCode != code {
		t.Fatalf("RPG dependency status=%s/%s want %s/%s", gotStatus, gotCode, status, code)
	}
	return id
}

func requiredRPGPackArchive(t *testing.T) []byte {
	t.Helper()
	root := "../../testdata/public-roms/rpgmaker-smoke/rpg2000"
	files := make(map[string][]byte)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if name == "RPG_RT.ini" {
			contents = bytes.ReplaceAll(contents, []byte("FullPackageFlag=1"), []byte("FullPackageFlag=0"))
		}
		files[filepath.ToSlash(name)] = contents
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return makeZIP(t, files)
}
