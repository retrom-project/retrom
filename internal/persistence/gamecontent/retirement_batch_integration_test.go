//go:build integration

package gamecontent

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"retrom/internal/service/gamecontent"
	"retrom/internal/testsupport"
)

func TestContentRetirementDrainsReferencesAcrossBatchBoundary(t *testing.T) {
	t.Parallel()
	fixture := contentRetirementFixture(t)
	for i := 0; i < 200; i++ {
		_, err := fixture.db.ExecContext(t.Context(), `INSERT INTO launch_content_files
(launch_session_id,logical_name,blob_id,format_version,created_at_ms)
SELECT launch_session_id,?,blob_id,format_version,created_at_ms FROM launch_content_files
WHERE launch_session_id=? ORDER BY logical_name LIMIT 1`, fmt.Sprintf("companion-%03d.bin", i), fixture.launchID)
		if err != nil {
			t.Fatal(err)
		}
	}
	var impact gamecontent.RetirementImpact
	err := New(fixture.db).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		var err error
		impact, err = gamecontent.RetireInScope(t.Context(), scope.Retirements, fixture.gameID, fixture.variantID, time.Now().UnixMilli())
		return err
	})
	if err != nil || impact.SaveStateCount != 1 {
		t.Fatalf("retirement failed: impact=%+v err=%v", impact, err)
	}
	var files, saves int
	var state, variant string
	err = fixture.db.QueryRowContext(t.Context(), `SELECT
(SELECT count(*) FROM launch_content_files WHERE launch_session_id=?),
(SELECT count(*) FROM save_states WHERE id=?),
(SELECT state FROM launch_sessions WHERE id=?),
(SELECT status FROM game_variants WHERE id=?)`, fixture.launchID, fixture.saveID, fixture.launchID, fixture.variantID).
		Scan(&files, &saves, &state, &variant)
	if err != nil || files != 0 || saves != 0 || state != "REVOKED" || variant != "READY" {
		t.Fatalf("incomplete retirement: files=%d saves=%d state=%s variant=%s err=%v", files, saves, state, variant, err)
	}
}

func TestContentRetirementPreservesLateReadCauseAndRollsBack(t *testing.T) {
	t.Parallel()
	fixture := contentRetirementFixture(t)
	cause := errors.New("late content reference read failed")
	var hits atomic.Int64
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.db, testsupport.SQLFaultHooks{
		BeforeQuery: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.HasPrefix(strings.Join(strings.Fields(query), " "), "SELECT file.launch_session_id,file.logical_name,'',file.blob_id,NULL FROM launch_content_files") {
				hits.Add(1)
				return cause
			}
			return nil
		},
	})
	err := New(fault).WithWrite(t.Context(), func(scope gamecontent.WriteScope) error {
		_, err := gamecontent.RetireInScope(t.Context(), scope.Retirements, fixture.gameID, fixture.variantID, time.Now().UnixMilli())
		return err
	})
	if !errors.Is(err, cause) || hits.Load() != 1 {
		t.Fatalf("late read cause lost: hits=%d err=%v", hits.Load(), err)
	}
	fixture.assertUnchanged(t)
}
