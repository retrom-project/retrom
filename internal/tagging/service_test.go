package tagging

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/cleanup"
	"retrom/internal/store"
)

const (
	testAdminID = "01980000-0000-7000-8000-00000000b401"
	testGameID  = "01980000-0000-7000-8000-00000000f401"
)

func openTaggingTest(t *testing.T) (*store.DB, *Service, *int64) {
	t.Helper()
	clock := int64(1_000)
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "retrom.db"), func() time.Time {
		return time.UnixMilli(clock)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SQL.Exec(`
PRAGMA defer_foreign_keys=ON;
BEGIN;
INSERT INTO profiles(id,display_name,created_at_ms)
VALUES('01980000-0000-7000-8000-00000000a401','Tag Admin',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('` + testAdminID + `','01980000-0000-7000-8000-00000000a401','tag.admin','Tag Admin','ADMIN','ENABLED',1,1);
INSERT INTO game_metadata_revisions(
  id,game_id,title,description,developer,publisher,genre,players,release_year,
  source_kind,source_ref_id,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f402','` + testGameID + `','Tagged Game','','','','',NULL,2001,
  'ADMIN_EDIT',NULL,1
);
INSERT INTO game_content_revisions(
  id,game_id,source_kind,source_ref_id,source_manifest_json,source_manifest_digest,content_kind,created_at_ms
) VALUES(
  '01980000-0000-7000-8000-00000000f403','` + testGameID + `','ADMIN_REPLACE','tag-fixture','[]',
  'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','SINGLE_FILE',1
);
INSERT INTO games(
  id,platform_instance_id,status,current_metadata_revision_id,current_content_revision_id,
  search_text,version,created_at_ms,updated_at_ms
) VALUES(
  '` + testGameID + `','01980000-0000-7000-8000-000000000005','PUBLISHED',
  '01980000-0000-7000-8000-00000000f402','01980000-0000-7000-8000-00000000f403',
  'tagged game',1,1,1
);
COMMIT;
`); err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	return database, New(database.SQL, func() time.Time { return time.UnixMilli(clock) }), &clock
}

func TestTagLifecycleAndNameReuse(t *testing.T) {
	t.Parallel()
	database, service, clock := openTaggingTest(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	ctx := context.Background()

	created, err := service.Create(ctx, testAdminID, "  ACTION ")
	if err != nil || created.Name != "ACTION" || created.Version != 1 || created.Status != StatusActive {
		t.Fatalf("created = %#v, %v", created, err)
	}
	if _, err := service.Create(ctx, testAdminID, "action"); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("case-fold conflict = %v", err)
	}
	*clock = 2_000
	renamed, err := service.Rename(ctx, testAdminID, created.TagID, "合作", created.Version)
	if err != nil || renamed.Name != "合作" || renamed.Version != 2 {
		t.Fatalf("renamed = %#v, %v", renamed, err)
	}
	if _, err := service.Rename(ctx, testAdminID, created.TagID, "旧版本", 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale rename = %v", err)
	}
	if _, _, err := service.Delete(ctx, testAdminID, created.TagID, "错误", renamed.Version); !errors.Is(err, ErrDeleteConfirmation) {
		t.Fatalf("confirmation = %v", err)
	}
	*clock = 3_000
	deleted, _, err := service.Delete(ctx, testAdminID, created.TagID, "合作", renamed.Version)
	if err != nil || deleted.Status != StatusDeleted || deleted.Version != 3 || deleted.DeletedAtMS == nil {
		t.Fatalf("deleted = %#v, %v", deleted, err)
	}
	recreated, err := service.Create(ctx, testAdminID, "合作")
	if err != nil || recreated.TagID == deleted.TagID {
		t.Fatalf("recreated = %#v, %v", recreated, err)
	}
	items, err := service.List(ctx, ListFilter{Status: "ALL", Sort: SortNameAsc, Limit: 10})
	if err != nil || len(items) != 2 {
		t.Fatalf("list = %#v, %v", items, err)
	}
	var audits int
	if err := database.SQL.QueryRow(`SELECT count(*) FROM audit_events WHERE resource_type='TAG'`).Scan(&audits); err != nil || audits != 4 {
		t.Fatalf("audits = %d, %v", audits, err)
	}
}

func TestReplaceGameTagsAndDeleteInvalidatesGameVersion(t *testing.T) {
	t.Parallel()
	database, service, clock := openTaggingTest(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	ctx := context.Background()
	first, err := service.Create(ctx, testAdminID, "双人")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, testAdminID, "动作")
	if err != nil {
		t.Fatal(err)
	}
	*clock = 2_000
	replaced, err := service.ReplaceGameTags(ctx, testAdminID, testGameID, 1, []string{second.TagID, first.TagID})
	if err != nil || replaced.Version != 2 || len(replaced.Tags) != 2 || replaced.Tags[0].Name != "动作" {
		t.Fatalf("replace = %#v, %v", replaced, err)
	}
	noOp, err := service.ReplaceGameTags(ctx, testAdminID, testGameID, 2, []string{first.TagID, second.TagID})
	if err != nil || noOp.Version != 2 {
		t.Fatalf("no-op = %#v, %v", noOp, err)
	}
	if _, err := service.ReplaceGameTags(ctx, testAdminID, testGameID, 1, []string{}); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale replace = %v", err)
	}
	*clock = 3_000
	currentFirst, err := service.Get(ctx, first.TagID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Delete(ctx, testAdminID, first.TagID, first.Name, currentFirst.Version); err != nil {
		t.Fatal(err)
	}
	var gameVersion int64
	if err := database.SQL.QueryRow(`SELECT version FROM games WHERE id=?`, testGameID).Scan(&gameVersion); err != nil || gameVersion != 3 {
		t.Fatalf("game version = %d, %v", gameVersion, err)
	}
	references, err := service.References(ctx, []string{testGameID})
	if err != nil || len(references[testGameID]) != 1 || references[testGameID][0].TagID != second.TagID {
		t.Fatalf("references = %#v, %v", references, err)
	}
	invalidID := "01980000-0000-7000-8000-00000000c999"
	if _, err := service.ReplaceGameTags(ctx, testAdminID, testGameID, 3, []string{invalidID}); !errors.Is(err, ErrReferenceInvalid) {
		t.Fatalf("invalid reference = %v", err)
	}
}

func TestTagCapacityAndDatabaseAssignmentGuard(t *testing.T) {
	t.Parallel()
	database, service, _ := openTaggingTest(t)
	defer func() { cleanup.Error("close", database.Close()) }()
	ctx := context.Background()

	if _, err := database.SQL.Exec(`
WITH RECURSIVE sequence(value) AS (
  SELECT 1
  UNION ALL SELECT value+1 FROM sequence WHERE value<1000
)
INSERT INTO tags(
  id,name,name_key,search_text,status,version,created_by_user_id,updated_by_user_id,
  created_at_ms,updated_at_ms,deleted_at_ms
)
SELECT printf('01980000-0000-7000-8001-%012x',value),
       printf('Tag %04d',value),printf('tag %04d',value),printf('tag %04d',value),
       'ACTIVE',1,?, ?,1,1,NULL
FROM sequence
`, testAdminID, testAdminID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, testAdminID, "超过实例上限"); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("capacity error = %v", err)
	}

	for index := 1; index <= MaxTagsPerOwner; index++ {
		tagID := fmt.Sprintf("01980000-0000-7000-8001-%012x", index)
		if _, err := database.SQL.Exec(`
INSERT INTO game_tags(game_id,tag_id,assigned_by_user_id,created_at_ms) VALUES(?,?,?,2)
`, testGameID, tagID, testAdminID); err != nil {
			t.Fatalf("insert assignment %d: %v", index, err)
		}
	}
	if _, err := database.SQL.Exec(`
INSERT INTO tags(
  id,name,name_key,search_text,status,version,created_by_user_id,updated_by_user_id,
  created_at_ms,updated_at_ms,deleted_at_ms
) VALUES('01980000-0000-7000-8002-000000000001','Extra','extra','extra','ACTIVE',1,?,?,1,1,NULL);
INSERT INTO game_tags(game_id,tag_id,assigned_by_user_id,created_at_ms)
VALUES(?,'01980000-0000-7000-8002-000000000001',?,2)
`, testAdminID, testAdminID, testGameID, testAdminID); err == nil {
		t.Fatal("database accepted a twenty-first active game tag")
	}
}
