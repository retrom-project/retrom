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

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/testsupport"
)

func TestRPGReviewRechecksRealPackDependenciesWithoutRuntimeProof(t *testing.T) {
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
	assertRPGDependencyStatus(t, database.SQL, itemID, "BLOCKED", "RPG_RUNTIME_PACK_MISSING")
	current := refreshRPGDraft(t, importer, itemID, 1)
	seedReadyRPGDependency(t, database.SQL, blobs)
	current = refreshRPGDraft(t, importer, itemID, current.Version)
	validationID := assertRPGDependencyStatus(t, database.SQL, itemID, "READY", "READY")
	var selected int
	if err := database.SQL.QueryRowContext(ctx, "SELECT count(*) FROM review_draft_runtime_pack_selections").Scan(&selected); err != nil || selected != 1 {
		t.Fatalf("recheck did not bind the available RTP: %d %v", selected, err)
	}
	setRPGDependencyEnabled(t, database.SQL, false)
	fresh, err := importer.ReviewValidationCurrent(ctx, validationID)
	if err != nil || fresh {
		t.Fatalf("disabled required RTP left the review current: %t %v", fresh, err)
	}
	if _, err := importer.Approve(ctx, itemID, current.Version); err == nil {
		t.Fatal("publication bypassed a disabled required RTP")
	}
	current = refreshRPGDraft(t, importer, itemID, current.Version)
	assertRPGDependencyStatus(t, database.SQL, itemID, "BLOCKED", "RPG_RUNTIME_PACK_MISSING")
	setRPGDependencyEnabled(t, database.SQL, true)
	current = refreshRPGDraft(t, importer, itemID, current.Version)
	validationID = assertRPGDependencyStatus(t, database.SQL, itemID, "READY", "READY")
	current = refreshRPGDraft(t, importer, itemID, current.Version)
	if repeated := assertRPGDependencyStatus(t, database.SQL, itemID, "READY", "READY"); repeated != validationID {
		t.Fatal("unchanged dependencies created a redundant validation")
	}
	var derived int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM import_item_validation_files
WHERE import_item_core_validation_id=? AND role='RPG_EASYRPG_INDEX'`, validationID).Scan(&derived); err != nil || derived != 1 {
		t.Fatalf("recheck lost the actual EasyRPG index: %d %v", derived, err)
	}
	approved, err := importer.Approve(ctx, itemID, current.Version)
	if err != nil || approved.GameID == "" {
		t.Fatalf("valid RPG project required a proof session: %+v %v", approved, err)
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

func seedReadyRPGDependency(t *testing.T, database *sql.DB, blobs *blobstore.Store) {
	t.Helper()
	blob, err := blobs.Put(bytes.NewReader([]byte("RTP metadata test payload")))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	blobID, err := blobstore.EnsureRecord(t.Context(), database, blob, "application/octet-stream", now)
	if err != nil {
		t.Fatal(err)
	}
	installationID := uuid.NewString()
	for _, query := range []string{
		"INSERT INTO profiles(id,display_name,created_at_ms) VALUES('pack-profile','Pack reviewer',0)",
		`INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('pack-reviewer','pack-profile','pack-reviewer','Pack reviewer','ADMIN','ENABLED',0,0)`,
	} {
		if _, err := database.ExecContext(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO runtime_asset_pack_installations(
 id,definition_id,files_digest,file_count,total_bytes,bundle_blob_id,bundle_sha256,status,
 diagnostic_json,created_by_user_id,created_at_ms)
VALUES(?,'rpg2000_rtp',?,1,?,?,?,'VALIDATING','{}','pack-reviewer',?)`,
		installationID, blob.SHA256, blob.Size, blobID, blob.SHA256, now); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
INSERT INTO runtime_asset_pack_files(installation_id,path,ordinal,blob_id,size_bytes,sha256)
VALUES(?,'Music/theme.wav',0,?,?,?)`, installationID, blobID, blob.Size, blob.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(t.Context(), `
UPDATE runtime_asset_pack_installations SET status='READY',validated_at_ms=?,version=version+1 WHERE id=?`, now, installationID); err != nil {
		t.Fatal(err)
	}
}

func setRPGDependencyEnabled(t *testing.T, database *sql.DB, enabled bool) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), `
UPDATE runtime_asset_pack_definitions SET enabled=? WHERE id='rpg2000_rtp'`, enabled); err != nil {
		t.Fatal(err)
	}
}
