package libraryimport

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestDuplicateMatchingPreservesRolesMultiplicityPlatformAndCurrentContent(t *testing.T) {
	db := duplicateScaleDatabase(t, 6, 3)
	metadataExec(t, db, `UPDATE import_item_source_snapshot_files
 SET file_record=json_object('sha256',printf('%064x',1)) WHERE sort_order=2`)
	copyDuplicateSource(t, db)
	metadataExec(t, db, `UPDATE game_files SET file_record=json_object('sha256',printf('%064x',3))
 WHERE game_id='game-2' AND sort_order=2`)
	metadataExec(t, db, `UPDATE game_files SET role='COMPANION' WHERE game_id='game-3' AND sort_order=3`)
	otherDirectory := "other-nes-directory"
	metadataExec(t, db, `INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,enabled,created_at_ms,updated_at_ms)
 SELECT ?,'nes','fceumm','Other NES','other-nes',1,1,1`, otherDirectory)
	otherPlatform := testsupport.MustPlatformInstanceID(t, db, "gba/mgba")
	metadataExec(t, db, `UPDATE games SET platform_instance_id=? WHERE id='game-4'`, otherDirectory)
	metadataExec(t, db, `UPDATE games SET platform_instance_id=? WHERE id='game-5'`, otherPlatform)
	metadataExec(t, db, `INSERT INTO jobs(id,scope_type,scope_id,kind,execution_no,dedupe_key,payload_json,state,cancellable,attempt_count,max_attempts,
 available_at_ms,created_at_ms,updated_at_ms)
 VALUES('cleanup','GAME','game-6','OWNER_CLEANUP',1,?,'{}','QUEUED',0,0,4,1,1,1)`, strings.Repeat("c", 64))
	metadataExec(t, db, `UPDATE games SET status='DELETED',deleted_at_ms=6,payload_state='RELEASING',
 payload_release_job_id='cleanup' WHERE id='game-6'`)
	assertDuplicateGames(t, db, "SINGLE_FILE", []string{"game-1", "game-4"})
	// Content replacement immediately changes matching; no derived digest can go stale.
	metadataExec(t, db, `UPDATE game_files SET file_record=json_object('sha256',printf('%064x',99))
 WHERE game_id='game-1' AND sort_order=1`)
	assertDuplicateGames(t, db, "SINGLE_FILE", []string{"game-4"})
	// Parent attachment changes the current source multiset, including its role.
	metadataExec(t, db, `INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,
 upload_file_id,file_record,sort_order,created_at_ms)
 VALUES('snapshot','COMPANION','parent.zip','scale-upload',json_object('sha256',printf('%064x',100)),4,1)`)
	assertDuplicateGames(t, db, "SINGLE_FILE", []string{})
}

func TestMultiDiscDuplicateMatchingUsesDiscOrderAndIgnoresWrapper(t *testing.T) {
	db := duplicateScaleDatabase(t, 3, 2)
	metadataExec(t, db, `UPDATE import_item_source_snapshot_files SET role='DISC'`)
	metadataExec(t, db, `UPDATE games SET content_kind='MULTI_DISC'`)
	copyDuplicateSource(t, db)
	metadataExec(t, db, `UPDATE game_files SET sort_order=3-sort_order WHERE game_id='game-2'`)
	metadataExec(t, db, `INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order)
 VALUES('game-3','PLAYLIST_SOURCE','renamed.m3u',json_object('sha256',printf('%064x',99)),0)`)
	assertDuplicateGames(t, db, "MULTI_DISC", []string{"game-1", "game-3"})
}

func TestLargeIdenticalProjectsStillMatchWithinRequestBudget(t *testing.T) {
	db := duplicateScaleDatabase(t, 2, 5000)
	copyDuplicateSource(t, db)
	assertDuplicateGames(t, db, "SINGLE_FILE", []string{"game-1", "game-2"})
}

func copyDuplicateSource(t *testing.T, db dbapi.DB) {
	t.Helper()
	metadataExec(t, db, `DELETE FROM game_files`)
	metadataExec(t, db, `INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order)
 SELECT game.id,source.role,'renamed-'||source.logical_name,source.file_record,source.sort_order
 FROM games game CROSS JOIN import_item_source_snapshot_files source WHERE source.source_snapshot_id='snapshot'`)
}

func assertDuplicateGames(t *testing.T, db dbapi.DB, kind string, want []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	games, err := BindContentDuplicates(db).PublishedMatches(ctx, libraryservice.DuplicateQuery{
		PlatformID: "nes", SnapshotID: "snapshot", ContentKind: kind,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(games))
	for _, game := range games {
		ids = append(ids, game.GameID)
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("duplicate games = %v, want %v", ids, want)
	}
}
