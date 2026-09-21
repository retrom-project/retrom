package payloadrelease

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"retrom/internal/testsupport"
)

func TestGameDeleteImpactRetainsOneSnapshotAcrossAllReads(t *testing.T) {
	t.Parallel()
	db := impactGame(t, 10)
	var changes atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
		BeforeQuery: func(ctx context.Context, query string, args []driver.NamedValue) error {
			if query == "SELECT size_bytes FROM blobs WHERE id=?" && len(args) == 1 &&
				args[0].Value == "impact-blob-0" && changes.CompareAndSwap(0, 1) {
				_, err := db.ExecContext(ctx, "UPDATE blobs SET size_bytes=20 WHERE id='impact-blob-0'")
				return err
			}
			return nil
		},
	})
	before, err := GameDeleteImpact(t.Context(), faultDB, "schedule-game")
	if err != nil || before.RegisteredBytes != "10" || before.ExclusiveBytes != "10" || changes.Load() != 1 {
		t.Fatalf("deletion impact mixed snapshots: %+v %v changes=%d", before, err, changes.Load())
	}
	after, err := GameDeleteImpact(t.Context(), db, "schedule-game")
	if err != nil || after.RegisteredBytes != "20" || after.ExclusiveBytes != "20" || before.ImpactDigest == after.ImpactDigest {
		t.Fatalf("next snapshot did not observe committed mutation: %+v %v", after, err)
	}
}

func TestGameDeleteImpactDoesNotReturnPartialEvidenceOnReadFailure(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"blob", "references", "counts", "sources"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := impactGame(t, 10)
			cause := errors.New("impact snapshot storage failure")
			var hits atomic.Int64
			faultDB := testsupport.OpenSQLFaultDatabase(t, db, testsupport.SQLFaultHooks{
				BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
					if impactReadFault(stage, query, args) {
						hits.Add(1)
						return cause
					}
					return nil
				},
			})
			result, err := GameDeleteImpact(t.Context(), faultDB, "schedule-game")
			if !errors.Is(err, cause) || hits.Load() != 1 || !reflect.DeepEqual(result, GameImpact{}) {
				t.Fatalf("partial impact or lost error: %+v %v hits=%d", result, err, hits.Load())
			}
		})
	}
}

func impactReadFault(stage, query string, args []driver.NamedValue) bool {
	var prefix, id string
	switch stage {
	case "blob":
		prefix, id = "SELECT size_bytes FROM blobs WHERE id=?", "impact-blob-0"
	case "references":
		prefix, id = "WITH game_import_items(id) AS", "schedule-game"
	case "counts":
		prefix, id = "SELECT (SELECT count(*) FROM save_states", "schedule-game"
	case "sources":
		prefix, id = "SELECT metadata_source_kind FROM games WHERE id=? UNION SELECT content_source_kind", "schedule-game"
	default:
		return false
	}
	return len(args) > 0 && args[0].Value == id && strings.HasPrefix(strings.Join(strings.Fields(query), " "), prefix)
}
