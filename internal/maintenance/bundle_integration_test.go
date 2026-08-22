//go:build integration

package maintenance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/accounts"
	"retrom/internal/authn"
	"retrom/internal/blobstore"
	"retrom/internal/config"
	"retrom/internal/dependencies"
	"retrom/internal/netplay"
	"retrom/internal/processlock"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/tagging"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

const backupGameID = "01980000-0000-7000-8000-00000000f501"

func seedBackupFavorites(t *testing.T, database *sql.DB, profileID string) {
	t.Helper()
	const (
		metadataID = "01980000-0000-7000-8000-00000000d501"
		contentID  = "01980000-0000-7000-8000-00000000e501"
		folderID   = "01980000-0000-7000-8000-00000000c501"
	)
	transaction, err := database.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = transaction.Rollback() }()
	if _, err := transaction.Exec("PRAGMA defer_foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(?,?,?,'','','','',NULL,1995,'ADMIN_EDIT',NULL,1000)
`, metadataID, backupGameID, "Backup favorite"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,created_at_ms
) VALUES(?,?,'ADMIN_REPLACE','backup-favorite-test','[]',?,1000)
`, contentID, backupGameID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(?,'01980000-0000-7000-8000-000000000005','PUBLISHED',?,?,lower(?),1,1000,1000)
`, backupGameID, metadataID, contentID, "Backup favorite"); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO favorite_games(profile_id,game_id,created_at_ms) VALUES(?,?,2000)`, profileID, backupGameID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(`
INSERT INTO favorite_folders(id,profile_id,name,name_key,version,created_at_ms,updated_at_ms)
VALUES(?,?,'备份收藏夹','备份收藏夹',1,2000,2000)
`, folderID, profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.Exec(
		`INSERT INTO favorite_folder_games(profile_id,folder_id,game_id,created_at_ms) VALUES(?,?,?,2000)`,
		profileID, folderID, backupGameID,
	); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func favoriteBackupSnapshot(t *testing.T, database *sql.DB) (string, int) {
	t.Helper()
	rows, err := database.Query(`
SELECT value FROM (
  SELECT 'game|'||profile_id||'|'||game_id||'|'||created_at_ms AS value FROM favorite_games
  UNION ALL
  SELECT 'folder|'||id||'|'||profile_id||'|'||name||'|'||name_key||'|'||version||'|'||created_at_ms||'|'||updated_at_ms FROM favorite_folders
  UNION ALL
  SELECT 'membership|'||profile_id||'|'||folder_id||'|'||game_id||'|'||created_at_ms FROM favorite_folder_games
) ORDER BY value
`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	hash := sha256.New()
	count := 0
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		_, _ = hash.Write([]byte(value + "\n"))
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil)), count
}

func seedBackupTags(t *testing.T, ctx context.Context, database *sql.DB, userID string) {
	t.Helper()
	service := tagging.New(database, func() time.Time { return time.UnixMilli(3_000) })
	active, err := service.Create(ctx, userID, "合作")
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := service.Create(ctx, userID, "历史标签")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceGameTags(
		ctx, userID, backupGameID, 1, []string{active.TagID, deleted.TagID},
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, deleted.TagID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Delete(ctx, userID, deleted.TagID, deleted.Name, current.Version); err != nil {
		t.Fatal(err)
	}
}

func tagBackupSnapshot(t *testing.T, database *sql.DB) (string, int) {
	t.Helper()
	rows, err := database.Query(`
SELECT value FROM (
  SELECT 'tag|'||id||'|'||name||'|'||name_key||'|'||search_text||'|'||status||'|'||version||'|'||
    created_by_user_id||'|'||updated_by_user_id||'|'||created_at_ms||'|'||updated_at_ms||'|'||
    coalesce(deleted_at_ms,-1) AS value FROM tags
  UNION ALL
  SELECT 'game-tag|'||game_id||'|'||tag_id||'|'||assigned_by_user_id||'|'||created_at_ms FROM game_tags
  UNION ALL
  SELECT 'audit|'||action||'|'||resource_type||'|'||resource_id||'|'||coalesce(before_json,'')||'|'||
    coalesce(after_json,'')||'|'||coalesce(diff_json,'')||'|'||created_at_ms
  FROM audit_events WHERE action IN ('TAG_CREATED','TAG_DELETED','GAME_TAGS_REPLACED')
) ORDER BY value
`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	hash := sha256.New()
	count := 0
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		_, _ = hash.Write([]byte(value + "\n"))
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(hash.Sum(nil)), count
}

func TestBackupRestoreRoundTripAndOnlineRefusal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	dataDir := filepath.Join(root, "source")
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencyRoot := filepath.Join(repositoryRoot, "data")
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	dependencySet, err := dependencies.Load(dependencyRoot, []string{"4.2.3"}, "4.2.3")
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
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	upload, err := uploadService.Create(
		ctx,
		uploads.CreateRequest{
			SourceType: "FILES",
			Files:      []uploads.FileDeclaration{{ClientFileID: "part", RelativePath: "partial.bin", SizeBytes: 4}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	part := []byte("data")
	digest := "sha-256=:Om6weQ85rIfJTzhWst0sXREOaBFgImGpqSPTuyOtyLc=:"
	if err := uploadService.PutPart(ctx, upload.ID, upload.Files[0].ID, 0, "bytes 0-3/4", digest, bytes.NewReader(part)); err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := netplay.LoadOrCreateCredentials(dataDir); err != nil {
		t.Fatal(err)
	}
	accountService, err := accounts.New(
		ctx, database.SQL, credentials, config.ModeTest, authn.EmptyBlocklist{}, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := accountService.Start(ctx); err != nil {
		t.Fatal(err)
	}
	admin, err := accountService.Login(ctx, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := accountService.CreateInvitation(
		ctx, admin.Principal, "USER", false, uuid.NewString(),
	); err != nil {
		t.Fatal(err)
	}
	seedBackupFavorites(t, database.SQL, admin.Principal.ProfileID)
	seedBackupTags(t, ctx, database.SQL, admin.Principal.UserID)
	const serverImportID = "01980000-0000-7000-8000-00000000f601"
	const serverImportJobID = "01980000-0000-7000-8000-00000000f602"
	const pegasusImportID = "01980000-0000-7000-8000-00000000f603"
	const pegasusScanJobID = "01980000-0000-7000-8000-00000000f604"
	if _, err := database.SQL.Exec(`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,leased_until_ms,heartbeat_at_ms,worker_id,created_at_ms,updated_at_ms)
VALUES(?,'SERVER_IMPORT',?,'SERVER_BIOS_IMPORT',?,1,'{"inputExecutionNo":1}',1,'RUNNING',1,4,1,1,60000,1,
'server-import-worker',1,1)
`, serverImportJobID, serverImportID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO server_imports(id,kind,root_id,root_label_snapshot,source_relative_path,root_config_digest,
catalog_snapshot_digest,replace_if_better,state,phase,catalog_item_count,job_id,created_by_user_id,version,
created_at_ms,updated_at_ms)
VALUES(?,'BIOS_DIRECTORY','backup-root','Backup root','bios',?,?,0,'RUNNING','DISCOVERING',0,?,?,1,1,1)
`, serverImportID, strings.Repeat("b", 64), strings.Repeat("c", 64), serverImportJobID, admin.Principal.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO jobs(id,scope_type,scope_id,kind,dedupe_key,execution_no,payload_json,cancellable,state,
attempt_count,max_attempts,version,available_at_ms,finished_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PEGASUS_IMPORT',?,'SERVER_PEGASUS_SCAN',?,1,'{"inputExecutionNo":1}',1,'SUCCEEDED',1,4,1,1,1,1,1)
`, pegasusScanJobID, pegasusImportID, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
INSERT INTO pegasus_imports(id,root_id,root_label_snapshot,source_relative_path,root_config_digest,state,phase,
scan_job_id,created_by_user_id,created_at_ms,updated_at_ms,expires_at_ms)
VALUES(?,'backup-root','Backup root','games',?,'AWAITING_MAPPING',NULL,?,?,1,1,9999999999999)
`, pegasusImportID, strings.Repeat("e", 64), pegasusScanJobID, admin.Principal.UserID); err != nil {
		t.Fatal(err)
	}
	favoriteHash, favoriteRows := favoriteBackupSnapshot(t, database.SQL)
	if favoriteRows != 3 {
		t.Fatalf("seed favorite rows = %d", favoriteRows)
	}
	tagHash, tagRows := tagBackupSnapshot(t, database.SQL)
	if tagRows != 8 {
		t.Fatalf("seed tag rows = %d", tagRows)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	configuration := config.Maintenance{
		DataDir:            dataDir,
		DBPath:             filepath.Join(dataDir, "retrom.db"),
		DependencyRoot:     dependencyRoot,
		DependencyVersions: []string{"4.2.3"},
		ActiveEJSVersion:   "4.2.3",
	}
	lock, err := processlock.Acquire(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(ctx, configuration, filepath.Join(root, "online-backup"), time.Now); !errors.Is(
		err,
		ErrBackupOffline,
	) {
		t.Fatalf("online backup error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	manifest, err := Backup(ctx, configuration, bundle, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts.UploadPartCount != 1 || manifest.Counts.DependencyVersionCount != 1 {
		t.Fatalf("backup counts = %#v", manifest.Counts)
	}
	restored := filepath.Join(root, "restored")
	if _, err := Restore(ctx, config.Maintenance{DependencyRoot: dependencyRoot, DependencyVersions: []string{"4.2.3"}, ActiveEJSVersion: "4.2.3"}, bundle, restored); err != nil {
		t.Fatal(err)
	}
	if _, _, err := digestRegular(filepath.Join(restored, "tmp", "uploads", upload.ID, upload.Files[0].ID, "0")); err != nil {
		t.Fatal(err)
	}
	restoredDatabase, err := openDatabase(ctx, filepath.Join(restored, "retrom.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restoredDatabase.Close()
	restoredFavoriteHash, restoredFavoriteRows := favoriteBackupSnapshot(t, restoredDatabase)
	if restoredFavoriteRows != favoriteRows || restoredFavoriteHash != favoriteHash {
		t.Fatalf(
			"favorite backup snapshot changed: before=%d/%s after=%d/%s",
			favoriteRows, favoriteHash, restoredFavoriteRows, restoredFavoriteHash,
		)
	}
	restoredTagHash, restoredTagRows := tagBackupSnapshot(t, restoredDatabase)
	if restoredTagRows != tagRows || restoredTagHash != tagHash {
		t.Fatalf(
			"tag backup snapshot changed: before=%d/%s after=%d/%s",
			tagRows, tagHash, restoredTagRows, restoredTagHash,
		)
	}
	var fencedSessions, fencedLinks, fenceAudits, fencedServerImports, fencedPegasusImports int
	if err := restoredDatabase.QueryRow(`
SELECT
  (SELECT count(*) FROM auth_sessions WHERE revoked_reason='RESTORE' AND revoked_at_ms IS NOT NULL),
  (SELECT count(*) FROM account_links WHERE revoked_by_kind='SYSTEM' AND revoked_at_ms IS NOT NULL),
  (SELECT count(*) FROM audit_events
   WHERE actor_kind='SYSTEM' AND actor_label='restore-security-fence' AND action='RESTORE_SECURITY_FENCE'),
  (SELECT count(*) FROM server_imports import JOIN jobs job ON job.id=import.job_id
   WHERE import.id=? AND import.state='FAILED' AND import.last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED'
   AND job.state='FAILED' AND job.error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED' AND job.error_retryable=0),
  (SELECT count(*) FROM pegasus_imports WHERE id=? AND state='FAILED'
   AND last_error_code='SERVER_IMPORT_SOURCE_NOT_RESTORED' AND completed_at_ms IS NOT NULL)
`, serverImportID, pegasusImportID).Scan(&fencedSessions, &fencedLinks, &fenceAudits, &fencedServerImports, &fencedPegasusImports); err != nil ||
		fencedSessions < 1 || fencedLinks != 1 || fenceAudits != 1 || fencedServerImports != 1 || fencedPegasusImports != 1 {
		t.Fatalf(
			"restore fence = sessions=%d links=%d audits=%d serverImports=%d pegasusImports=%d error=%v",
			fencedSessions, fencedLinks, fenceAudits, fencedServerImports, fencedPegasusImports, err,
		)
	}
	if _, err := Restore(ctx, config.Maintenance{DependencyRoot: dependencyRoot, DependencyVersions: []string{"4.2.3"}, ActiveEJSVersion: "4.2.3"}, bundle, restored); !errors.Is(
		err,
		ErrInvalidBundle,
	) {
		t.Fatalf("overwrite restore error = %v", err)
	}
}
