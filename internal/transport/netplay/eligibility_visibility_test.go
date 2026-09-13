package netplay

import (
	"context"
	"strings"
	"testing"
	"time"

	"retrom/internal/repo/dbexec"
	"retrom/internal/testkit/testsupport"
)

func TestDisabledPlatformCannotSupplyNetplayEligibility(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.UnixMilli(1_786_000_000_000).UTC()
	database := openNetplayTestDatabase(ctx, t, func() time.Time { return now })
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	const gameID = "01980000-0000-7000-8500-000000000010"
	const variantID = "01980000-0000-7000-8500-000000000014"
	const contentBlobID = "01980000-0000-7000-8500-000000000018"
	runtimeIdentity, err := testsupport.LookupRuntimeTarget(ctx, database.SQL, "fceumm")
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := database.SQL.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(transaction)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO games(id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,release_year,metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,source_manifest_json,source_manifest_digest,status,search_text,version,created_at_ms,updated_at_ms)
VALUES(?,(SELECT id FROM platform_instances WHERE catalog_template_key='nes/fceumm'),'Prepare fixture','P','','','','',2,NULL,'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','prepare-fixture','{}',?,'PUBLISHED','prepare fixture',1,?,?)`, []any{gameID, strings.Repeat("1", 64), now.UnixMilli(), now.UnixMilli()}},
		{`INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms) VALUES(?,?,32768,?,?,?,'application/octet-stream',?)`, []any{contentBlobID, strings.Repeat("9", 64), strings.Repeat("5", 32), strings.Repeat("6", 40), strings.Repeat("7", 8), now.UnixMilli()}},
		{`INSERT INTO game_files(game_id,role,logical_name,blob_id,sort_order) VALUES(?,'CONTENT','another-game.nes',?,0)`, []any{gameID, contentBlobID}},
		{`INSERT INTO game_variants(id,game_id,core_id,provider_id,target_id,emulator_game_id,status,compatibility_code,dependency_snapshot_json,version,created_at_ms,updated_at_ms)
VALUES(?,?,'fceumm',?,?,9001,'READY','READY','{"schemaVersion":1,"kind":"STATIC","bios":[]}',1,?,?)`, []any{variantID, gameID, runtimeIdentity.ProviderID, runtimeIdentity.TargetID, now.UnixMilli(), now.UnixMilli()}},
	}

	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.query, statement.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
	service := NewService(database.SQL, nil, nil, Options{}, func() time.Time { return now })
	rows, err := service.queryEligibilityRows(ctx, gameID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("enabled eligibility=%v, %v", rows, err)
	}
	if _, err := database.SQL.ExecContext(ctx, `UPDATE platform_instances SET enabled=0 WHERE id=(SELECT platform_instance_id FROM games WHERE id=?)`, gameID); err != nil {
		t.Fatal(err)
	}
	rows, err = service.queryEligibilityRows(ctx, gameID)
	if err != nil || len(rows) != 0 {
		t.Fatalf("disabled eligibility=%v, %v", rows, err)
	}
}
