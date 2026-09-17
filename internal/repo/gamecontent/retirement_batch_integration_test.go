//go:build integration

package gamecontent

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gamecontentmodel "retrom/internal/model/gamecontent"
	"retrom/internal/repo/dbexec"
	"retrom/internal/testkit/testsupport"
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
	var impact gamecontentmodel.RetirementImpact
	tx, err := fixture.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbexec.Rollback(tx)
	scope := BindRetirement(tx)
	impact, err = retireContent(t.Context(), scope, fixture.gameID, fixture.variantID, time.Now().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if impact.SaveStateCount != 1 {
		t.Fatalf("retirement failed: impact=%+v", impact)
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
	tx, err := fault.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = tx.Rollback()
	}()
	scope := BindRetirement(tx)
	_, retireErr := retireContent(t.Context(), scope, fixture.gameID, fixture.variantID, time.Now().UnixMilli())
	if !errors.Is(retireErr, cause) || hits.Load() != 1 {
		t.Fatalf("late read cause lost: hits=%d err=%v", hits.Load(), retireErr)
	}
	_ = tx.Rollback()
	fixture.assertUnchanged(t)
}

// retireContent is the repo-local retirement function exposed for tests.
// The function is defined in commands.go.
func init() {
	// compile-time assertion that retireContent exists and has correct signature.
	var _ func(context.Context, gamecontentmodel.RetirementScope, string, string, int64) (gamecontentmodel.RetirementImpact, error) = retireContent
	_ = sql.ErrNoRows // keep sql import alive
}
