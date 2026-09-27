//go:build integration

package libraryimport

import (
	"os"
	"testing"
	"time"

	"retrom/internal/composition/cleanupjobs"
	dbapi "retrom/internal/database"
	"retrom/internal/filestore"
)

func assertReconfigurationFilesIndependent(t *testing.T, database dbapi.DB, files *filestore.Store, original, replacement string) {
	t.Helper()
	var originalID, cloneID, cloneOwner, cloneOwnerID, uploadID, reviewID string
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT source_file.final_blob_id,
 clone_file.final_blob_id,stored.owner_kind,stored.owner_id,clone.upload_session_id,review.blob_id
 FROM import_job_file_resolutions resolution
 JOIN upload_files source_file ON source_file.id=resolution.upload_file_id
 JOIN import_jobs clone ON clone.id=resolution.replacement_import_job_id
 JOIN upload_files clone_file ON clone_file.upload_session_id=clone.upload_session_id
 JOIN stored_files stored ON stored.id=clone_file.final_blob_id
 JOIN import_items item ON item.import_job_id=clone.id
 JOIN import_item_source_files review ON review.import_item_id=item.id
 WHERE resolution.import_job_id=? AND clone.id=?`, original, replacement).
		Scan(&originalID, &cloneID, &cloneOwner, &cloneOwnerID, &uploadID, &reviewID)
	if err != nil || originalID == cloneID || cloneOwner != "UPLOAD" || cloneOwnerID != uploadID {
		t.Fatalf("replacement upload still depends on original: original=%s clone=%s owner=%s/%s expected=%s error=%v",
			originalID, cloneID, cloneOwner, cloneOwnerID, uploadID, err)
	}
	originalStat, err := os.Stat(files.Path(originalID))
	if err != nil {
		t.Fatal(err)
	}
	cloneStat, err := os.Stat(files.Path(cloneID))
	if err != nil || os.SameFile(originalStat, cloneStat) {
		t.Fatalf("replacement must own independent physical bytes: %v", err)
	}
	releases, err := cleanupjobs.New(t.Context(), database, files, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
	for attempt := 0; attempt < 64; attempt++ {
		if err := releases.ReconcileDeletion(t.Context()); err != nil {
			t.Fatal(err)
		}
		worked, err := releases.RunOnce(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	var originals int
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM stored_files WHERE id=?`, originalID).
		Scan(&originals); err != nil || originals != 0 {
		t.Fatalf("original upload did not finish deletion: count=%d error=%v", originals, err)
	}
	for _, id := range []string{cloneID, reviewID} {
		if _, err := os.ReadFile(files.Path(id)); err != nil {
			t.Fatalf("original cleanup damaged replacement input or review: %v", err)
		}
	}
}

func assertFailedReconfigurationRetiresClone(t *testing.T, importer *Service, database dbapi.DB, source string, version int64) {
	t.Helper()
	if _, err := importer.Reconfigure(t.Context(), source, version, ReconfigureRequest{
		TargetPlatformInstanceID: "missing-target", MetadataProvider: "NONE",
	}); err == nil {
		t.Fatal("unknown replacement target must fail")
	}
	var retired int
	err := dbapi.QueryRowContext(t.Context(), database, `SELECT count(*) FROM stored_files f
 WHERE owner_kind='UPLOAD' AND retired_at_ms IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM upload_sessions u WHERE u.id=f.owner_id)`).Scan(&retired)
	if err != nil || retired != 1 {
		t.Fatalf("failed replacement left owned files without a lifecycle: retired=%d error=%v", retired, err)
	}
}
