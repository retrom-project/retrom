package payloadrelease

import (
	"database/sql"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestGameDeleteImpactRejectsRegisteredByteOverflow(t *testing.T) {
	t.Parallel()
	db := impactGame(t, math.MaxInt64, 1)
	impact, err := GameDeleteImpact(t.Context(), db, "schedule-game")
	if err == nil || !reflect.DeepEqual(impact, GameImpact{}) {
		t.Fatalf("overflow produced actionable deletion impact: %+v error=%v", impact, err)
	}
}

func impactGame(t *testing.T, sizes ...int64) *sql.DB {
	t.Helper()
	db := schedulingGame(t)
	for i, size := range sizes {
		id := fmt.Sprintf("impact-blob-%d", i)
		_, err := db.ExecContext(t.Context(), `INSERT INTO blobs(id,sha256,size_bytes,md5,sha1,crc32,media_type,created_at_ms)
VALUES(?,?,?,?,?,?,'application/octet-stream',1)`, id, fmt.Sprintf("%064x", i+1), size,
			strings.Repeat("0", 32), strings.Repeat("0", 40), strings.Repeat("0", 8))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(t.Context(), `INSERT INTO game_files(game_id,role,logical_name,blob_id,sort_order)
VALUES('schedule-game','CONTENT',?,?,?)`, id, id, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	return db
}
