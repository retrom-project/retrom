package libraryimport

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	libraryservice "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestLargeSourceDuplicateLookupExcludesUnrelatedGames(t *testing.T) {
	for _, games := range []int{0, 100, 1000, 5000} {
		t.Run(fmt.Sprintf("games=%d", games), func(t *testing.T) {
			db := duplicateScaleDatabase(t, games, 5000)
			for _, platform := range []string{"gba", "nes"} {
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				started := time.Now()
				matches, err := BindContentDuplicates(db).PublishedMatches(ctx, libraryservice.DuplicateQuery{
					PlatformID: platform, SnapshotID: "snapshot", ContentKind: "SINGLE_FILE",
				})
				cancel()
				t.Logf("platform=%s games=%d files=5000 elapsed=%s", platform, games, time.Since(started))
				if err != nil || len(matches) != 0 {
					t.Fatalf("unrelated games delayed or matched large source: %+v %v", matches, err)
				}
			}
		})
	}
}

func duplicateScaleDatabase(t *testing.T, games, files int) dbapi.DB {
	t.Helper()
	db := metadataDatabase(t)
	instance := testsupport.MustPlatformInstanceID(t, db, "nes/fceumm")
	metadataExec(t, db, `INSERT INTO upload_files(id,upload_session_id,relative_path,declared_size_bytes,
 received_size_bytes,final_file_record,state,created_at_ms,updated_at_ms)
 VALUES('scale-upload','upload','project',1,1,'{}','COMPLETE',1,1)`)
	metadataExec(t, db, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?)
 INSERT INTO import_item_source_snapshot_files(source_snapshot_id,role,logical_name,upload_file_id,
 file_record,sort_order,created_at_ms)
 SELECT 'snapshot','CONTENT',printf('file-%d',x),'scale-upload',json_object('sha256',printf('%064x',x)),x,1 FROM n`, files)
	if games > 0 {
		metadataExec(t, db, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<?)
 INSERT INTO games(id,platform_instance_id,title,title_initial,description,developer,publisher,genre,
 metadata_source_kind,content_source_kind,source_manifest_json,source_manifest_digest,status,search_text,
 created_at_ms,updated_at_ms)
 SELECT printf('game-%d',x),?,'Game','G','','','','','IMPORT_REVIEW','IMPORT_REVIEW','{}',printf('%064x',x),
 'PUBLISHED','game',x,x FROM n`, games, instance)
		metadataExec(t, db, `INSERT INTO game_files(game_id,role,logical_name,file_record,sort_order)
 SELECT id,'CONTENT','game.nes',json_object('sha256',printf('%064x',created_at_ms)),0 FROM games`)
	}
	return db
}
